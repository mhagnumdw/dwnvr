package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

// As rotas da tela Usuários (só admin) e as do link de convite (públicas: quem
// abre o link ainda não tem sessão).

// usuarioNaTela é a pessoa como a tela Usuários a vê: sem a senha guardada e
// sem o hash do token.
type usuarioNaTela struct {
	Usuario  string         `json:"usuario"`
	Nome     string         `json:"nome"`
	Papel    usuarios.Papel `json:"papel"`
	Situacao string         `json:"situacao"`
	// Só com link em aberto ou vencido.
	LinkVenceEmMs int64 `json:"linkVenceEmMs,omitempty"`
	CriadoEmMs    int64 `json:"criadoEmMs,omitempty"`
}

func naTela(u usuarios.Usuario, agora time.Time) usuarioNaTela {
	v := usuarioNaTela{Usuario: u.Usuario, Nome: u.Nome, Papel: u.Papel, Situacao: u.Situacao(agora)}
	if u.Link != nil {
		v.LinkVenceEmMs = u.Link.VenceEm.UnixMilli()
	}
	if !u.CriadoEm.IsZero() {
		v.CriadoEmMs = u.CriadoEm.UnixMilli()
	}
	return v
}

// conviteNaTela é a resposta de quem gera um link: a pessoa e o token, que só
// existe nesta resposta. A tela monta o link com o endereço pelo qual o admin
// está acessando.
func conviteNaTela(c usuarios.Convite) map[string]any {
	return map[string]any{"usuario": naTela(c.Usuario, time.Now()), "token": c.Token}
}

// semCadastro recusa o que grava no cadastro quando não há autenticação: sem
// ela não há login, e as pessoas do usuarios.json são ignoradas.
func (s *Server) semCadastro(w http.ResponseWriter) bool {
	if s.cfg.Server.AuthEnabled() {
		return false
	}
	writeError(w, http.StatusConflict,
		"a autenticação está desligada: defina server.username e server.password no dwnvr.yaml")
	return true
}

// recusa traduz o erro do cadastro para a resposta.
func (s *Server) recusa(w http.ResponseWriter, what string, err error) {
	var recusado usuarios.Recusado
	switch {
	case errors.As(err, &recusado):
		writeError(w, http.StatusBadRequest, recusado.Error())
	case errors.Is(err, usuarios.ErrNaoExiste):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, usuarios.ErrLinkInvalido):
		writeError(w, http.StatusGone, err.Error())
	default:
		s.fail(w, what, err)
	}
}

func lerCorpo(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return false
	}
	return true
}

// handleUsuarios lista as pessoas, com a situação de cada uma. O dono vem à
// parte: ele não está no cadastro, e a tela o mostra sem botão nenhum.
func (s *Server) handleUsuarios(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"authRequired": s.cfg.Server.AuthEnabled(),
		"usuarios":     []usuarioNaTela{},
		// Os números do formulário, para a tela não os repetir à mão.
		"validadeDoLinkMs":       usuarios.ValidadeDoLink.Milliseconds(),
		"tamanhoMaximoDoNome":    usuarios.TamanhoMaximoDoNome,
		"tamanhoMaximoDoUsuario": usuarios.TamanhoMaximoDoUsuario,
	}
	if !s.cfg.Server.AuthEnabled() {
		writeJSON(w, resp)
		return
	}
	dono, _ := s.quem(s.cfg.Server.Username)
	resp["dono"] = map[string]string{"usuario": dono.Usuario, "nome": dono.Nome}
	agora := time.Now()
	lista := []usuarioNaTela{}
	for _, u := range s.usuarios.Listar() {
		lista = append(lista, naTela(u, agora))
	}
	resp["usuarios"] = lista
	writeJSON(w, resp)
}

// handleCriarUsuario cria uma pessoa comum, ainda sem senha, e devolve o
// token do link para ela definir a sua.
func (s *Server) handleCriarUsuario(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Usuario string `json:"usuario"`
		Nome    string `json:"nome"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	c, err := s.usuarios.Criar(req.Usuario, req.Nome)
	if err != nil {
		s.recusa(w, "criando o usuário", err)
		return
	}
	s.log.Info("usuário criado pela interface", "usuario", req.Usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, conviteNaTela(c))
}

// handleNovoLink gera outro link para a pessoa. A senha dela deixa de valer,
// o link anterior também, e o que ela tem aberto cai na hora.
func (s *Server) handleNovoLink(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Usuario string `json:"usuario"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	c, err := s.usuarios.NovoLink(req.Usuario)
	if err != nil {
		s.recusa(w, "gerando o link novo", err)
		return
	}
	s.derrubarPessoa(req.Usuario, "link novo")
	s.log.Info("link novo gerado pela interface", "usuario", req.Usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, conviteNaTela(c))
}

// handleRemoverUsuario apaga a pessoa e derruba o que ela tem aberto.
func (s *Server) handleRemoverUsuario(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	usuario := r.URL.Query().Get("usuario")
	if err := s.usuarios.Remover(usuario); err != nil {
		s.recusa(w, "removendo o usuário", err)
		return
	}
	s.derrubarPessoa(usuario, "removido")
	s.log.Info("usuário removido pela interface", "usuario", usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, map[string]bool{"removido": true})
}

// handleConferirConvite diz de quem é o link, para a tela de definir a senha
// mostrar o nome da pessoa antes de ela digitar. O token vem no corpo, e não
// na URL, para não ficar no log de nenhum proxy.
func (s *Server) handleConferirConvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	u, err := s.conviteValido(req.Token)
	if err != nil {
		s.recusa(w, "conferindo o link", err)
		return
	}
	writeJSON(w, map[string]any{
		"usuario":       u.Usuario,
		"nome":          u.Nome,
		"linkVenceEmMs": u.Link.VenceEm.UnixMilli(),
		"senhaMinima":   usuarios.SenhaMinima,
		"senhaMaxima":   usuarios.SenhaMaxima,
	})
}

// conviteValido confere o link. Sem autenticação ligada, link nenhum vale:
// não haveria login para usar a senha definida.
func (s *Server) conviteValido(token string) (usuarios.Usuario, error) {
	if !s.cfg.Server.AuthEnabled() {
		return usuarios.Usuario{}, usuarios.ErrLinkInvalido
	}
	return s.usuarios.Conferir(token)
}

// handleConvite define a senha pelo link e já abre a sessão: a pessoa entra
// direto, sem passar pela tela de login.
func (s *Server) handleConvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Senha string `json:"senha"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	if _, err := s.conviteValido(req.Token); err != nil {
		s.recusa(w, "conferindo o link", err)
		return
	}
	u, err := s.usuarios.DefinirSenha(r.Context(), req.Token, req.Senha)
	if r.Context().Err() != nil {
		return // o navegador desistiu na fila
	}
	if err != nil {
		s.recusa(w, "definindo a senha", err)
		return
	}
	p, ok := s.quem(u.Usuario)
	if !ok {
		s.fail(w, "abrindo a sessão de quem acabou de definir a senha", errors.New("pessoa sumiu"))
		return
	}
	s.log.Info("senha definida pelo link", "usuario", u.Usuario, "de", r.RemoteAddr)
	s.setSessionCookie(w, r, p)
	writeJSON(w, map[string]any{"ok": true, "pessoa": p})
}
