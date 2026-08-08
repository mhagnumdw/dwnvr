package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookie = "dwnvr_session"
	sessionTTL    = 30 * 24 * time.Hour
)

// O token de sessão é "<expiraEmUnix>.<hmac>". Não há estado no servidor: a
// assinatura basta para validar, o que evita manter uma tabela de sessões viva
// num dispositivo com 1,5 GB de RAM.
func (s *Server) signToken(expires int64) string {
	payload := strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) validToken(tok string) bool {
	payload, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return false
	}
	expires, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return false
	}

	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(want))
}

// requireAuth embrulha um handler exigindo sessão válida.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Server.AuthEnabled() {
			next(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil || !s.validToken(c.Value) {
			writeError(w, http.StatusUnauthorized, "não autenticado")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	// Comparação em tempo constante nos dois campos, e sempre nos dois, para
	// não vazar por tempo se o usuário existe.
	okUser := subtle.ConstantTimeCompare([]byte(req.Username), []byte(s.cfg.Server.Username))
	okPass := subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.cfg.Server.Password))
	if okUser&okPass != 1 {
		s.log.Warn("tentativa de login recusada", "usuario", req.Username, "de", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "usuário ou senha inválidos")
		return
	}

	expires := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.signToken(expires.Unix()),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Sem Secure: a instalação típica é HTTP na LAN, e marcar Secure faria
		// o cookie ser descartado silenciosamente. Quem expuser à internet deve
		// pôr um proxy TLS na frente.
	})
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
	})
	writeJSON(w, map[string]any{"ok": true})
}

// handleSession diz ao frontend se precisa mostrar a tela de login.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	authed := !s.cfg.Server.AuthEnabled()
	if !authed {
		if c, err := r.Cookie(sessionCookie); err == nil && s.validToken(c.Value) {
			authed = true
		}
	}
	writeJSON(w, map[string]any{
		"authRequired":  s.cfg.Server.AuthEnabled(),
		"authenticated": authed,
	})
}
