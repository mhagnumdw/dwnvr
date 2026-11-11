package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/users"
)

// The routes of the Users screen (admin only) and those of the invite link
// (public: whoever opens the link has no session yet).

// userOnScreen is the person as the Users screen sees them: without the stored
// password and without the token hash.
type userOnScreen struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	// Only for whoever has a valid email: one written wrong by hand in the file
	// does not show up, as if there were none.
	Email  string     `json:"email,omitempty"`
	Role   users.Role `json:"role"`
	Status string     `json:"status"`
	Avatar string     `json:"avatar,omitempty"`
	// Only with a link open or expired.
	LinkExpiresAtMs int64 `json:"inviteExpiresAtMs,omitempty"`
	CreatedAtMs     int64 `json:"createdAtMs,omitempty"`
	// Only with the password set through the link, and not for whoever set it
	// before the field existed.
	ActiveSinceMs int64 `json:"activeSinceMs,omitempty"`
	// Only for whoever has used dwnvr after the field existed, in any status:
	// whoever got a new link keeps the access from before.
	LastAccessMs int64 `json:"lastAccessMs,omitempty"`
}

// ownerOnScreen is the owner as the Users screen sees them. The owner is not in
// the registry, and the screen shows them with no button at all.
type ownerOnScreen struct {
	Username     string `json:"username"`
	Name         string `json:"name"`
	Avatar       string `json:"avatar"`
	LastAccessMs int64  `json:"lastAccessMs,omitempty"`
}

// ms is the instant in milliseconds, or 0 for the zero instant, which omitempty
// drops.
func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func (s *Server) onScreen(u users.User, now time.Time) userOnScreen {
	v := userOnScreen{Username: u.Username, Name: u.Name, Email: u.ValidEmail(), Role: u.Role,
		Status: u.Status(now), Avatar: u.Avatar}
	// An access from before the account was created belongs to whoever had the
	// same username before. The Forget in handleCreateUser only reaches disk on
	// the next write, and a crash in the meantime would bring the old access
	// back at boot.
	if last := s.accesses.Last(u.Username); !last.Before(u.CreatedAt) {
		v.LastAccessMs = ms(last)
	}
	if u.Link != nil {
		v.LinkExpiresAtMs = u.Link.ExpiresAt.UnixMilli()
	}
	if !u.CreatedAt.IsZero() {
		v.CreatedAtMs = u.CreatedAt.UnixMilli()
	}
	if !u.ActiveSince.IsZero() && v.Status == "active" {
		v.ActiveSinceMs = u.ActiveSince.UnixMilli()
	}
	return v
}

// inviteOnScreen is the response to whoever generates a link: the person and
// the token, which only exists in this response. The screen builds the link
// with the address through which the admin is accessing.
func (s *Server) inviteOnScreen(c users.Invite) map[string]any {
	return map[string]any{"user": s.onScreen(c.User, time.Now()), "token": c.Token}
}

// noRegistry rejects what writes to the registry when there is no
// authentication: without it there is no login, and the people in usuarios.json
// are ignored.
func (s *Server) noRegistry(w http.ResponseWriter) bool {
	if s.cfg.Server.AuthEnabled() {
		return false
	}
	writeError(w, http.StatusConflict,
		"a autenticação está desligada: defina server.username e server.password no dwnvr.yaml")
	return true
}

// reject translates the registry error into the response. A form's rejections
// go field by field in "fields", so the screen puts each message under its
// field, and the first one also in "error", as in every rejection.
func (s *Server) reject(w http.ResponseWriter, what string, err error) {
	var rejected users.ValidationError
	var rejections users.FieldErrors
	switch {
	case errors.As(err, &rejections):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": rejections.Error(), "fields": rejections})
	case errors.As(err, &rejected):
		writeError(w, http.StatusBadRequest, rejected.Error())
	case errors.Is(err, users.ErrNotFound), errors.Is(err, users.ErrNoAvatar):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, users.ErrInvalidLink):
		writeError(w, http.StatusGone, err.Error())
	default:
		s.fail(w, what, err)
	}
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return false
	}
	return true
}

// handleUsers lists the people, with the status of each one. The owner comes
// separately: the owner is not in the registry, and the screen shows them with
// no button at all.
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"authRequired": s.cfg.Server.AuthEnabled(),
		"users":        []userOnScreen{},
		// The form numbers, so the screen does not repeat them by hand.
		"inviteValidityMs":  users.InviteValidity.Milliseconds(),
		"maxNameLength":     users.MaxNameLen,
		"maxUsernameLength": users.MaxUsernameLen,
		"maxEmailLength":    users.MaxEmailLen,
		// Without the smtp section enabled, the screen hides sending by email.
		// The person's email it shows all the same, because it is used for
		// signing in.
		"emailEnabled": s.sending != nil,
	}
	if !s.cfg.Server.AuthEnabled() {
		writeJSON(w, resp)
		return
	}
	owner, _ := s.who(s.cfg.Server.Username)
	resp["owner"] = ownerOnScreen{Username: owner.Username, Name: owner.Name, Avatar: owner.Avatar,
		LastAccessMs: ms(s.accesses.Last(owner.Username))}
	now := time.Now()
	list := []userOnScreen{}
	for _, u := range s.users.List() {
		list = append(list, s.onScreen(u, now))
	}
	resp["users"] = list
	writeJSON(w, resp)
}

// handleCreateUser creates a viewer, still without a password, and returns the
// link token for them to set their own. The email is optional.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Email    string `json:"email"`
	}
	if !readBody(w, r, &req) {
		return
	}
	c, err := s.users.Create(req.Username, req.Name, req.Email)
	if err != nil {
		s.reject(w, "criando o usuário", err)
		return
	}
	// A reused login does not inherit the access of whoever had it before.
	s.accesses.Forget(req.Username)
	s.log.Info("user created from the interface", "user", req.Username, "by", personOf(r).Username)
	writeJSON(w, s.inviteOnScreen(c))
}

// handleCheckNew says what POST /api/usuarios would reject with the same body,
// without creating anyone: the screen checks each field when the admin leaves
// it. It answers 200 with empty "fields" when everything is right.
func (s *Server) handleCheckNew(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Email    string `json:"email"`
	}
	if !readBody(w, r, &req) {
		return
	}
	rejections, err := s.users.CheckNew(req.Username, req.Name, req.Email)
	if err != nil {
		s.reject(w, "conferindo o usuário novo", err)
		return
	}
	writeJSON(w, map[string]any{"fields": rejections})
}

// handleNewLink generates another link for the person. Their password stops
// working, so does the previous link, and whatever they have open drops at
// once.
func (s *Server) handleNewLink(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
	}
	if !readBody(w, r, &req) {
		return
	}
	c, err := s.users.NewLink(req.Username)
	if err != nil {
		s.reject(w, "gerando o link novo", err)
		return
	}
	s.dropPerson(req.Username, "new link")
	s.log.Info("new link generated from the interface", "user", req.Username, "by", personOf(r).Username)
	writeJSON(w, s.inviteOnScreen(c))
}

// handleChangeEmail changes someone's email, or deletes it, with an empty
// email. It also works with email disabled: storing sends nothing.
func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if !readBody(w, r, &req) {
		return
	}
	u, err := s.users.SetEmail(req.Username, req.Email)
	if err != nil {
		s.reject(w, "trocando o e-mail", err)
		return
	}
	s.log.Info("email changed from the interface", "user", req.Username, "by", personOf(r).Username)
	writeJSON(w, map[string]any{"user": s.onScreen(u, time.Now())})
}

// handleCheckEmail says what POST /api/usuarios/email would reject with the
// same body, without changing anything. It answers 200 with empty "fields" when
// everything is right.
func (s *Server) handleCheckEmail(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if !readBody(w, r, &req) {
		return
	}
	rejections, err := s.users.CheckEmail(req.Username, req.Email)
	if err != nil {
		s.reject(w, "conferindo o e-mail", err)
		return
	}
	writeJSON(w, map[string]any{"fields": rejections})
}

// handleSendLink sends by email the link that is on the admin's screen. The
// server does not know through which address it is accessed, so the link comes
// ready from the screen, which builds it with the address of the admin's
// browser; here it is only checked, and it must be that person's open link. The
// log says who it went to, never the link: the token is the credential.
func (s *Server) handleSendLink(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) || s.noEmail(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Link     string `json:"url"`
	}
	if !readBody(w, r, &req) {
		return
	}
	token, ok := linkToken(req.Link)
	if !ok {
		writeError(w, http.StatusBadRequest, "link inválido: mande o link como a tela o mostra")
		return
	}
	u, err := s.validInvite(token)
	if err != nil {
		s.reject(w, "conferindo o link", err)
		return
	}
	if u.Username != req.Username {
		writeError(w, http.StatusBadRequest, "o link não é desta pessoa")
		return
	}
	to := u.ValidEmail()
	if to == "" {
		writeError(w, http.StatusBadRequest, u.Name+" não tem e-mail cadastrado")
		return
	}
	if err := s.sending.Send(r.Context(), inviteMessage(u, to, req.Link)); err != nil {
		s.log.Warn("the link did not go by email", "user", u.Username, "by", personOf(r).Username, "err", err)
		writeError(w, http.StatusBadGateway, "o e-mail não foi: "+err.Error())
		return
	}
	s.log.Info("link sent by email", "user", u.Username, "by", personOf(r).Username)
	writeJSON(w, map[string]string{"sentTo": to})
}

// handleRemoveUser deletes the person and drops whatever they have open.
func (s *Server) handleRemoveUser(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	username := r.URL.Query().Get("username")
	if err := s.users.Remove(username); err != nil {
		s.reject(w, "removendo o usuário", err)
		return
	}
	s.dropPerson(username, "removed")
	s.accesses.Forget(username)
	s.log.Info("user removed from the interface", "user", username, "by", personOf(r).Username)
	writeJSON(w, map[string]bool{"removed": true})
}

// handleRemoveAvatarOf removes someone's photo: the admin does not change
// anyone's photo, but can remove one that is not fit.
func (s *Server) handleRemoveAvatarOf(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	username := r.URL.Query().Get("username")
	if err := s.users.RemoveAvatar(username); err != nil {
		s.reject(w, "tirando a foto", err)
		return
	}
	s.log.Info("photo removed from the interface", "user", username, "by", personOf(r).Username)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleCheckInvite says whose link it is, so the set-password screen shows the
// person's name before they type. The token comes in the body, and not in the
// URL, so it does not end up in any proxy log.
func (s *Server) handleCheckInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !readBody(w, r, &req) {
		return
	}
	u, err := s.validInvite(req.Token)
	if err != nil {
		s.reject(w, "conferindo o link", err)
		return
	}
	writeJSON(w, map[string]any{
		"username":          u.Username,
		"name":              u.Name,
		"inviteExpiresAtMs": u.Link.ExpiresAt.UnixMilli(),
		"minPasswordLength": users.MinPasswordLen,
		"maxPasswordLength": users.MaxPasswordLen,
	})
}

// validInvite checks the link. Without authentication enabled, no link is
// valid: there would be no login to use the password set.
func (s *Server) validInvite(token string) (users.User, error) {
	if !s.cfg.Server.AuthEnabled() {
		return users.User{}, users.ErrInvalidLink
	}
	return s.users.CheckInvite(token)
}

// handleInvite sets the password through the link and opens the session right
// away: the person goes straight in, without going through the login screen.
func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !readBody(w, r, &req) {
		return
	}
	if _, err := s.validInvite(req.Token); err != nil {
		s.reject(w, "conferindo o link", err)
		return
	}
	u, err := s.users.SetPassword(r.Context(), req.Token, req.Password)
	if r.Context().Err() != nil {
		return // the browser gave up in the queue
	}
	if err != nil {
		s.reject(w, "definindo a senha", err)
		return
	}
	p, ok := s.who(u.Username)
	if !ok {
		s.fail(w, "abrindo a sessão de quem acabou de definir a senha", errors.New("person vanished"))
		return
	}
	s.log.Info("password set through the link", "user", u.Username, "from", r.RemoteAddr, forwarded(r))
	s.setSessionCookie(w, r, p)
	s.accesses.Record(u.Username)
	writeJSON(w, map[string]any{"ok": true, "user": p})
}
