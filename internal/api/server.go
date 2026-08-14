// Package api expõe a interface HTTP do dwnvr: gravações, live e diagnóstico.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/buildinfo"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/retention"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

type Server struct {
	cfg    *config.Config
	store  *store.Store
	client *go2rtc.Client
	mgr    *recorder.Manager
	log    *slog.Logger
	secret []byte
	// Quando este processo subiu. É a referência do uptime da aplicação, que a
	// tela de diagnóstico compara com o da máquina para distinguir "só o dwnvr
	// reiniciou" de "a máquina reiniciou".
	startedAt time.Time
}

func New(cfg *config.Config, st *store.Store, client *go2rtc.Client,
	mgr *recorder.Manager, secret []byte, log *slog.Logger) *Server {

	return &Server{cfg: cfg, store: st, client: client, mgr: mgr,
		secret: secret, log: log, startedAt: time.Now()}
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

	// Gravações
	mux.HandleFunc("GET /api/cameras", s.requireAuth(s.handleCameras))
	mux.HandleFunc("POST /api/cameras", s.requireAuth(s.handleSaveCamera))
	mux.HandleFunc("DELETE /api/cameras", s.requireAuth(s.handleDeleteCamera))
	mux.HandleFunc("GET /api/health", s.requireAuth(s.handleHealth))
	mux.HandleFunc("DELETE /api/rec", s.requireAuth(s.handleDeleteRecordings))
	mux.HandleFunc("GET /api/rec/days", s.requireAuth(s.handleDays))
	mux.HandleFunc("GET /api/rec/timeline", s.requireAuth(s.handleTimeline))
	mux.HandleFunc("GET /api/rec/init", s.requireAuth(s.handleInit))
	mux.HandleFunc("GET /api/rec/seg", s.requireAuth(s.handleSegment))
	mux.HandleFunc("GET /api/rec/thumb", s.requireAuth(s.handleThumb))
	mux.HandleFunc("GET /api/rec/playlist.m3u8", s.requireAuth(s.handlePlaylist))
	mux.HandleFunc("GET /api/rec/export", s.requireAuth(s.handleExport))

	// Live: sinalização e mídia ficam com o go2rtc; o dwnvr só faz proxy.
	mux.Handle("/api/live/", s.requireAuthHandler(s.liveProxy()))

	// A interface é servida SEM autenticação, de propósito: são só HTML, CSS e
	// JS, sem nenhum dado das câmeras. Protegê-la impediria o navegador de
	// carregar a própria tela de login. Tudo que é dado está atrás da API.
	mux.Handle("/", s.webHandler())

	return logRequests(s.log, mux)
}

func (s *Server) requireAuthHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(s.requireAuth(h.ServeHTTP))
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
// sobrou em disco de câmeras já removidas.
//
// Juntar as três coisas numa resposta só é o que permite à tela de cadastro
// mostrar apenas streams que existem de verdade, em vez de pedir que o usuário
// digite um nome e descubra o erro depois - e é onde as gravações órfãs voltam a
// ser visíveis, já que nenhum outro endpoint enxerga câmera sem cadastro.
func (s *Server) handleCameras(w http.ResponseWriter, r *http.Request) {
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

	resp := map[string]any{"cameras": cams}

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
	resp["streams"] = available
	writeJSON(w, resp)
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
		resp["disk"] = map[string]any{
			"freeBytes":  free,
			"totalBytes": total,
			"dwnvrBytes": used,
			"minFreeMB":  s.cfg.Storage.MinFreeMB,
			"belowMin":   free < s.cfg.Storage.MinFreeMB<<20,
		}
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

	writeJSON(w, resp)
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
