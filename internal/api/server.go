// Package api expõe a interface HTTP do dwnvr: gravações, live e diagnóstico.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

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

	mu   sync.RWMutex
	cams []config.Camera
}

func New(cfg *config.Config, st *store.Store, client *go2rtc.Client,
	mgr *recorder.Manager, cams []config.Camera, secret []byte, log *slog.Logger) *Server {

	return &Server{cfg: cfg, store: st, client: client, mgr: mgr,
		cams: cams, secret: secret, log: log}
}

// knownCamera evita que um ID arbitrário vindo da URL vire caminho no disco.
// Só câmeras cadastradas são aceitas, o que fecha travessia de diretório na
// origem em vez de tentar higienizar o caminho depois.
func (s *Server) knownCamera(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.cams {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Sessão
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)

	// Gravações
	mux.HandleFunc("GET /api/cameras", s.requireAuth(s.handleCameras))
	mux.HandleFunc("GET /api/health", s.requireAuth(s.handleHealth))
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

// handleCameras lista as câmeras cadastradas e o que o go2rtc oferece.
//
// Juntar as duas coisas numa resposta só é o que permite à tela de cadastro
// mostrar apenas streams que existem de verdade, em vez de pedir que o usuário
// digite um nome e descubra o erro depois.
func (s *Server) handleCameras(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cams := make([]config.Camera, len(s.cams))
	for i, c := range s.cams {
		cams[i] = s.cfg.Resolve(c)
	}
	s.mu.RUnlock()

	type streamInfo struct {
		Name        string   `json:"name"`
		Registered  bool     `json:"registered"`
		HasAudio    bool     `json:"hasAudio"`
		AudioCodecs []string `json:"audioCodecs,omitempty"`
		Transcoding bool     `json:"transcoding"`
	}

	resp := map[string]any{"cameras": cams}

	streams, err := s.client.Streams(r.Context())
	if err != nil {
		// O go2rtc estar fora do ar não pode impedir a listagem das câmeras já
		// cadastradas — só a descoberta de novas.
		resp["go2rtcError"] = err.Error()
		writeJSON(w, resp)
		return
	}

	registered := map[string]bool{}
	for _, c := range cams {
		registered[c.ID] = true
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
		// segmentos, registrar tudo afogaria o journal do Pi.
		if sw.code >= 400 {
			log.Warn("requisição recusada", "status", sw.code,
				"metodo", r.Method, "caminho", r.URL.Path, "de", r.RemoteAddr)
		}
	})
}
