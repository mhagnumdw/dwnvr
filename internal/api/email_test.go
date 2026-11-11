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

// fakeSender keeps what it would send, or fails with an error.
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

// comEmail turns email on in the test server and keeps the log, to check that
// the link does not end up in it.
func comEmail(t *testing.T, s *Server) (*fakeSender, *bytes.Buffer) {
	t.Helper()
	withAuth(t, s)
	fake := &fakeSender{}
	s.sending = fake
	var log bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	return fake, &log
}

// createWithLink creates the person through the API and returns the link as the
// screen builds it.
func createWithLink(t *testing.T, s *Server, username, herEmail string) string {
	t.Helper()
	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios",
		map[string]string{"username": username, "name": "Maria Silva", "email": herEmail})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d, %s", rec.Code, rec.Body)
	}
	user, _ := resp["user"].(map[string]any)
	if user["email"] != herEmail && herEmail != "" {
		t.Errorf("created with email %v", user["email"])
	}
	token, _ := resp["token"].(string)
	return "https://nvr.exemplo.com/#convite?token=" + token
}

// From the admin creating the person with an email until the link reaches them.
func TestLinkByEmail(t *testing.T) {
	s, _ := testServer(t)
	fake, log := comEmail(t, s)
	link := createWithLink(t, s, "maria", "maria@exemplo.com")

	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link})
	if rec.Code != http.StatusOK || resp["sentTo"] != "maria@exemplo.com" {
		t.Fatalf("send: status %d, %s", rec.Code, rec.Body)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("%d emails sent", len(fake.sent))
	}
	m := fake.sent[0]
	if len(m.To) != 1 || m.To[0] != "maria@exemplo.com" {
		t.Errorf("to %v", m.To)
	}
	for _, stretch := range []string{"Olá, Maria Silva.", link, "vale 10 minutos", "Seu usuário para entrar: maria"} {
		if !strings.Contains(m.Text, stretch) {
			t.Errorf("the text does not have %q:\n%s", stretch, m.Text)
		}
	}
	token := strings.TrimPrefix(link, "https://nvr.exemplo.com/#convite?token=")
	if strings.Contains(log.String(), token) {
		t.Error("the token ended up in the log")
	}
	if !strings.Contains(log.String(), "link sent by email") {
		t.Errorf("the send did not go to the log:\n%s", log)
	}
}

// TestLinkByEmailChecksTheLink: the server only sends that person's open link,
// to their email.
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
		{"another person's link", "maria", joaosLink, http.StatusBadRequest},
		{"person without email", "joao", joaosLink, http.StatusBadRequest},
		{"made-up token", "maria", "https://nvr.exemplo.com/#convite?token=inventado", http.StatusGone},
		{"another scheme", "maria", "javascript:alert(1)#convite?token=" + token, http.StatusBadRequest},
		{"with login in the URL", "maria", "https://x@nvr.exemplo.com/#convite?token=" + token, http.StatusBadRequest},
		{"with query", "maria", "https://nvr.exemplo.com/?a=1#convite?token=" + token, http.StatusBadRequest},
		{"another fragment", "maria", "https://nvr.exemplo.com/#conta?token=" + token, http.StatusBadRequest},
		{"extra token", "maria", link + "&token=outro", http.StatusBadRequest},
	} {
		rec, _ := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/link/email",
			map[string]string{"username": c.username, "url": c.url})
		if rec.Code != c.status {
			t.Errorf("%s: status %d, expected %d (%s)", c.nome, rec.Code, c.status, rec.Body)
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("%d emails went out from rejected requests", len(fake.sent))
	}

	// The viewer does not send any email.
	ana := users.User{Username: "ana", Name: "Ana", Role: users.RoleViewer, PasswordHash: "$guardada$da-ana"}
	if err := s.users.Update(func(a *users.FileData) error {
		a.Users = append(a.Users, ana)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := requestAs(t, s, "ana", http.MethodPost, "/api/usuarios/link/email",
		map[string]string{"username": "maria", "url": link}); rec.Code != http.StatusForbidden {
		t.Errorf("not admin: status %d", rec.Code)
	}
}

// TestEmailServerFailureReachesTheScreen: the SMTP error comes back as 502,
// with its text.
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
		t.Errorf("test: status %d, %s", rec.Code, rec.Body)
	}
}

// TestWithoutEmailTheRoutesReject: without the smtp section, nothing goes out,
// and the response says why.
func TestWithoutEmailTheRoutesReject(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s)
	s.cfg.SMTP.Problem = "smtp.from vazio"
	link := createWithLink(t, s, "maria", "maria@exemplo.com")

	_, resp := requestAs(t, s, "admin", http.MethodGet, "/api/usuarios", nil)
	if resp["emailEnabled"] != false {
		t.Errorf("emailEnabled %v without email", resp["emailEnabled"])
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

// TestEmailTest: the test goes to the typed address, and only to one.
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
		t.Errorf("sent %+v", fake.sent)
	}
}

// TestChangeEmailThroughTheAPI: changes it, rejects the duplicate and deletes
// it.
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
		t.Errorf("duplicate email: status %d", code)
	}
	if code, resp := swap("joao", "joao@exemplo.com"); code != http.StatusOK ||
		resp["user"].(map[string]any)["email"] != "joao@exemplo.com" {
		t.Errorf("change: status %d, %v", code, resp)
	}
	if code, resp := swap("joao", ""); code != http.StatusOK || resp["user"].(map[string]any)["email"] != nil {
		t.Errorf("delete: status %d, %v", code, resp)
	}
	if code, _ := swap("ninguem", "x@exemplo.com"); code != http.StatusNotFound {
		t.Errorf("person that does not exist: status %d", code)
	}
}

// TestRejectedSmtpBecomesWarning: /api/health carries the reason, for the
// Warnings card and the bell; without a problem, the field does not even come.
func TestRejectedSmtpBecomesWarning(t *testing.T) {
	s, _ := testServer(t)
	_, resp := requestAs(t, s, "", http.MethodGet, "/api/health", nil)
	if _, has := resp["smtp"]; has {
		t.Errorf("smtp in health with no problem at all: %v", resp["smtp"])
	}
	s.cfg.SMTP.Problem = `smtp.security "ssl" fora do aceito`
	_, resp = requestAs(t, s, "", http.MethodGet, "/api/health", nil)
	smtp, _ := resp["smtp"].(map[string]any)
	if smtp["error"] != s.cfg.SMTP.Problem {
		t.Errorf("smtp %v", resp["smtp"])
	}
}

// TestAlertsEmailBecomesWarning: the problem in the alerts section goes to
// /api/health even without a camera with alert, because it belongs to the
// configuration.
func TestAlertsEmailBecomesWarning(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Alerts.Email.Warnings = []string{"falta a seção smtp, e sem ela a lista alerts.email.to não recebe nada"}
	_, resp := requestAs(t, s, "", http.MethodGet, "/api/health", nil)
	alerts, _ := resp["alerts"].(map[string]any)
	if w, _ := alerts["warnings"].([]any); len(w) != 1 || w[0] != s.cfg.Alerts.Email.Warnings[0] {
		t.Errorf("alerts %v", resp["alerts"])
	}
	if _, has := alerts["total"]; has {
		t.Errorf("total without a camera with alert: %v", alerts)
	}
}

// TestDiagnosticsShowsTheServerWithoutThePassword: the Email card sees the
// server and the sender, never the login or the password.
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
		t.Error("the login or the password ended up in the response")
	}
}
