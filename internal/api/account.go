package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mhagnumdw/dwnvr/internal/users"
)

// As rotas da Minha conta: o que cada pessoa muda de si mesma, de qualquer
// papel. Ninguém muda o dos outros por aqui; o que o admin faz com os outros
// está em usuarios.go.

// handleConta diz quem está logado e os números do formulário, para a tela
// não os repetir à mão.
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

// respostaDaConta devolve a pessoa como ficou depois da mudança, para a tela
// atualizar o header sem perguntar de novo.
func (s *Server) accountResponse(w http.ResponseWriter, username string) {
	p, ok := s.who(username)
	if !ok {
		writeError(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "user": p})
}

// handleTrocarSenha troca a própria senha, pedindo a atual. Os outros
// aparelhos da pessoa voltam para a tela de login; o que pediu recebe um
// cookie novo e continua.
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
		Nova    string `json:"newPassword"`
	}
	if !readBody(w, r, &req) {
		return
	}
	u, err := s.users.ChangePassword(r.Context(), p.Username, req.Current, req.Nova)
	if r.Context().Err() != nil {
		return // o navegador desistiu na fila
	}
	if err != nil {
		s.reject(w, "trocando a senha", err)
		return
	}
	newOne, ok := s.who(u.Username)
	if !ok {
		s.fail(w, "abrindo a sessão de quem acabou de trocar a senha", errors.New("pessoa sumiu"))
		return
	}
	s.setSessionCookie(w, r, newOne)
	writeJSON(w, map[string]any{"ok": true, "user": newOne})
	// O aviso de sessão vai depois da resposta: o ao vivo aberto em outra aba
	// deste navegador confere a sessão assim que o aviso cai, e precisa
	// encontrar o cookie novo já guardado.
	_ = http.NewResponseController(w).Flush()
	s.dropPerson(p.Username, "senha trocada")
	s.log.Info("senha trocada pela própria pessoa", "usuario", p.Username)
}

// handleMudarNome troca o próprio nome de exibição.
func (s *Server) handleChangeName(w http.ResponseWriter, r *http.Request) {
	if s.noRegistry(w) {
		return
	}
	var req struct {
		Nome string `json:"name"`
	}
	if !readBody(w, r, &req) {
		return
	}
	p := personOf(r)
	if _, err := s.users.Rename(p.Username, req.Nome); err != nil {
		s.reject(w, "trocando o nome", err)
		return
	}
	s.accountResponse(w, p.Username)
}

// handleTrocarAvatar guarda a própria foto. O corpo é o JPEG, já reduzido
// pelo navegador; acima do teto, a leitura para no meio.
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

// handleTirarAvatar apaga a própria foto.
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

// handleAvatar serve a foto pelo id. Cada um vê a própria; o admin vê todas,
// para a tela Usuários. A foto de outra pessoa responde como a que não
// existe.
//
// O id muda a cada troca, então a resposta pode ficar no navegador para
// sempre: foto nova é URL nova.
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
	// private: um proxy no caminho não guarda a foto de ninguém.
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), f)
}
