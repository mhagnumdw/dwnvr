package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/alert"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/email"
	"github.com/mhagnumdw/dwnvr/internal/users"
)

// What the API does with email: the Diagnostics test, the pieces of the invite
// link by email (the route is in users.go) and the email alert as the Alert tab
// shows it.

// noEmail rejects what sends email when the smtp section is not enabled, saying
// why.
func (s *Server) noEmail(w http.ResponseWriter) bool {
	if s.sending != nil {
		return false
	}
	msg := "o e-mail está desligado: configure a seção smtp do dwnvr.yaml (ver docs/configuracao.md)"
	if s.cfg.SMTP.Problem != "" {
		msg = "o e-mail está desligado: " + s.cfg.SMTP.Problem
	}
	writeError(w, http.StatusConflict, msg)
	return true
}

// tokenDoLink extracts the token from the invite link, the way the screen
// builds it: http(s)://address/path#convite?token=…. Any other form is
// rejected: the email only carries an invite link.
func tokenDoLink(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(u.Fragment, "convite?")
	if !ok {
		return "", false
	}
	q, err := url.ParseQuery(rest)
	if err != nil || len(q) != 1 || len(q["token"]) != 1 || q.Get("token") == "" {
		return "", false
	}
	return q.Get("token"), true
}

// inviteMessage is the email with the link to set the password.
func inviteMessage(u users.User, para, link string) email.Message {
	minutes := int(users.InviteValidity.Minutes())
	return email.Message{
		To:      []string{para},
		Subject: "Defina sua senha no dwnvr",
		Text: fmt.Sprintf("Olá, %s.\n\n"+
			"Abra o link abaixo para definir a sua senha no dwnvr. Ele vale %d minutos e funciona uma vez só.\n\n"+
			"%s\n\n"+
			"Seu usuário para entrar: %s\n\n"+
			"Se o link vencer, peça outro a quem administra o dwnvr.\n",
			u.Name, minutes, link, u.Username),
	}
}

// handleEmailTest sends a test email, for the admin to check the smtp section
// without registering anyone. The server error comes the way it answered.
func (s *Server) handleEmailTest(w http.ResponseWriter, r *http.Request) {
	if s.noEmail(w) {
		return
	}
	var req struct {
		Para string `json:"to"`
	}
	if !readBody(w, r, &req) {
		return
	}
	para, err := users.ValidateEmail(req.Para)
	if err != nil || para == "" {
		writeError(w, http.StatusBadRequest, "e-mail inválido: escreva só o endereço, como fulano@exemplo.com")
		return
	}
	m := email.Message{
		To:      []string{para},
		Subject: "Teste do dwnvr",
		Text: "Este é um e-mail de teste do dwnvr, mandado pela tela Diagnóstico.\n\n" +
			"Se ele chegou, o envio de e-mail está funcionando.\n",
	}
	if err := s.sending.Send(r.Context(), m); err != nil {
		s.log.Warn("the test email did not go", "by", personOf(r).Username, "err", err)
		writeError(w, http.StatusBadGateway, "o e-mail não foi: "+err.Error())
		return
	}
	s.log.Info("test email sent", "by", personOf(r).Username)
	writeJSON(w, map[string]string{"sentTo": para})
}

// smtpInfo is the smtp section as Diagnostics shows it: without the login and
// without the password. Error is the reason email is disabled, when there is
// one.
type smtpInfo struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"`
	From     string `json:"from,omitempty"`
	Error    string `json:"error,omitempty"`
}

// smtpInDiagnostics is nil without the smtp section: whoever does not configure
// it does not see the card.
func smtpInDiagnostics(c config.SMTP) *smtpInfo {
	if c.Host == "" && c.Problem == "" {
		return nil
	}
	return &smtpInfo{Host: c.Host, Port: c.Port, Security: c.Security, From: c.From, Error: c.Problem}
}

// emailAlert is the "Where it goes" of the Alert tab: the list that receives,
// with the since when and the error if the notifier is failing; or, without the
// notifier, the dwnvr.yaml sections that are missing, with the reason the smtp
// section was rejected. Only the admin receives it, because it carries the
// addresses.
type emailAlert struct {
	To           []string   `json:"to,omitempty"`
	FailingSince *time.Time `json:"failingSince,omitempty"`
	Error        string     `json:"error,omitempty"`
	Missing      []string   `json:"missing,omitempty"`
	SMTPError    string     `json:"smtpError,omitempty"`
}

func (s *Server) emailAlert() emailAlert {
	if missing := alert.MissingForEmail(s.cfg); len(missing) > 0 {
		return emailAlert{Missing: missing, SMTPError: s.cfg.SMTP.Problem}
	}
	a := emailAlert{To: s.cfg.Alerts.Email.To}
	// The notifier state only exists after some camera has enabled the alert.
	if c := s.mgr.Alerts(); c != nil {
		for _, m := range c.NotifierStates() {
			if m.Name == alert.NotifierEmail && !m.FailingSince.IsZero() {
				a.FailingSince, a.Error = &m.FailingSince, m.Err
			}
		}
	}
	return a
}
