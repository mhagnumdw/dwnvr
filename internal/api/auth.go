package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/users"
)

const (
	sessionCookie = "dwnvr_session"
	sessionTTL    = 30 * 24 * time.Hour

	// With less than this left before it expires, the session in use gets a new
	// cookie, with the full term. That way the 30 days count from the last time
	// the screen talked to dwnvr, and not from the login: whoever uses the
	// installed app is not sent back to the login screen every month.
	sessionRenew = sessionTTL / 2

	// Cap of the X-Forwarded-For copied to the log (see forwarded). A whole
	// proxy chain fits; the rest is cut.
	forwardedMax = 200
)

// person is who made the request: the owner, from dwnvr.yaml, or someone from
// usuarios.json.
type person struct {
	Username string     `json:"username,omitempty"`
	Nome     string     `json:"name,omitempty"`
	Role     users.Role `json:"role"`
	// Owner is the dwnvr.yaml account. Its password is changed there, and not
	// through the screen.
	Owner bool `json:"owner"`
	// Avatar is the photo id (see GET /api/avatar), or empty.
	Avatar string `json:"avatar,omitempty"`

	// credential is what ties the cookie to the person's password (see
	// sessionKey): the owner's password, or the value stored in usuarios.json.
	credential string
}

func (p person) admin() bool { return p.Role == users.RoleAdmin }

// noAuth is who uses dwnvr with authentication disabled: there are no users,
// and whoever reaches the screen does everything.
var noAuth = person{Role: users.RoleAdmin, Owner: true}

// who finds the person by login. Whoever has not set the password yet is left
// out: there is no credential to tie a session to.
func (s *Server) who(username string) (person, bool) {
	if username == s.cfg.Server.Username {
		owner := s.users.Owner()
		nome := owner.Name
		if nome == "" {
			nome = username
		}
		return person{Username: username, Nome: nome, Role: users.RoleAdmin, Owner: true,
			Avatar: owner.Avatar, credential: s.cfg.Server.Password}, true
	}
	u, ok := s.users.Find(username)
	if !ok || u.PasswordHash == "" {
		return person{}, false
	}
	return person{Username: u.Username, Nome: u.Name, Role: u.Role, Avatar: u.Avatar,
		credential: u.PasswordHash}, true
}

// whoLogsIn finds the person by what they typed at login: the username or, with
// "@", the email. The registry username does not accept "@" (see
// users.ValidateUsername), so there is no doubt which of the two came. The
// owner has no email and signs in only by username, which dwnvr.yaml lets have
// "@": the owner's counts before looking for an email.
func (s *Server) whoLogsIn(typed string) (p person, exists, byEmail bool) {
	if !strings.Contains(typed, "@") || typed == s.cfg.Server.Username {
		p, exists = s.who(typed)
		return p, exists, false
	}
	u, ok := s.users.FindByEmail(strings.TrimSpace(typed))
	if !ok {
		return person{}, false, true
	}
	p, exists = s.who(u.Username)
	return p, exists, true
}

// sessionKey is the key that signs a person's cookies. It comes from
// .session-secret, from the login and from their credential: changing the
// password, or generating a new link, changes the key, and every cookie of
// theirs issued before stops matching the signature. Only they go back to the
// login screen; the others carry on. Without this, changing the password would
// kick no one out, and the session in use, which renews itself, would never
// expire.
//
// Restarting with the same credential gives the same key and drops no one.
// Dropping everyone at once is deleting .session-secret.
func (s *Server) sessionKey(p person) []byte {
	mac := hmac.New(sha256.New, s.sessionSecret)
	mac.Write([]byte(p.Username + "\x00" + p.credential))
	return mac.Sum(nil)
}

// The session token is "<login>.<expiresAtUnix>.<hmac>", with the login in
// base64 (it may have a dot). There is no state on the server: the signature is
// enough to validate, which avoids keeping a live session table on a device
// with 1.5 GB of RAM.
func (s *Server) signToken(p person, expires int64) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(p.Username)) + "." +
		strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, s.sessionKey(p))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// tokenPerson returns whose the token is and until when it is valid, and
// ok=false if it is expired, malformed, with a signature that does not match,
// or if the person no longer exists.
func (s *Server) tokenPerson(tok string) (p person, expires int64, ok bool) {
	before, after, ok0 := strings.CutLast(tok, ".")
	if !ok0 {
		return person{}, 0, false
	}
	payload, sig := before, after
	login, deadline, ok := strings.Cut(payload, ".")
	if !ok {
		return person{}, 0, false
	}
	username, err := base64.RawURLEncoding.DecodeString(login)
	if err != nil {
		return person{}, 0, false
	}
	expires, err = strconv.ParseInt(deadline, 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return person{}, 0, false
	}
	p, ok = s.who(string(username))
	if !ok {
		return person{}, 0, false
	}

	mac := hmac.New(sha256.New, s.sessionKey(p))
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return person{}, 0, false
	}
	return p, expires, true
}

// session says who the person of the request is, if the session is valid. If it
// is already past half of its term, the response carries a new cookie (see
// sessionRenew).
//
// It is also where the person's last access is recorded: every authenticated
// request goes through here, including /api/session. The record only touches
// memory (see users.AccessLog).
//
// It creates no session state on the server: renewing is just signing another
// term.
func (s *Server) session(w http.ResponseWriter, r *http.Request) (person, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return person{}, false
	}
	p, expires, ok := s.tokenPerson(c.Value)
	if !ok {
		return person{}, false
	}
	if time.Until(time.Unix(expires, 0)) < sessionRenew {
		s.setSessionCookie(w, r, p)
	}
	s.accesses.Record(p.Username)
	return p, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, p person) {
	expires := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.signToken(p, expires.Unix()),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   viaHTTPS(r),
	})
}

type personKey struct{}

// personOf returns who made the request. Only valid inside requireAuth and
// requireAdmin, which put it in the context.
func personOf(r *http.Request) person {
	p, _ := r.Context().Value(personKey{}).(person)
	return p
}

// requireAuth wraps a handler requiring a valid session, of any role.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return s.require(false, next)
}

// requireAdmin wraps a handler requiring an admin session. Hiding the tab on
// the screen is not security: this is where the viewer is rejected.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.require(true, next)
}

func (s *Server) require(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := noAuth
		if s.cfg.Server.AuthEnabled() {
			var ok bool
			if p, ok = s.session(w, r); !ok {
				writeError(w, http.StatusUnauthorized, "não autenticado")
				return
			}
		}
		if admin && !p.admin() {
			writeError(w, http.StatusForbidden, "só o administrador")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), personKey{}, p)))
	}
}

// handleLogin checks the password and opens the session.
//
// Every attempt pays for a full password computation (see
// users.VerifyPassword), in the one-at-a-time queue, including the owner's,
// whose password is in plain text in dwnvr.yaml, and that of a user who does
// not exist. That way the response time does not tell who exists nor who the
// owner is.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limitLogin(w, r) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	p, exists, byEmail := s.whoLogsIn(req.Username)
	stored := ""
	if exists && !p.Owner {
		stored = p.credential
	}
	ok, err := users.VerifyPassword(r.Context(), req.Password, stored)
	if r.Context().Err() != nil {
		return // the browser gave up in the queue
	}
	if err != nil {
		s.log.Error("unreadable stored password in usuarios.json", "user", req.Username, "err", err)
	}
	if exists && p.Owner {
		ok = subtle.ConstantTimeCompare([]byte(req.Password), []byte(p.credential)) == 1
	}
	if !exists || !ok {
		s.log.Warn("login attempt rejected", "user", req.Username, "from", r.RemoteAddr, forwarded(r))
		writeError(w, http.StatusUnauthorized, "usuário ou senha inválidos")
		return
	}

	s.setSessionCookie(w, r, p)
	s.accesses.Record(p.Username)
	// The session, the last access and the log stay with the username, even
	// when signing in by email.
	via := slog.Attr{} // empty, slog does not write it
	if byEmail {
		via = slog.Bool("via_email", true)
	}
	s.log.Info("login accepted", "user", p.Username, via, "from", r.RemoteAddr, forwarded(r))
	writeJSON(w, map[string]any{"ok": true, "user": p})
}

// limitLogin counts the attempt against the address limit and, past it, answers
// 429 with Retry-After in seconds. It comes before everything, including
// reading the body: the password computation is what the limit saves. It counts
// every attempt, the right one too, because only after the computation is it
// known whether it was the right one.
func (s *Server) limitLogin(w http.ResponseWriter, r *http.Request) bool {
	if s.loginLimit == nil {
		return true
	}
	client := clientOf(r, s.proxies)
	res := s.loginLimit.Allow(client.String())
	if res.Allowed {
		return true
	}
	// Rounded up: with 0 s the screen would try again too early.
	seg := int((res.RetryAfter + time.Second - 1) / time.Second)
	if res.FirstDenial {
		s.log.Warn("login attempt limit reached", "client", client, "wait_s", seg,
			"from", r.RemoteAddr, forwarded(r))
	}
	w.Header().Set("Retry-After", strconv.Itoa(seg))
	writeError(w, http.StatusTooManyRequests, fmt.Sprintf("muitas tentativas; tente de novo em %d s", seg))
	return false
}

// handleLogout deletes the cookie. It only becomes a log with a valid session:
// without one there is no one to name, and there was nothing to end.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if p, _, ok := s.tokenPerson(c.Value); ok {
			s.log.Info("logout", "user", p.Username, "from", r.RemoteAddr, forwarded(r))
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: viaHTTPS(r),
	})
	writeJSON(w, map[string]any{"ok": true})
}

// viaHTTPS says whether the browser talks to dwnvr over HTTPS, and it is what
// turns on the Secure of the session cookie.
//
// dwnvr only serves HTTP, and the typical install is HTTP on the LAN, where a
// Secure cookie would be silently discarded and login would never stick. HTTPS
// comes from a TLS proxy in front, and `tailscale serve` and Caddy announce it
// through X-Forwarded-Proto. Then the cookie goes out Secure and does not leak
// in an HTTP request to the same address.
//
// Trusting the header opens nothing, because it only decides Secure: an HTTP
// client that forges it gets a cookie the browser itself rejects.
//
// With the proxy and the HTTP port on the same host name, after a login over
// HTTPS the browser does not let HTTP store a cookie with the same name.
// Whoever sets it up that way must always come in through the same address.
func viaHTTPS(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// forwarded is the X-Forwarded-For of the request, for the log next to "from".
//
// Behind Caddy or `tailscale serve`, "from" (the RemoteAddr) is the proxy
// address, the same for everyone. Who really arrived is in X-Forwarded-For,
// which both fill in.
//
// The value goes as it came, without deciding whether it is trustworthy:
// whoever talks directly to 8080 can make up the header. That is why it stays
// in a field of its own, and "from" remains the connection. Without the header,
// the attribute comes out empty and slog omits it.
func forwarded(r *http.Request) slog.Attr {
	v := strings.Join(r.Header.Values("X-Forwarded-For"), ", ")
	if v == "" {
		return slog.Attr{}
	}
	if len(v) > forwardedMax {
		v = v[:forwardedMax] + "…"
	}
	return slog.String("forwarded", v)
}

// handleSession tells the frontend whether it needs to show the login screen
// and, with a valid session, who is logged in. It is the first call when the
// screen opens, and so it also renews the session.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"authRequired": s.cfg.Server.AuthEnabled()}
	p, ok := noAuth, true
	if s.cfg.Server.AuthEnabled() {
		p, ok = s.session(w, r)
	}
	resp["authenticated"] = ok
	if ok {
		resp["user"] = p
	}
	writeJSON(w, resp)
}
