package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

const (
	sessionCookie = "dwnvr_session"
	sessionTTL    = 30 * 24 * time.Hour

	// Faltando menos que isto para vencer, a sessão em uso ganha um cookie
	// novo, com o prazo inteiro. Assim os 30 dias contam da última vez que a
	// tela falou com o dwnvr, e não do login: quem usa o app instalado não é
	// mandado de volta para a tela de login a cada mês.
	sessionRenew = sessionTTL / 2
)

// pessoa é quem fez a requisição: o dono, do dwnvr.yaml, ou alguém do
// usuarios.json.
type pessoa struct {
	Usuario string         `json:"usuario,omitempty"`
	Nome    string         `json:"nome,omitempty"`
	Papel   usuarios.Papel `json:"papel"`
	// Dono é a conta do dwnvr.yaml. A senha dele se troca lá, e não pela tela.
	Dono bool `json:"dono"`
	// Avatar é o id da foto (ver GET /api/avatar), ou vazio.
	Avatar string `json:"avatar,omitempty"`

	// credencial é o que amarra o cookie à senha da pessoa (ver chaveDaSessao):
	// a senha do dono, ou o valor guardado no usuarios.json.
	credencial string
}

func (p pessoa) admin() bool { return p.Papel == usuarios.PapelAdmin }

// semAutenticacao é quem usa o dwnvr com a autenticação desligada: não há
// usuários, e quem alcança a tela faz tudo.
var semAutenticacao = pessoa{Papel: usuarios.PapelAdmin, Dono: true}

// quem acha a pessoa pelo login. Quem ainda não definiu a senha fica de fora:
// não há credencial para amarrar uma sessão.
func (s *Server) quem(usuario string) (pessoa, bool) {
	if usuario == s.cfg.Server.Username {
		dono := s.usuarios.Dono()
		nome := dono.Nome
		if nome == "" {
			nome = usuario
		}
		return pessoa{Usuario: usuario, Nome: nome, Papel: usuarios.PapelAdmin, Dono: true,
			Avatar: dono.Avatar, credencial: s.cfg.Server.Password}, true
	}
	u, ok := s.usuarios.Buscar(usuario)
	if !ok || u.Senha == "" {
		return pessoa{}, false
	}
	return pessoa{Usuario: u.Usuario, Nome: u.Nome, Papel: u.Papel, Avatar: u.Avatar,
		credencial: u.Senha}, true
}

// chaveDaSessao é a chave que assina os cookies de uma pessoa. Sai do
// .session-secret, do login e da credencial dela: trocar a senha, ou gerar um
// link novo, muda a chave, e todo cookie dela emitido antes deixa de bater com
// a assinatura. Só ela volta para a tela de login; os outros seguem. Sem isso,
// trocar a senha não tiraria ninguém, e a sessão em uso, que se renova, não
// venceria nunca.
//
// Reiniciar com a mesma credencial dá a mesma chave e não derruba ninguém.
// Derrubar todo mundo de uma vez é apagar o .session-secret.
func (s *Server) chaveDaSessao(p pessoa) []byte {
	mac := hmac.New(sha256.New, s.sessionSecret)
	mac.Write([]byte(p.Usuario + "\x00" + p.credencial))
	return mac.Sum(nil)
}

// O token de sessão é "<login>.<expiraEmUnix>.<hmac>", com o login em base64
// (ele pode ter ponto). Não há estado no servidor: a assinatura basta para
// validar, o que evita manter uma tabela de sessões viva num dispositivo com
// 1,5 GB de RAM.
func (s *Server) signToken(p pessoa, expires int64) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(p.Usuario)) + "." +
		strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, s.chaveDaSessao(p))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// tokenPessoa devolve de quem é o token e até quando ele vale, e ok=false se
// ele estiver vencido, malformado, com assinatura que não bate, ou se a
// pessoa não existir mais.
func (s *Server) tokenPessoa(tok string) (p pessoa, expires int64, ok bool) {
	before, after, ok0 := strings.CutLast(tok, ".")
	if !ok0 {
		return pessoa{}, 0, false
	}
	payload, sig := before, after
	login, prazo, ok := strings.Cut(payload, ".")
	if !ok {
		return pessoa{}, 0, false
	}
	usuario, err := base64.RawURLEncoding.DecodeString(login)
	if err != nil {
		return pessoa{}, 0, false
	}
	expires, err = strconv.ParseInt(prazo, 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return pessoa{}, 0, false
	}
	p, ok = s.quem(string(usuario))
	if !ok {
		return pessoa{}, 0, false
	}

	mac := hmac.New(sha256.New, s.chaveDaSessao(p))
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return pessoa{}, 0, false
	}
	return p, expires, true
}

// sessao diz quem é a pessoa da requisição, se a sessão for válida. Se ela já
// passou da metade do prazo, a resposta leva um cookie novo (ver
// sessionRenew).
//
// Não cria estado no servidor: renovar é só assinar outro prazo.
func (s *Server) sessao(w http.ResponseWriter, r *http.Request) (pessoa, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return pessoa{}, false
	}
	p, expires, ok := s.tokenPessoa(c.Value)
	if !ok {
		return pessoa{}, false
	}
	if time.Until(time.Unix(expires, 0)) < sessionRenew {
		s.setSessionCookie(w, r, p)
	}
	return p, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, p pessoa) {
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

type chavePessoa struct{}

// pessoaDe devolve quem fez a requisição. Só vale dentro de requireAuth e
// requireAdmin, que a põem no contexto.
func pessoaDe(r *http.Request) pessoa {
	p, _ := r.Context().Value(chavePessoa{}).(pessoa)
	return p
}

// requireAuth embrulha um handler exigindo sessão válida, de qualquer papel.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return s.exigir(false, next)
}

// requireAdmin embrulha um handler exigindo sessão de admin. Esconder a aba
// na tela não é segurança: é aqui que o usuário comum é recusado.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.exigir(true, next)
}

func (s *Server) exigir(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := semAutenticacao
		if s.cfg.Server.AuthEnabled() {
			var ok bool
			if p, ok = s.sessao(w, r); !ok {
				writeError(w, http.StatusUnauthorized, "não autenticado")
				return
			}
		}
		if admin && !p.admin() {
			writeError(w, http.StatusForbidden, "só o administrador")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), chavePessoa{}, p)))
	}
}

// handleLogin confere a senha e abre a sessão.
//
// Toda tentativa paga uma conta de senha inteira (ver usuarios.ConferirSenha),
// na fila de uma por vez, inclusive a do dono, cuja senha está em texto no
// dwnvr.yaml, e a de um usuário que não existe. Assim o tempo de resposta não
// diz quem existe nem quem é o dono.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	p, existe := s.quem(req.Username)
	guardado := ""
	if existe && !p.Dono {
		guardado = p.credencial
	}
	ok, err := usuarios.ConferirSenha(r.Context(), req.Password, guardado)
	if r.Context().Err() != nil {
		return // o navegador desistiu na fila
	}
	if err != nil {
		s.log.Error("senha guardada ilegível no usuarios.json", "usuario", req.Username, "erro", err)
	}
	if existe && p.Dono {
		ok = subtle.ConstantTimeCompare([]byte(req.Password), []byte(p.credencial)) == 1
	}
	if !existe || !ok {
		s.log.Warn("tentativa de login recusada", "usuario", req.Username, "de", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "usuário ou senha inválidos")
		return
	}

	s.setSessionCookie(w, r, p)
	writeJSON(w, map[string]any{"ok": true, "pessoa": p})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: viaHTTPS(r),
	})
	writeJSON(w, map[string]any{"ok": true})
}

// viaHTTPS diz se o navegador fala com o dwnvr por HTTPS, e é o que liga o
// Secure do cookie de sessão.
//
// O dwnvr só serve HTTP, e a instalação típica é HTTP na LAN, onde um cookie
// Secure seria descartado em silêncio e o login nunca pegaria. O HTTPS vem de
// um proxy TLS na frente, e o `tailscale serve` e o Caddy avisam pelo
// X-Forwarded-Proto. Aí o cookie sai Secure e não vaza numa requisição em HTTP
// para o mesmo endereço.
//
// Confiar no header não abre nada, porque ele só decide o Secure: um cliente em
// HTTP que o forje recebe um cookie que o próprio navegador recusa.
//
// Com o proxy e a porta HTTP no mesmo nome de host, depois de um login por
// HTTPS o navegador não deixa o HTTP gravar um cookie com o mesmo nome. Quem
// montar assim precisa entrar sempre pelo mesmo endereço.
func viaHTTPS(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// handleSession diz ao frontend se precisa mostrar a tela de login e, com a
// sessão válida, quem está logado. É a primeira chamada quando a tela abre, e
// por isso também renova a sessão.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"authRequired": s.cfg.Server.AuthEnabled()}
	p, ok := semAutenticacao, true
	if s.cfg.Server.AuthEnabled() {
		p, ok = s.sessao(w, r)
	}
	resp["authenticated"] = ok
	if ok {
		resp["pessoa"] = p
	}
	writeJSON(w, resp)
}
