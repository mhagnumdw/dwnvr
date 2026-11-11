package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/users"
)

// As rotas da tela Usuários (só admin) e as do link de convite (públicas: quem
// abre o link ainda não tem sessão).

// usuarioNaTela é a pessoa como a tela Usuários a vê: sem a senha guardada e
// sem o hash do token.
type usuarioNaTela struct {
	Usuario string `json:"username"`
	Nome    string `json:"name"`
	// Só de quem tem e-mail válido: o escrito errado à mão no arquivo não
	// aparece, como se não houvesse.
	Email    string     `json:"email,omitempty"`
	Papel    users.Role `json:"role"`
	Situacao string     `json:"status"`
	Avatar   string     `json:"avatar,omitempty"`
	// Só com link em aberto ou vencido.
	LinkVenceEmMs int64 `json:"inviteExpiresAtMs,omitempty"`
	CriadoEmMs    int64 `json:"createdAtMs,omitempty"`
	// Só com a senha definida pelo link, e não em quem a definiu antes de o
	// campo existir.
	AtivoDesdeMs int64 `json:"activeSinceMs,omitempty"`
	// Só de quem já usou o dwnvr depois de o campo existir, em qualquer
	// situação: quem ganhou link novo continua com o acesso de antes.
	UltimoAcessoMs int64 `json:"lastAccessMs,omitempty"`
}

// donoNaTela é o dono como a tela Usuários o vê. Ele não está no cadastro, e a
// tela o mostra sem botão nenhum.
type donoNaTela struct {
	Usuario        string `json:"username"`
	Nome           string `json:"name"`
	Avatar         string `json:"avatar"`
	UltimoAcessoMs int64  `json:"lastAccessMs,omitempty"`
}

// ms é o instante em milissegundos, ou 0 para o instante zero, que o
// omitempty some.
func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func (s *Server) naTela(u users.User, agora time.Time) usuarioNaTela {
	v := usuarioNaTela{Usuario: u.Username, Nome: u.Name, Email: u.ValidEmail(), Papel: u.Role,
		Situacao: u.Status(agora), Avatar: u.Avatar}
	// Acesso de antes da criação da conta é de quem teve o mesmo usuário
	// antes. O Esquecer do Criar só chega ao disco na gravação seguinte, e uma
	// queda nesse meio-tempo traria o acesso antigo de volta no boot.
	if ultimo := s.acessos.Last(u.Username); !ultimo.Before(u.CreatedAt) {
		v.UltimoAcessoMs = ms(ultimo)
	}
	if u.Link != nil {
		v.LinkVenceEmMs = u.Link.ExpiresAt.UnixMilli()
	}
	if !u.CreatedAt.IsZero() {
		v.CriadoEmMs = u.CreatedAt.UnixMilli()
	}
	if !u.ActiveSince.IsZero() && v.Situacao == "active" {
		v.AtivoDesdeMs = u.ActiveSince.UnixMilli()
	}
	return v
}

// conviteNaTela é a resposta de quem gera um link: a pessoa e o token, que só
// existe nesta resposta. A tela monta o link com o endereço pelo qual o admin
// está acessando.
func (s *Server) conviteNaTela(c users.Invite) map[string]any {
	return map[string]any{"user": s.naTela(c.User, time.Now()), "token": c.Token}
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

// recusa traduz o erro do cadastro para a resposta. As Recusas de um
// formulário vão campo a campo em "fields", para a tela pôr cada mensagem
// embaixo do seu campo, e a primeira também em "error", como em toda recusa.
func (s *Server) recusa(w http.ResponseWriter, what string, err error) {
	var recusado users.ValidationError
	var recusas users.FieldErrors
	switch {
	case errors.As(err, &recusas):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": recusas.Error(), "fields": recusas})
	case errors.As(err, &recusado):
		writeError(w, http.StatusBadRequest, recusado.Error())
	case errors.Is(err, users.ErrNotFound), errors.Is(err, users.ErrNoAvatar):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, users.ErrInvalidLink):
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
		"users":        []usuarioNaTela{},
		// Os números do formulário, para a tela não os repetir à mão.
		"inviteValidityMs":  users.InviteValidity.Milliseconds(),
		"maxNameLength":     users.MaxNameLen,
		"maxUsernameLength": users.MaxUsernameLen,
		"maxEmailLength":    users.MaxEmailLen,
		// Sem a seção smtp ligada, a tela esconde o envio por e-mail. O
		// e-mail da pessoa ela mostra igual, porque serve para entrar.
		"emailEnabled": s.envio != nil,
	}
	if !s.cfg.Server.AuthEnabled() {
		writeJSON(w, resp)
		return
	}
	dono, _ := s.quem(s.cfg.Server.Username)
	resp["owner"] = donoNaTela{Usuario: dono.Usuario, Nome: dono.Nome, Avatar: dono.Avatar,
		UltimoAcessoMs: ms(s.acessos.Last(dono.Usuario))}
	agora := time.Now()
	lista := []usuarioNaTela{}
	for _, u := range s.usuarios.List() {
		lista = append(lista, s.naTela(u, agora))
	}
	resp["users"] = lista
	writeJSON(w, resp)
}

// handleCriarUsuario cria um viewer, ainda sem senha, e devolve o
// token do link para ela definir a sua. O e-mail é opcional.
func (s *Server) handleCriarUsuario(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Usuario string `json:"username"`
		Nome    string `json:"name"`
		Email   string `json:"email"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	c, err := s.usuarios.Create(req.Usuario, req.Nome, req.Email)
	if err != nil {
		s.recusa(w, "criando o usuário", err)
		return
	}
	// Um login reaproveitado não herda o acesso de quem o teve antes.
	s.acessos.Forget(req.Usuario)
	s.log.Info("usuário criado pela interface", "usuario", req.Usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, s.conviteNaTela(c))
}

// handleConferirNovo diz o que o POST /api/usuarios recusaria com o mesmo
// corpo, sem criar ninguém: a tela confere cada campo quando o admin sai dele.
// Responde 200 com "fields" vazio quando está tudo certo.
func (s *Server) handleConferirNovo(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Usuario string `json:"username"`
		Nome    string `json:"name"`
		Email   string `json:"email"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	recusas, err := s.usuarios.CheckNew(req.Usuario, req.Nome, req.Email)
	if err != nil {
		s.recusa(w, "conferindo o usuário novo", err)
		return
	}
	writeJSON(w, map[string]any{"fields": recusas})
}

// handleNovoLink gera outro link para a pessoa. A senha dela deixa de valer,
// o link anterior também, e o que ela tem aberto cai na hora.
func (s *Server) handleNovoLink(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Usuario string `json:"username"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	c, err := s.usuarios.NewLink(req.Usuario)
	if err != nil {
		s.recusa(w, "gerando o link novo", err)
		return
	}
	s.derrubarPessoa(req.Usuario, "link novo")
	s.log.Info("link novo gerado pela interface", "usuario", req.Usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, s.conviteNaTela(c))
}

// handleTrocarEmail troca o e-mail de alguém, ou o apaga, com email vazio.
// Vale também com o e-mail desligado: guardar não envia nada.
func (s *Server) handleTrocarEmail(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Usuario string `json:"username"`
		Email   string `json:"email"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	u, err := s.usuarios.SetEmail(req.Usuario, req.Email)
	if err != nil {
		s.recusa(w, "trocando o e-mail", err)
		return
	}
	s.log.Info("e-mail trocado pela interface", "usuario", req.Usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, map[string]any{"user": s.naTela(u, time.Now())})
}

// handleConferirEmail diz o que o POST /api/usuarios/email recusaria com o
// mesmo corpo, sem trocar nada. Responde 200 com "fields" vazio quando está
// tudo certo.
func (s *Server) handleConferirEmail(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	var req struct {
		Usuario string `json:"username"`
		Email   string `json:"email"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	recusas, err := s.usuarios.CheckEmail(req.Usuario, req.Email)
	if err != nil {
		s.recusa(w, "conferindo o e-mail", err)
		return
	}
	writeJSON(w, map[string]any{"fields": recusas})
}

// handleEnviarLink manda por e-mail o link que está na tela do admin. O
// servidor não sabe por qual endereço é acessado, então o link vem pronto da
// tela, que o monta com o endereço do navegador do admin; aqui ele só é
// conferido, e tem de ser o link em aberto daquela pessoa. O log diz para
// quem foi, nunca o link: o token é a credencial.
func (s *Server) handleEnviarLink(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) || s.semEmail(w) {
		return
	}
	var req struct {
		Usuario string `json:"username"`
		Link    string `json:"url"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	token, ok := tokenDoLink(req.Link)
	if !ok {
		writeError(w, http.StatusBadRequest, "link inválido: mande o link como a tela o mostra")
		return
	}
	u, err := s.conviteValido(token)
	if err != nil {
		s.recusa(w, "conferindo o link", err)
		return
	}
	if u.Username != req.Usuario {
		writeError(w, http.StatusBadRequest, "o link não é desta pessoa")
		return
	}
	para := u.ValidEmail()
	if para == "" {
		writeError(w, http.StatusBadRequest, u.Name+" não tem e-mail cadastrado")
		return
	}
	if err := s.envio.Send(r.Context(), mensagemDoConvite(u, para, req.Link)); err != nil {
		s.log.Warn("o link não foi por e-mail", "usuario", u.Username, "por", pessoaDe(r).Usuario, "erro", err)
		writeError(w, http.StatusBadGateway, "o e-mail não foi: "+err.Error())
		return
	}
	s.log.Info("link enviado por e-mail", "usuario", u.Username, "por", pessoaDe(r).Usuario)
	writeJSON(w, map[string]string{"sentTo": para})
}

// handleRemoverUsuario apaga a pessoa e derruba o que ela tem aberto.
func (s *Server) handleRemoverUsuario(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	usuario := r.URL.Query().Get("username")
	if err := s.usuarios.Remove(usuario); err != nil {
		s.recusa(w, "removendo o usuário", err)
		return
	}
	s.derrubarPessoa(usuario, "removido")
	s.acessos.Forget(usuario)
	s.log.Info("usuário removido pela interface", "usuario", usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, map[string]bool{"removed": true})
}

// handleTirarAvatarDe tira a foto de alguém: o admin não troca a foto de
// ninguém, mas pode tirar uma que não sirva.
func (s *Server) handleTirarAvatarDe(w http.ResponseWriter, r *http.Request) {
	if s.semCadastro(w) {
		return
	}
	usuario := r.URL.Query().Get("username")
	if err := s.usuarios.RemoveAvatar(usuario); err != nil {
		s.recusa(w, "tirando a foto", err)
		return
	}
	s.log.Info("foto tirada pela interface", "usuario", usuario, "por", pessoaDe(r).Usuario)
	writeJSON(w, map[string]bool{"ok": true})
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
		"username":          u.Username,
		"name":              u.Name,
		"inviteExpiresAtMs": u.Link.ExpiresAt.UnixMilli(),
		"minPasswordLength": users.MinPasswordLen,
		"maxPasswordLength": users.MaxPasswordLen,
	})
}

// conviteValido confere o link. Sem autenticação ligada, link nenhum vale:
// não haveria login para usar a senha definida.
func (s *Server) conviteValido(token string) (users.User, error) {
	if !s.cfg.Server.AuthEnabled() {
		return users.User{}, users.ErrInvalidLink
	}
	return s.usuarios.CheckInvite(token)
}

// handleConvite define a senha pelo link e já abre a sessão: a pessoa entra
// direto, sem passar pela tela de login.
func (s *Server) handleConvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Senha string `json:"password"`
	}
	if !lerCorpo(w, r, &req) {
		return
	}
	if _, err := s.conviteValido(req.Token); err != nil {
		s.recusa(w, "conferindo o link", err)
		return
	}
	u, err := s.usuarios.SetPassword(r.Context(), req.Token, req.Senha)
	if r.Context().Err() != nil {
		return // o navegador desistiu na fila
	}
	if err != nil {
		s.recusa(w, "definindo a senha", err)
		return
	}
	p, ok := s.quem(u.Username)
	if !ok {
		s.fail(w, "abrindo a sessão de quem acabou de definir a senha", errors.New("pessoa sumiu"))
		return
	}
	s.log.Info("senha definida pelo link", "usuario", u.Username, "de", r.RemoteAddr, encaminhado(r))
	s.setSessionCookie(w, r, p)
	s.acessos.Record(u.Username)
	writeJSON(w, map[string]any{"ok": true, "user": p})
}
