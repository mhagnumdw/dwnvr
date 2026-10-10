package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

// As rotas da Minha conta: o que cada pessoa muda de si mesma, de qualquer
// papel. Ninguém muda o dos outros por aqui; o que o admin faz com os outros
// está em usuarios.go.

// handleConta diz quem está logado e os números do formulário, para a tela
// não os repetir à mão.
func (s *Server) handleConta(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	writeJSON(w, map[string]any{
		"pessoa":              pessoaDe(r),
		"senhaMinima":         usuarios.SenhaMinima,
		"senhaMaxima":         usuarios.SenhaMaxima,
		"tamanhoMaximoDoNome": usuarios.TamanhoMaximoDoNome,
		"avatar": map[string]int{
			"lado":      usuarios.LadoDoAvatar,
			"tetoBytes": usuarios.TetoDoAvatar,
			"qualidade": usuarios.QualidadeDoAvatar,
		},
	})
}

// respostaDaConta devolve a pessoa como ficou depois da mudança, para a tela
// atualizar o header sem perguntar de novo.
func (s *Server) respostaDaConta(w http.ResponseWriter, usuario string) {
	p, ok := s.quem(usuario)
	if !ok {
		writeError(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "pessoa": p})
}

// handleTrocarSenha troca a própria senha, pedindo a atual. Os outros
// aparelhos da pessoa voltam para a tela de login; o que pediu recebe um
// cookie novo e continua.
func (s *Server) handleTrocarSenha(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	p := pessoaDe(r)
	if p.Dono {
		writeError(w, http.StatusConflict, "a senha do dono fica no dwnvr.yaml, e se troca lá")
		return
	}
	var req struct {
		Atual string `json:"atual"`
		Nova  string `json:"nova"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	u, err := s.usuarios.TrocarSenha(r.Context(), p.Usuario, req.Atual, req.Nova)
	if r.Context().Err() != nil {
		return // o navegador desistiu na fila
	}
	if err != nil {
		s.recusa(w, "trocando a senha", err)
		return
	}
	novo, ok := s.quem(u.Usuario)
	if !ok {
		s.fail(w, "abrindo a sessão de quem acabou de trocar a senha", errors.New("pessoa sumiu"))
		return
	}
	s.setSessionCookie(w, r, novo)
	writeJSON(w, map[string]any{"ok": true, "pessoa": novo})
	// O aviso de sessão vai depois da resposta: o ao vivo aberto em outra aba
	// deste navegador confere a sessão assim que o aviso cai, e precisa
	// encontrar o cookie novo já guardado.
	_ = http.NewResponseController(w).Flush()
	s.derrubarPessoa(p.Usuario, "senha trocada")
	s.log.Info("senha trocada pela própria pessoa", "usuario", p.Usuario)
}

// handleMudarNome troca o próprio nome de exibição.
func (s *Server) handleMudarNome(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Nome string `json:"nome"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	p := pessoaDe(r)
	if _, err := s.usuarios.MudarNome(p.Usuario, req.Nome); err != nil {
		s.recusa(w, "trocando o nome", err)
		return
	}
	s.respostaDaConta(w, p.Usuario)
}

// handleTrocarAvatar guarda a própria foto. O corpo é o JPEG, já reduzido
// pelo navegador; acima do teto, a leitura para no meio.
func (s *Server) handleTrocarAvatar(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, usuarios.TetoDoAvatar))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("a foto passa de %d KB", usuarios.TetoDoAvatar>>10))
			return
		}
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}
	p := pessoaDe(r)
	if _, err := s.usuarios.TrocarAvatar(p.Usuario, b); err != nil {
		s.recusa(w, "guardando a foto", err)
		return
	}
	s.respostaDaConta(w, p.Usuario)
}

// handleTirarAvatar apaga a própria foto.
func (s *Server) handleTirarAvatar(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	p := pessoaDe(r)
	if err := s.usuarios.TirarAvatar(p.Usuario); err != nil {
		s.recusa(w, "tirando a foto", err)
		return
	}
	s.respostaDaConta(w, p.Usuario)
}

// handleAvatar serve a foto pelo id. Cada um vê a própria; o admin vê todas,
// para a tela Usuários. A foto de outra pessoa responde como a que não
// existe.
//
// O id muda a cada troca, então a resposta pode ficar no navegador para
// sempre: foto nova é URL nova.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	caminho, dono, err := s.usuarios.Avatar(r.URL.Query().Get("id"))
	p := pessoaDe(r)
	if err == nil && !p.admin() && dono != p.Usuario {
		err = usuarios.ErrSemAvatar
	}
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	f, err := os.Open(caminho)
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
