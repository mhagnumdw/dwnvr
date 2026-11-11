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
	"github.com/mhagnumdw/dwnvr/internal/users"
)

// enviadorFalso guarda o que mandaria, ou falha com erro.
type fakeSender struct {
	mu      sync.Mutex
	sent    []email.Message
	errText error
}

func (e *fakeSender) Send(_ context.Context, m email.Message) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errText != nil {
		return e.errText
	}
	e.sent = append(e.sent, m)
	return nil
}

// comEmail liga o e-mail no servidor de teste e guarda o log, para conferir
// que o link não vai parar nele.
func comEmail(t *testing.T, s *Server) (*fakeSender, *bytes.Buffer) {
	t.Helper()
	withAuth(t, s)
	fake := &fakeSender{}
	s.sending = fake
	var log bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	return fake, &log
}

// criarComLink cria a pessoa pela API e devolve o link como a tela o monta.
func createWithLink(t *testing.T, s *Server, username, herEmail string) string {
	t.Helper()
	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios",
		map[string]string{"username": username, "name": "Maria Silva", "email": herEmail})
	if rec.Code != http.StatusOK {
		t.Fatalf("criar: status %d, %s", rec.Code, rec.Body)
	}
	user, _ := resp["user"].(map[string]any)
	if user["email"] != herEmail && herEmail != "" {
		t.Errorf("criada com e-mail %v", user["email"])
	}
	token, _ := resp["token"].(string)
	return "https://nvr.exemplo.com/#convite?token=" + token
}

// Do admin criar a pessoa com e-mail até o link chegar nela.
func TestLinkByEmail(t *testing.T) {
	s, _ := testServer(t)
	fake, log := comEmail(t, s)
	link := createWithLink(t, s, "maria", "maria@exemplo.com")

	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link})
	if rec.Code != http.StatusOK || resp["sentTo"] != "maria@exemplo.com" {
		t.Fatalf("enviar: status %d, %s", rec.Code, rec.Body)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("%d e-mails enviados", len(fake.sent))
	}
	m := fake.sent[0]
	if len(m.To) != 1 || m.To[0] != "maria@exemplo.com" {
		t.Errorf("para %v", m.To)
	}
	for _, stretch := range []string{"Olá, Maria Silva.", link, "vale 10 minutos", "Seu usuário para entrar: maria"} {
		if !strings.Contains(m.Text, stretch) {
			t.Errorf("o texto não tem %q:\n%s", stretch, m.Text)
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
func TestLinkByEmailChecksTheLink(t *testing.T) {
	s, _ := testServer(t)
	fake, _ := comEmail(t, s)
	link := createWithLink(t, s, "maria", "maria@exemplo.com")
	joaosLink := createWithLink(t, s, "joao", "")
	token := strings.TrimPrefix(link, "https://nvr.exemplo.com/#convite?token=")

	for _, c := range []struct {
		nome, username, url string
		status              int
	}{
		{"link de outra pessoa", "maria", joaosLink, http.StatusBadRequest},
		{"pessoa sem e-mail", "joao", joaosLink, http.StatusBadRequest},
		{"token inventado", "maria", "https://nvr.exemplo.com/#convite?token=inventado", http.StatusGone},
		{"outro esquema", "maria", "javascript:alert(1)#convite?token=" + token, http.StatusBadRequest},
		{"com login na URL", "maria", "https://x@nvr.exemplo.com/#convite?token=" + token, http.StatusBadRequest},
		{"com query", "maria", "https://nvr.exemplo.com/?a=1#convite?token=" + token, http.StatusBadRequest},
		{"outro fragmento", "maria", "https://nvr.exemplo.com/#conta?token=" + token, http.StatusBadRequest},
		{"token a mais", "maria", link + "&token=outro", http.StatusBadRequest},
	} {
		rec, _ := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
			map[string]string{"username": c.username, "url": c.url})
		if rec.Code != c.status {
			t.Errorf("%s: status %d, esperava %d (%s)", c.nome, rec.Code, c.status, rec.Body)
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("%d e-mails saíram de pedidos recusados", len(fake.sent))
	}

	// O viewer não manda e-mail nenhum.
	ana := users.User{Username: "ana", Name: "Ana", Role: users.RoleViewer, PasswordHash: "$guardada$da-ana"}
	if err := s.users.Update(func(a *users.FileData) error {
		a.Users = append(a.Users, ana)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := requestAs(t, s, "ana", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link}); rec.Code != http.StatusForbidden {
		t.Errorf("sem ser admin: status %d", rec.Code)
	}
}

// TestFalhaDoServidorDeEmailChegaNaTela: o erro do SMTP volta como 502, com
// o texto dele.
func TestEmailServerFailureReachesTheScreen(t *testing.T) {
	s, _ := testServer(t)
	fake, _ := comEmail(t, s)
	link := createWithLink(t, s, "maria", "maria@exemplo.com")
	fake.errText = errors.New("o servidor recusou o login: 535 5.7.8 Username and Password not accepted")

	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link})
	if rec.Code != http.StatusBadGateway || !strings.Contains(resp["error"].(string), "535 5.7.8") {
		t.Errorf("status %d, %s", rec.Code, rec.Body)
	}
	rec, resp = requestAs(t, s, "admin", http.MethodPost, "/api/email/teste",
		map[string]string{"to": "dono@exemplo.com"})
	if rec.Code != http.StatusBadGateway || !strings.Contains(resp["error"].(string), "535 5.7.8") {
		t.Errorf("teste: status %d, %s", rec.Code, rec.Body)
	}
}

// TestSemEmailAsRotasRecusam: sem a seção smtp, nada sai, e a resposta diz
// por quê.
func TestWithoutEmailTheRoutesReject(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s)
	s.cfg.SMTP.Problem = "smtp.from vazio"
	link := createWithLink(t, s, "maria", "maria@exemplo.com")

	_, resp := requestAs(t, s, "admin", http.MethodGet, "/api/usuarios", nil)
	if resp["emailEnabled"] != false {
		t.Errorf("emailEnabled %v sem e-mail", resp["emailEnabled"])
	}
	for rota, body := range map[string]map[string]string{
		"/api/usuarios/link/email": {"username": "maria", "url": link},
		"/api/email/teste":         {"to": "dono@exemplo.com"},
	} {
		rec, resp := requestAs(t, s, "admin", http.MethodPost, rota, body)
		if rec.Code != http.StatusConflict || !strings.Contains(resp["error"].(string), "smtp.from vazio") {
			t.Errorf("%s: status %d, %s", rota, rec.Code, rec.Body)
		}
	}
}

// TestTesteDeEmail: o teste vai para o endereço digitado, e só para um.
func TestEmailTest(t *testing.T) {
	s, _ := testServer(t)
	fake, _ := comEmail(t, s)
	for _, para := range []string{"", "dono", "Dono <dono@exemplo.com>", "a@exemplo.com, b@exemplo.com"} {
		if rec, _ := requestAs(t, s, "admin", http.MethodPost, "/api/email/teste",
			map[string]string{"to": para}); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status %d", para, rec.Code)
		}
	}
	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/email/teste",
		map[string]string{"to": " dono@exemplo.com "})
	if rec.Code != http.StatusOK || resp["sentTo"] != "dono@exemplo.com" {
		t.Fatalf("status %d, %s", rec.Code, rec.Body)
	}
	if len(fake.sent) != 1 || fake.sent[0].To[0] != "dono@exemplo.com" {
		t.Errorf("enviadas %+v", fake.sent)
	}
}

// TestTrocarEmailPelaAPI: troca, recusa o repetido e apaga.
func TestChangeEmailThroughTheAPI(t *testing.T) {
	s, _ := testServer(t)
	comEmail(t, s)
	createWithLink(t, s, "maria", "maria@exemplo.com")
	createWithLink(t, s, "joao", "")

	swap := func(username, e string) (int, map[string]any) {
		rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/email",
			map[string]string{"username": username, "email": e})
		return rec.Code, resp
	}
	if code, _ := swap("joao", "Maria@Exemplo.com"); code != http.StatusBadRequest {
		t.Errorf("e-mail repetido: status %d", code)
	}
	if code, resp := swap("joao", "joao@exemplo.com"); code != http.StatusOK ||
		resp["user"].(map[string]any)["email"] != "joao@exemplo.com" {
		t.Errorf("trocar: status %d, %v", code, resp)
	}
	if code, resp := swap("joao", ""); code != http.StatusOK || resp["user"].(map[string]any)["email"] != nil {
		t.Errorf("apagar: status %d, %v", code, resp)
	}
	if code, _ := swap("ninguem", "x@exemplo.com"); code != http.StatusNotFound {
		t.Errorf("pessoa que não existe: status %d", code)
	}
}

// TestSmtpRecusadoViraAviso: o /api/health leva o motivo, para o card Avisos
// e o sino; sem problema, o campo nem vem.
func TestRejectedSmtpBecomesWarning(t *testing.T) {
	s, _ := testServer(t)
	_, resp := requestAs(t, s, "", http.MethodGet, "/api/health", nil)
	if _, has := resp["smtp"]; has {
		t.Errorf("smtp no health sem problema nenhum: %v", resp["smtp"])
	}
	s.cfg.SMTP.Problem = `smtp.security "ssl" fora do aceito`
	_, resp = requestAs(t, s, "", http.MethodGet, "/api/health", nil)
	smtp, _ := resp["smtp"].(map[string]any)
	if smtp["error"] != s.cfg.SMTP.Problem {
		t.Errorf("smtp %v", resp["smtp"])
	}
}

// TestAlertsEmailViraAviso: o problema da seção alerts vai ao /api/health
// mesmo sem câmera com alerta, porque é da configuração.
func TestAlertsEmailBecomesWarning(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Alerts.Email.Warnings = []string{"falta a seção smtp, e sem ela a lista alerts.email.to não recebe nada"}
	_, resp := requestAs(t, s, "", http.MethodGet, "/api/health", nil)
	alerts, _ := resp["alerts"].(map[string]any)
	if w, _ := alerts["warnings"].([]any); len(w) != 1 || w[0] != s.cfg.Alerts.Email.Warnings[0] {
		t.Errorf("alerts %v", resp["alerts"])
	}
	if _, has := alerts["total"]; has {
		t.Errorf("total sem câmera com alerta: %v", alerts)
	}
}

// TestDiagnosticoMostraOServidorSemASenha: o card E-mail vê o servidor e o
// remetente, nunca o login nem a senha.
func TestDiagnosticsShowsTheServerWithoutThePassword(t *testing.T) {
	s, _ := testServer(t)
	login, password := "conta-do-login", "troque-me"
	s.cfg.SMTP.Host, s.cfg.SMTP.Port, s.cfg.SMTP.Security = "smtp.exemplo.com", 587, "starttls"
	s.cfg.SMTP.Username = login
	s.cfg.SMTP.Password = password
	s.cfg.SMTP.From = "dwnvr <dwnvr@exemplo.com>"
	rec, resp := requestAs(t, s, "", http.MethodGet, "/api/health/servidor", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	smtp, _ := resp["smtp"].(map[string]any)
	if smtp["host"] != "smtp.exemplo.com" || smtp["from"] != "dwnvr <dwnvr@exemplo.com>" {
		t.Errorf("smtp %v", resp["smtp"])
	}
	if b := rec.Body.String(); strings.Contains(b, password) || strings.Contains(b, login) {
		t.Error("o login ou a senha foram parar na resposta")
	}
}
