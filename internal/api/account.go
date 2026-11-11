package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mhagnumdw/dwnvr/internal/users"
)

// The My account routes: what each person changes about themselves, in any
// role. No one changes someone else's here; what the admin does with the others
// is in users.go.

// handleAccount says who is logged in and the form numbers, so the screen does
// not repeat them by hand.
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	writeJSON(w, map[string]any{
		"user":              personOf(r),
		"minPasswordLength": users.MinPasswordLen,
		"maxPasswordLength": users.MaxPasswordLen,
		"maxNameLength":     users.MaxNameLen,
		"avatar": map[string]int{
			"sidePx":   users.AvatarSide,
			"maxBytes": users.MaxAvatarBytes,
			"quality":  users.AvatarQuality,
		},
	})
}

// accountResponse returns the person as they ended up after the change, so the
// screen updates the header without asking again.
func (s *Server) accountResponse(w http.ResponseWriter, username string) {
	p, ok := s.who(username)
	if !ok {
		writeError(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "user": p})
}

// handleChangePassword changes one's own password, asking for the current one.
// The person's other devices go back to the login screen; the one that asked
// gets a new cookie and carries on.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	p := personOf(r)
	if p.Owner {
		writeError(w, http.StatusConflict, "a senha do dono fica no dwnvr.yaml, e se troca lá")
		return
	}
	var req struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if !readBody(w, r, &req) {
		return
	}
	u, err := s.users.ChangePassword(r.Context(), p.Username, req.Current, req.New)
	if r.Context().Err() != nil {
		return // the browser gave up in the queue
	}
	if err != nil {
		s.reject(w, "trocando a senha", err)
		return
	}
	newOne, ok := s.who(u.Username)
	if !ok {
		s.fail(w, "abrindo a sessão de quem acabou de trocar a senha", errors.New("person vanished"))
		return
	}
	s.setSessionCookie(w, r, newOne)
	writeJSON(w, map[string]any{"ok": true, "user": newOne})
	// The session warning goes after the response: live open in another tab of
	// this browser checks the session as soon as the warning drops, and needs
	// to find the new cookie already stored.
	_ = http.NewResponseController(w).Flush()
	s.dropPerson(p.Username, "password changed")
	s.log.Info("password changed by the person themselves", "user", p.Username)
}

// handleChangeName changes one's own display name.
func (s *Server) handleChangeName(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readBody(w, r, &req) {
		return
	}
	p := personOf(r)
	if _, err := s.users.Rename(p.Username, req.Name); err != nil {
		s.reject(w, "trocando o nome", err)
		return
	}
	s.accountResponse(w, p.Username)
}

// handleChangeAvatar stores one's own photo. The body is the JPEG, already
// shrunk by the browser; above the cap, reading stops midway.
func (s *Server) handleChangeAvatar(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, users.MaxAvatarBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("a foto passa de %d KB", users.MaxAvatarBytes>>10))
			return
		}
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	p := personOf(r)
	if _, err := s.users.SetAvatar(p.Username, b); err != nil {
		s.reject(w, "guardando a foto", err)
		return
	}
	s.accountResponse(w, p.Username)
}

// handleRemoveAvatar deletes one's own photo.
func (s *Server) handleRemoveAvatar(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	p := personOf(r)
	if err := s.users.RemoveAvatar(p.Username); err != nil {
		s.reject(w, "tirando a foto", err)
		return
	}
	s.accountResponse(w, p.Username)
}

// handleAvatar serves the photo by id. Each one sees their own; the admin sees
// all, for the Users screen. Someone else's photo answers like one that does
// not exist.
//
// The id changes on every change, so the response can stay in the browser
// forever: a new photo is a new URL.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	filePath, owner, err := s.users.Avatar(r.URL.Query().Get("id"))
	p := personOf(r)
	if err == nil && !p.admin() && owner != p.Username {
		err = users.ErrNoAvatar
	}
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		s.fail(w, "abrindo a foto", err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.fail(w, "abrindo a foto", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("X-Content-Type-Options", "nosniff")
	// private: a proxy on the way does not store anyone's photo.
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), f)
}
