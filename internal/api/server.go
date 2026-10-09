// Package api expõe a interface HTTP do dwnvr: gravações, live e diagnóstico.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/buildinfo"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/retention"
	"github.com/mhagnumdw/dwnvr/internal/store"
	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

type Server struct {
	cfg    *config.Config
	store  *store.Store
	client *go2rtc.Client
	mgr    *recorder.Manager
	log    *slog.Logger
	// sessionSecret é o conteúdo do .session-secret, de onde sai a chave que
	// assina os cookies de cada pessoa (ver chaveDaSessao).
	sessionSecret []byte
	// usuarios são as pessoas do usuarios.json. O dono vem do cfg.
	usuarios *usuarios.Cadastro
	// ret diz desde quando o disco está abaixo do mínimo. Pode ser nil nos
	// testes, e aí o aviso sai sem o "desde".
	ret *retention.Manager
	// Quando este processo subiu. É a referência do uptime da aplicação, que a
	// tela de diagnóstico compara com o da máquina para distinguir "só o dwnvr
	// reiniciou" de "a máquina reiniciou".
	startedAt time.Time
	// Resultado das sondas de áudio, para não reabrir conexão com a câmera a
	// cada vez que o formulário de cadastro é aberto. Ver probe.go.
	probes probeCache
	// Um teste de escrita no storage em andamento, e o erro do último que
	// falhou. Ver diagnostico_servidor.go. Só a goroutine que ganhou o
	// escrevendo toca o erroEscrita.
	escrevendo  atomic.Bool
	erroEscrita string
	// cadastro enfileira quem mexe no ciclo de vida das câmeras: salvar,
	// remover e apagar gravações. Cada um lê a lista, grava o cameras.json e
	// para ou sobe recorder, e dois ao mesmo tempo - duas abas, dois aparelhos -
	// perdiam a alteração um do outro no arquivo e deixavam um recorder órfão
	// gravando fora do mapa do Manager. A corrida é entre goroutines deste
	// processo, o único que escreve ali: por isso um mutex, e não um arquivo de
	// lock, que só teria serventia entre processos.
	cadastro sync.Mutex
}

func New(cfg *config.Config, st *store.Store, client *go2rtc.Client,
	mgr *recorder.Manager, ret *retention.Manager, cad *usuarios.Cadastro, secret []byte,
	log *slog.Logger) *Server {

	return &Server{cfg: cfg, store: st, client: client, mgr: mgr, ret: ret,
		usuarios: cad, sessionSecret: secret, log: log, startedAt: time.Now()}
}

// knownCamera evita que um ID arbitrário vindo da URL vire caminho no disco.
// Só câmeras cadastradas são aceitas, o que fecha travessia de diretório na
// origem em vez de tentar higienizar o caminho depois.
func (s *Server) knownCamera(id string) bool {
	for _, c := range s.mgr.Cameras() {
		if c.ID == id {
			return true
		}
	}
	return false
}

// registeredIDs é o conjunto de câmeras cadastradas, na forma que a varredura de
// órfãos precisa: tudo que está no storage e não está aqui é material de câmera
// que já foi removida.
func (s *Server) registeredIDs() map[string]bool {
	ids := map[string]bool{}
	for _, c := range s.mgr.Cameras() {
		ids[c.ID] = true
	}
	return ids
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Sessão
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)

	// Versão fica fora da autenticação pelo mesmo motivo da tela de login:
	// precisa ser visível antes de entrar. Além disso é a sonda de deploy -
	// um curl responde se o dwnvr subiu com o código novo, sem cookie.
	mux.HandleFunc("GET /api/version", s.handleVersion)

	// Câmeras e diagnóstico. A lista de câmeras é de todos, e vem enxuta para
	// o usuário comum; o cadastro e o diagnóstico do servidor, só do admin.
	mux.HandleFunc("GET /api/cameras", s.requireAuth(s.handleCameras))
	mux.HandleFunc("POST /api/cameras", s.requireAdmin(s.handleSaveCamera))
	mux.HandleFunc("DELETE /api/cameras", s.requireAdmin(s.handleDeleteCamera))
	mux.HandleFunc("GET /api/streams/probe", s.requireAdmin(s.handleProbeStream))
	mux.HandleFunc("POST /api/go2rtc/restart", s.requireAdmin(s.handleReiniciarGo2rtc))
	mux.HandleFunc("GET /api/health", s.requireAdmin(s.handleHealth))
	mux.HandleFunc("POST /api/reconnects/reset", s.requireAdmin(s.handleZerarReconexoes))
	mux.HandleFunc("GET /api/health/servidor", s.requireAdmin(s.handleDiagnosticoServidor))
	mux.HandleFunc("DELETE /api/rec", s.requireAdmin(s.handleDeleteRecordings))

	// Gravações
	mux.HandleFunc("GET /api/rec/days", s.requireAuth(s.handleDays))
	mux.HandleFunc("GET /api/rec/timeline", s.requireAuth(s.handleTimeline))
	mux.HandleFunc("GET /api/rec/events", s.requireAuth(s.handleEvents))
	mux.HandleFunc("GET /api/rec/init", s.requireAuth(s.handleInit))
	mux.HandleFunc("GET /api/rec/seg", s.requireAuth(s.handleSegment))
	mux.HandleFunc("GET /api/rec/thumb", s.requireAuth(s.handleThumb))
	mux.HandleFunc("GET /api/rec/playlist.m3u8", s.requireAuth(s.handlePlaylist))
	mux.HandleFunc("GET /api/rec/export", s.requireAuth(s.handleExport))

	// Detecções de objeto, de todas as câmeras juntas: a tela de Detecções.
	mux.HandleFunc("GET /api/deteccoes", s.requireAuth(s.handleDeteccoes))
	mux.HandleFunc("GET /api/deteccoes/quadro", s.requireAuth(s.handleQuadroDaDeteccao))
	mux.HandleFunc("GET /api/deteccoes/dias", s.requireAuth(s.handleDiasDeDeteccao))

	// Live: sinalização e mídia ficam com o go2rtc; o dwnvr só faz proxy do
	// websocket do player, e o resto da API do go2rtc fica de fora.
	mux.Handle("GET /api/live/ws", s.requireAuthHandler(s.liveProxy()))

	// A interface é servida SEM autenticação, de propósito: são só HTML, CSS e
	// JS, sem nenhum dado das câmeras. Protegê-la impediria o navegador de
	// carregar a própria tela de login. Tudo que é dado está atrás da API.
	mux.Handle("/", s.webHandler())

	return logRequests(s.log, mux)
}

func (s *Server) requireAuthHandler(h http.Handler) http.Handler {
	return s.requireAuth(h.ServeHTTP)
}

// cameraInfo é a câmera como a tela a vê: o cadastro já com os defaults
// aplicados, mais o diretório onde as gravações dela ficam.
//
// O caminho não entra em config.Camera porque não é cadastro - é consequência
// do storage.root do servidor, e um campo lá acabaria gravado no cameras.json
// como se fosse configurável.
type cameraInfo struct {
	config.Camera
	Dir string `json:"dir"`
}

// handleCameras lista as câmeras cadastradas, o que o go2rtc oferece e o que
// sobrou em disco de câmeras já removidas. O usuário comum recebe só as
// câmeras e o detector (ver camerasDoComum).
//
// Juntar as três coisas numa resposta só é o que permite à tela de cadastro
// mostrar apenas streams que existem de verdade, em vez de pedir que o usuário
// digite um nome e descubra o erro depois - e é onde as gravações órfãs voltam a
// ser visíveis, já que nenhum outro endpoint enxerga câmera sem cadastro.
func (s *Server) handleCameras(w http.ResponseWriter, r *http.Request) {
	if !pessoaDe(r).admin() {
		s.camerasDoComum(w)
		return
	}
	raw := s.mgr.Cameras()
	cams := make([]cameraInfo, len(raw))
	registered := map[string]bool{}
	for i, c := range raw {
		cams[i] = cameraInfo{Camera: s.cfg.Resolve(c), Dir: s.store.Camera(c.ID).Dir()}
		registered[c.ID] = true
	}

	type streamInfo struct {
		Name        string   `json:"name"`
		Registered  bool     `json:"registered"`
		HasAudio    bool     `json:"hasAudio"`
		AudioCodecs []string `json:"audioCodecs,omitempty"`
		Transcoding bool     `json:"transcoding"`
	}

	// "padrao" é uma câmera vazia com os defaults aplicados: o formulário de
	// câmera nova parte dela, para seguir o defaults do dwnvr.yaml. "faixas"
	// dá o min/max dos inputs, para a tela cobrar a mesma régua da API.
	resp := map[string]any{"cameras": cams, "padrao": s.cfg.Resolve(config.Camera{}),
		"faixas": config.FaixasDaCamera}

	// A interface só mostra a aba Detecções com o detector de objetos
	// configurado: sem ele não há detecção nenhuma para listar. Vai aqui, e
	// não no /api/health, porque esta é a resposta que a interface busca ao
	// entrar.
	resp["detector"] = s.mgr.Detector() != nil

	if orphans, err := s.store.Orphans(registered); err != nil {
		// Não impede a listagem: no caso comum não há órfão nenhum, e uma falha
		// ao varrer o storage não deve derrubar a tela de câmeras inteira.
		s.log.Error("listando gravações órfãs", "erro", err)
	} else {
		resp["orphans"] = orphans
	}

	streams, err := s.client.Streams(r.Context())
	if err != nil {
		// O go2rtc estar fora do ar não pode impedir a listagem das câmeras já
		// cadastradas - só a descoberta de novas.
		resp["go2rtcError"] = err.Error()
		writeJSON(w, resp)
		return
	}

	available := make([]streamInfo, 0, len(streams))
	for name, st := range streams {
		info := streamInfo{Name: name, Registered: registered[name]}
		for _, p := range st.Producers {
			if p.Transcoding() {
				info.Transcoding = true
			}
			if p.HasAudio() {
				info.HasAudio = true
				info.AudioCodecs = append(info.AudioCodecs, p.AudioCodecs()...)
			}
		}
		available = append(available, info)
	}
	// O go2rtc devolve os streams num map, e o Go não garante ordem ao
	// percorrê-lo: sem isto a lista mudava de ordem a cada recarga da tela.
	slices.SortFunc(available, func(a, b streamInfo) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	resp["streams"] = available

	// O go2rtc só lê o go2rtc.yaml ao subir: câmera acrescentada depois não
	// aparece em streams, e nada na tela explicava por quê. A divergência entre
	// o arquivo e o que ele serve vira o aviso de reiniciar.
	//
	// Falhar aqui não é erro de ninguém - um go2rtc externo pode não expor o
	// /api/config -, e a tela segue sem o aviso. Debug, e não Warn, porque isto
	// roda a cada abertura da tela e encheria os avisos do Diagnóstico.
	if arquivo, err := s.client.Arquivo(r.Context()); err != nil {
		s.log.Debug("lendo o go2rtc.yaml pela API do go2rtc", "erro", err)
	} else if d, err := go2rtc.Divergencias(arquivo, streams); err != nil {
		s.log.Debug("comparando o go2rtc.yaml com os streams", "erro", err)
	} else if !d.Vazia() {
		resp["go2rtcConfigFile"] = d
	}
	writeJSON(w, resp)
}

// camerasDoComum é o /api/cameras do usuário comum: as câmeras, sem o caminho
// no disco, e se há detector de objetos. O resto só serve à tela Câmeras, que
// é do admin, e o go2rtc nem é consultado: cada abertura da tela custaria duas
// chamadas a ele.
func (s *Server) camerasDoComum(w http.ResponseWriter) {
	raw := s.mgr.Cameras()
	cams := make([]config.Camera, len(raw))
	for i, c := range raw {
		cams[i] = s.cfg.Resolve(c)
	}
	writeJSON(w, map[string]any{"cameras": cams, "detector": s.mgr.Detector() != nil})
}

// handleReiniciarGo2rtc faz o go2rtc reler o go2rtc.yaml. Todas as câmeras
// param por alguns segundos; quem avisa disso antes é a tela.
func (s *Server) handleReiniciarGo2rtc(w http.ResponseWriter, r *http.Request) {
	if err := s.client.Reiniciar(r.Context()); err != nil {
		s.log.Error("reiniciando o go2rtc", "erro", err)
		writeError(w, http.StatusBadGateway, "o go2rtc não aceitou reiniciar: "+err.Error())
		return
	}
	s.log.Info("go2rtc reiniciado pela interface")
	writeJSON(w, map[string]bool{"reiniciado": true})
}

// handleZerarReconexoes recomeça a contagem de reconexões de todas as câmeras.
// Não derruba conexão nenhuma: só os números da tela voltam a zero.
func (s *Server) handleZerarReconexoes(w http.ResponseWriter, r *http.Request) {
	s.mgr.ZerarReconexoes()
	s.log.Info("reconexões zeradas pela interface")
	writeJSON(w, map[string]bool{"zeradas": true})
}

// handleVersion diz qual código está rodando.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, buildinfo.Get())
}

// handleHealth alimenta a tela de diagnóstico.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"cameras": s.mgr.Status()}

	if free, err := retention.FreeBytes(s.cfg.Storage.Root); err == nil {
		total, _ := retention.TotalBytes(s.cfg.Storage.Root)
		var used int64
		for _, c := range s.mgr.Status() {
			used += c.DiskBytes
		}
		disk := map[string]any{
			"freeBytes":  free,
			"totalBytes": total,
			"dwnvrBytes": used,
			"minFreeMB":  s.cfg.Storage.MinFreeMB,
			"belowMin":   free < s.cfg.Storage.MinFreeMB<<20,
		}
		// As órfãs vão à parte, e não somadas ao dwnvrBytes: são do dwnvr, mas
		// nenhum limite da retenção as alcança, e é isso que a tela precisa
		// dizer. Fora da conta, elas apareceriam como espaço de outros
		// programas. Sem órfã, a varredura é um ReadDir do storage.
		if orfas, err := s.store.OrphanBytes(s.registeredIDs()); err != nil {
			s.log.Error("listando gravações órfãs", "erro", err)
		} else {
			disk["orphanBytes"] = orfas
		}
		// O "desde" vem da retenção, que mede o disco a cada passada. Logo
		// depois de o disco cruzar o mínimo ela ainda não passou, e o aviso sai
		// sem ele por até um minuto.
		if s.ret != nil {
			if desde := s.ret.AbaixoDoMinimoDesde(); !desde.IsZero() {
				disk["belowMinSince"] = desde
			}
		}
		resp["disk"] = disk
	}

	// Segundos, e não um instante ISO: o relógio do navegador e o do servidor
	// não são o mesmo, e uma diferença de fuso ou de NTP viraria um "no ar há 3
	// horas" falso. Duração já calculada aqui não tem como ser mal interpretada
	// lá.
	up := map[string]any{"appSeconds": int64(time.Since(s.startedAt).Seconds())}
	if d, ok := machineUptime(); ok {
		up["machineSeconds"] = int64(d.Seconds())
	}
	resp["uptime"] = up

	// A fila do detector é uma só para todas as câmeras, então vai fora da
	// lista delas. Some sem detector configurado.
	if d := s.mgr.Detector(); d != nil {
		resp["detector"] = d
	}

	// O relógio, ao contrário dos uptimes acima, vai como instante mesmo - e é
	// o único campo desta resposta que vai. A tela quer justamente a hora de
	// LÁ: quem administra o dwnvr olha isto para saber se o servidor está com
	// o fuso ou o NTP errado, e uma duração não responderia isso. O offset já
	// vem embutido no texto e a interface o mostra sem reconverter; converter
	// para o fuso do navegador daria a hora que o usuário já tem no relógio
	// dele, que não é a pergunta.
	agora := time.Now()
	sigla, offset := agora.Zone()
	relogio := map[string]any{
		"now":           agora.Format(time.RFC3339),
		"abbr":          sigla,
		"offsetSeconds": offset,
	}
	if nome := zonaLocal(); nome != "" {
		relogio["zone"] = nome
	}
	resp["clock"] = relogio

	// O tamanho da janela vai junto para a tela escrever "nas últimas 8h" sem
	// repetir o número.
	resp["reconnectsWindowHours"] = recorder.JanelaDeReconexoes.Hours()

	// Vem aqui, e não só no /api/cameras, porque este é o endpoint que a tela
	// de diagnóstico relê a cada poucos segundos: o go2rtc caindo com ela
	// aberta precisa aparecer, e sumir quando ele volta.
	if s.client != nil {
		if desde, erro := s.client.Inacessivel(); !desde.IsZero() {
			resp["go2rtc"] = map[string]any{"error": erro, "since": desde}
		}
	}

	writeJSON(w, resp)
}

// zonaLocal descobre o nome IANA do fuso do servidor ("America/Fortaleza").
//
// O pacote time não expõe isso: time.Local se chama "Local" e o que sobra é a
// abreviação ("-03"), que não identifica a região - "-03" é São Paulo, Buenos
// Aires e mais um punhado de lugares. As fontes consultadas aqui são as mesmas
// que o próprio time usa para montar time.Local, na mesma ordem.
//
// Devolve "" quando nenhuma delas responde, e aí a tela mostra só a hora e a
// abreviação: o nome é um detalhe do title, não a informação principal.
func zonaLocal() string {
	// TZ pode vir como ":America/Fortaleza" ou apontar para um arquivo; só o
	// nome interessa.
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && !strings.HasPrefix(tz, "/") {
		return tz
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	// /etc/localtime costuma ser link para .../zoneinfo/America/Fortaleza.
	if alvo, err := os.Readlink("/etc/localtime"); err == nil {
		if _, depois, ok := strings.Cut(alvo, "zoneinfo/"); ok {
			return depois
		}
	}
	return ""
}

// --- utilidades -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// A resposta já foi parcialmente escrita; só resta registrar.
		return
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// fail registra o erro real e devolve uma mensagem genérica: detalhe de
// filesystem não deve vazar para o navegador.
func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "erro", err)
	writeError(w, http.StatusInternalServerError, what)
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack precisa ser repassado para que o proxy de WebSocket funcione.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		// Só o que deu errado vira log: com uma timeline pedindo centenas de
		// segmentos, registrar tudo afogaria o journal do servidor.
		if sw.code >= 400 {
			log.Warn("requisição recusada", "status", sw.code,
				"metodo", r.Method, "caminho", r.URL.Path, "de", r.RemoteAddr)
		}
	})
}
