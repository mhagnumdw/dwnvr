package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/email"
	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

// enviadorFalso guarda o que mandaria, ou falha com erro.
type enviadorFalso struct {
	mu       sync.Mutex
	enviadas []email.Mensagem
	erro     error
}

func (e *enviadorFalso) Enviar(_ context.Context, m email.Mensagem) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.erro != nil {
		return e.erro
	}
	e.enviadas = append(e.enviadas, m)
	return nil
}

// comEmail liga o e-mail no servidor de teste e guarda o log, para conferir
// que o link não vai parar nele.
func comEmail(t *testing.T, s *Server) (*enviadorFalso, *bytes.Buffer) {
	t.Helper()
	comAutenticacao(t, s)
	falso := &enviadorFalso{}
	s.envio = falso
	var log bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	return falso, &log
}

// criarComLink cria a pessoa pela API e devolve o link como a tela o monta.
func criarComLink(t *testing.T, s *Server, usuario, emailDela string) string {
	t.Helper()
	rec, resp := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios",
		map[string]string{"username": usuario, "name": "Maria Silva", "email": emailDela})
	if rec.Code != http.StatusOK {
		t.Fatalf("criar: status %d, %s", rec.Code, rec.Body)
	}
	user, _ := resp["user"].(map[string]any)
	if user["email"] != emailDela && emailDela != "" {
		t.Errorf("criada com e-mail %v", user["email"])
	}
	token, _ := resp["token"].(string)
	return "https://nvr.exemplo.com/#convite?token=" + token
}

// Do admin criar a pessoa com e-mail até o link chegar nela.
func TestLinkPorEmail(t *testing.T) {
	s, _ := testServer(t)
	falso, log := comEmail(t, s)
	link := criarComLink(t, s, "maria", "maria@exemplo.com")

	rec, resp := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link})
	if rec.Code != http.StatusOK || resp["sentTo"] != "maria@exemplo.com" {
		t.Fatalf("enviar: status %d, %s", rec.Code, rec.Body)
	}
	if len(falso.enviadas) != 1 {
		t.Fatalf("%d e-mails enviados", len(falso.enviadas))
	}
	m := falso.enviadas[0]
	if len(m.Para) != 1 || m.Para[0] != "maria@exemplo.com" {
		t.Errorf("para %v", m.Para)
	}
	for _, trecho := range []string{"Olá, Maria Silva.", link, "vale 10 minutos", "Seu usuário para entrar: maria"} {
		if !strings.Contains(m.Texto, trecho) {
			t.Errorf("o texto não tem %q:\n%s", trecho, m.Texto)
		}
	}
	token := strings.TrimPrefix(link, "https://nvr.exemplo.com/#convite?token=")
	if strings.Contains(log.String(), token) {
		t.Error("o token foi parar no log")
	}
	if !strings.Contains(log.String(), "link enviado por e-mail") {
		t.Errorf("o envio não foi para o log:\n%s", log)
	}
}

// TestLinkPorEmailConfereOLink: o servidor só manda o link em aberto daquela
// pessoa, para o e-mail dela.
func TestLinkPorEmailConfereOLink(t *testing.T) {
	s, _ := testServer(t)
	falso, _ := comEmail(t, s)
	link := criarComLink(t, s, "maria", "maria@exemplo.com")
	linkDoJoao := criarComLink(t, s, "joao", "")
	token := strings.TrimPrefix(link, "https://nvr.exemplo.com/#convite?token=")

	for _, c := range []struct {
		nome, usuario, url string
		status             int
	}{
		{"link de outra pessoa", "maria", linkDoJoao, http.StatusBadRequest},
		{"pessoa sem e-mail", "joao", linkDoJoao, http.StatusBadRequest},
		{"token inventado", "maria", "https://nvr.exemplo.com/#convite?token=inventado", http.StatusGone},
		{"outro esquema", "maria", "javascript:alert(1)#convite?token=" + token, http.StatusBadRequest},
		{"com login na URL", "maria", "https://x@nvr.exemplo.com/#convite?token=" + token, http.StatusBadRequest},
		{"com query", "maria", "https://nvr.exemplo.com/?a=1#convite?token=" + token, http.StatusBadRequest},
		{"outro fragmento", "maria", "https://nvr.exemplo.com/#conta?token=" + token, http.StatusBadRequest},
		{"token a mais", "maria", link + "&token=outro", http.StatusBadRequest},
	} {
		rec, _ := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
			map[string]string{"username": c.usuario, "url": c.url})
		if rec.Code != c.status {
			t.Errorf("%s: status %d, esperava %d (%s)", c.nome, rec.Code, c.status, rec.Body)
		}
	}
	if len(falso.enviadas) != 0 {
		t.Errorf("%d e-mails saíram de pedidos recusados", len(falso.enviadas))
	}

	// O viewer não manda e-mail nenhum.
	ana := usuarios.Usuario{Usuario: "ana", Nome: "Ana", Papel: usuarios.PapelViewer, Senha: "$guardada$da-ana"}
	if err := s.usuarios.Alterar(func(a *usuarios.Arquivo) error {
		a.Usuarios = append(a.Usuarios, ana)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := pedirComo(t, s, "ana", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link}); rec.Code != http.StatusForbidden {
		t.Errorf("sem ser admin: status %d", rec.Code)
	}
}

// TestFalhaDoServidorDeEmailChegaNaTela: o erro do SMTP volta como 502, com
// o texto dele.
func TestFalhaDoServidorDeEmailChegaNaTela(t *testing.T) {
	s, _ := testServer(t)
	falso, _ := comEmail(t, s)
	link := criarComLink(t, s, "maria", "maria@exemplo.com")
	falso.erro = errors.New("o servidor recusou o login: 535 5.7.8 Username and Password not accepted")

	rec, resp := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link})
	if rec.Code != http.StatusBadGateway || !strings.Contains(resp["error"].(string), "535 5.7.8") {
		t.Errorf("status %d, %s", rec.Code, rec.Body)
	}
	rec, resp = pedirComo(t, s, "admin", http.MethodPost, "/api/email/teste",
		map[string]string{"to": "dono@exemplo.com"})
	if rec.Code != http.StatusBadGateway || !strings.Contains(resp["error"].(string), "535 5.7.8") {
		t.Errorf("teste: status %d, %s", rec.Code, rec.Body)
	}
}

// TestSemEmailAsRotasRecusam: sem a seção smtp, nada sai, e a resposta diz
// por quê.
func TestSemEmailAsRotasRecusam(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s)
	s.cfg.SMTP.Recusa = "smtp.from vazio"
	link := criarComLink(t, s, "maria", "maria@exemplo.com")

	_, resp := pedirComo(t, s, "admin", http.MethodGet, "/api/usuarios", nil)
	if resp["emailEnabled"] != false {
		t.Errorf("emailEnabled %v sem e-mail", resp["emailEnabled"])
	}
	for rota, corpo := range map[string]map[string]string{
		"/api/usuarios/link/email": {"username": "maria", "url": link},
		"/api/email/teste":         {"to": "dono@exemplo.com"},
	} {
		rec, resp := pedirComo(t, s, "admin", http.MethodPost, rota, corpo)
		if rec.Code != http.StatusConflict || !strings.Contains(resp["error"].(string), "smtp.from vazio") {
			t.Errorf("%s: status %d, %s", rota, rec.Code, rec.Body)
		}
	}
}

// TestTesteDeEmail: o teste vai para o endereço digitado, e só para um.
func TestTesteDeEmail(t *testing.T) {
	s, _ := testServer(t)
	falso, _ := comEmail(t, s)
	for _, para := range []string{"", "dono", "Dono <dono@exemplo.com>", "a@exemplo.com, b@exemplo.com"} {
		if rec, _ := pedirComo(t, s, "admin", http.MethodPost, "/api/email/teste",
			map[string]string{"to": para}); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status %d", para, rec.Code)
		}
	}
	rec, resp := pedirComo(t, s, "admin", http.MethodPost, "/api/email/teste",
		map[string]string{"to": " dono@exemplo.com "})
	if rec.Code != http.StatusOK || resp["sentTo"] != "dono@exemplo.com" {
		t.Fatalf("status %d, %s", rec.Code, rec.Body)
	}
	if len(falso.enviadas) != 1 || falso.enviadas[0].Para[0] != "dono@exemplo.com" {
		t.Errorf("enviadas %+v", falso.enviadas)
	}
}

// TestTrocarEmailPelaAPI: troca, recusa o repetido e apaga.
func TestTrocarEmailPelaAPI(t *testing.T) {
	s, _ := testServer(t)
	comEmail(t, s)
	criarComLink(t, s, "maria", "maria@exemplo.com")
	criarComLink(t, s, "joao", "")

	trocar := func(usuario, e string) (int, map[string]any) {
		rec, resp := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios/email",
			map[string]string{"username": usuario, "email": e})
		return rec.Code, resp
	}
	if code, _ := trocar("joao", "Maria@Exemplo.com"); code != http.StatusBadRequest {
		t.Errorf("e-mail repetido: status %d", code)
	}
	if code, resp := trocar("joao", "joao@exemplo.com"); code != http.StatusOK ||
		resp["user"].(map[string]any)["email"] != "joao@exemplo.com" {
		t.Errorf("trocar: status %d, %v", code, resp)
	}
	if code, resp := trocar("joao", ""); code != http.StatusOK || resp["user"].(map[string]any)["email"] != nil {
		t.Errorf("apagar: status %d, %v", code, resp)
	}
	if code, _ := trocar("ninguem", "x@exemplo.com"); code != http.StatusNotFound {
		t.Errorf("pessoa que não existe: status %d", code)
	}
}

// TestSmtpRecusadoViraAviso: o /api/health leva o motivo, para o card Avisos
// e o sino; sem problema, o campo nem vem.
func TestSmtpRecusadoViraAviso(t *testing.T) {
	s, _ := testServer(t)
	_, resp := pedirComo(t, s, "", http.MethodGet, "/api/health", nil)
	if _, tem := resp["smtp"]; tem {
		t.Errorf("smtp no health sem problema nenhum: %v", resp["smtp"])
	}
	s.cfg.SMTP.Recusa = `smtp.security "ssl" fora do aceito`
	_, resp = pedirComo(t, s, "", http.MethodGet, "/api/health", nil)
	smtp, _ := resp["smtp"].(map[string]any)
	if smtp["error"] != s.cfg.SMTP.Recusa {
		t.Errorf("smtp %v", resp["smtp"])
	}
}

// TestAlertsEmailViraAviso: o problema da seção alerts vai ao /api/health
// mesmo sem câmera com alerta, porque é da configuração.
func TestAlertsEmailViraAviso(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Alerts.Email.Avisos = []string{"falta a seção smtp, e sem ela a lista alerts.email.to não recebe nada"}
	_, resp := pedirComo(t, s, "", http.MethodGet, "/api/health", nil)
	alerts, _ := resp["alerts"].(map[string]any)
	if w, _ := alerts["warnings"].([]any); len(w) != 1 || w[0] != s.cfg.Alerts.Email.Avisos[0] {
		t.Errorf("alerts %v", resp["alerts"])
	}
	if _, tem := alerts["total"]; tem {
		t.Errorf("total sem câmera com alerta: %v", alerts)
	}
}

// TestDiagnosticoMostraOServidorSemASenha: o card E-mail vê o servidor e o
// remetente, nunca o login nem a senha.
func TestDiagnosticoMostraOServidorSemASenha(t *testing.T) {
	s, _ := testServer(t)
	login, senha := "conta-do-login", "troque-me"
	s.cfg.SMTP.Host, s.cfg.SMTP.Port, s.cfg.SMTP.Security = "smtp.exemplo.com", 587, "starttls"
	s.cfg.SMTP.Username = login
	s.cfg.SMTP.Password = senha
	s.cfg.SMTP.From = "dwnvr <dwnvr@exemplo.com>"
	rec, resp := pedirComo(t, s, "", http.MethodGet, "/api/health/servidor", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	smtp, _ := resp["smtp"].(map[string]any)
	if smtp["host"] != "smtp.exemplo.com" || smtp["from"] != "dwnvr <dwnvr@exemplo.com>" {
		t.Errorf("smtp %v", resp["smtp"])
	}
	if b := rec.Body.String(); strings.Contains(b, senha) || strings.Contains(b, login) {
		t.Error("o login ou a senha foram parar na resposta")
	}
}
