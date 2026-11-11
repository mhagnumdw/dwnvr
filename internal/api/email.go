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

// O que a API faz com o e-mail: o teste do Diagnóstico, as peças do link de
// convite por e-mail (a rota fica em usuarios.go) e o alerta por e-mail como a
// aba Alerta o mostra.

// semEmail recusa o que envia e-mail quando a seção smtp não está ligada,
// dizendo por quê.
func (s *Server) semEmail(w http.ResponseWriter) bool {
	if s.envio != nil {
		return false
	}
	msg := "o e-mail está desligado: configure a seção smtp do dwnvr.yaml (ver docs/configuracao.md)"
	if s.cfg.SMTP.Problem != "" {
		msg = "o e-mail está desligado: " + s.cfg.SMTP.Problem
	}
	writeError(w, http.StatusConflict, msg)
	return true
}

// tokenDoLink tira o token do link de convite, do jeito que a tela o monta:
// http(s)://endereço/caminho#convite?token=…. Qualquer outra forma é
// recusada: o e-mail só leva um link de convite.
func tokenDoLink(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" {
		return "", false
	}
	resto, ok := strings.CutPrefix(u.Fragment, "convite?")
	if !ok {
		return "", false
	}
	q, err := url.ParseQuery(resto)
	if err != nil || len(q) != 1 || len(q["token"]) != 1 || q.Get("token") == "" {
		return "", false
	}
	return q.Get("token"), true
}

// mensagemDoConvite é o e-mail do link de definir a senha.
func mensagemDoConvite(u users.User, para, link string) email.Message {
	minutos := int(users.InviteValidity.Minutes())
	return email.Message{
		To:      []string{para},
		Subject: "Defina sua senha no dwnvr",
		Text: fmt.Sprintf("Olá, %s.\n\n"+
			"Abra o link abaixo para definir a sua senha no dwnvr. Ele vale %d minutos e funciona uma vez só.\n\n"+
			"%s\n\n"+
			"Seu usuário para entrar: %s\n\n"+
			"Se o link vencer, peça outro a quem administra o dwnvr.\n",
			u.Name, minutos, link, u.Username),
	}
}

// handleTesteEmail manda um e-mail de teste, para o admin conferir a seção
// smtp sem cadastrar ninguém. O erro do servidor vem do jeito que ele
// respondeu.
func (s *Server) handleTesteEmail(w http.ResponseWriter, r *http.Request) {
	if s.semEmail(w) {
		return
	}
	var req struct {
		Para string `json:"to"`
	}
	if !lerCorpo(w, r, &req) {
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
	if err := s.envio.Send(r.Context(), m); err != nil {
		s.log.Warn("o e-mail de teste não foi", "por", pessoaDe(r).Usuario, "erro", err)
		writeError(w, http.StatusBadGateway, "o e-mail não foi: "+err.Error())
		return
	}
	s.log.Info("e-mail de teste enviado", "por", pessoaDe(r).Usuario)
	writeJSON(w, map[string]string{"sentTo": para})
}

// smtpInfo é a seção smtp como o Diagnóstico a mostra: sem o login e sem a
// senha. Error é o motivo de o e-mail estar desligado, quando há.
type smtpInfo struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"`
	From     string `json:"from,omitempty"`
	Error    string `json:"error,omitempty"`
}

// smtpNoDiagnostico é nil sem a seção smtp: quem não a configura não vê o
// card.
func smtpNoDiagnostico(c config.SMTP) *smtpInfo {
	if c.Host == "" && c.Problem == "" {
		return nil
	}
	return &smtpInfo{Host: c.Host, Port: c.Port, Security: c.Security, From: c.From, Error: c.Problem}
}

// alertaPorEmail é o "Para onde vai" da aba Alerta: a lista que recebe, com o
// desde quando e o erro se o meio está falhando; ou, sem o meio, as seções do
// dwnvr.yaml que faltam, com o motivo da seção smtp recusada. Só o admin a
// recebe, porque leva os endereços.
type alertaPorEmail struct {
	To           []string   `json:"to,omitempty"`
	FailingSince *time.Time `json:"failingSince,omitempty"`
	Error        string     `json:"error,omitempty"`
	Missing      []string   `json:"missing,omitempty"`
	SMTPError    string     `json:"smtpError,omitempty"`
}

func (s *Server) alertaPorEmail() alertaPorEmail {
	if falta := alert.MissingForEmail(s.cfg); len(falta) > 0 {
		return alertaPorEmail{Missing: falta, SMTPError: s.cfg.SMTP.Problem}
	}
	a := alertaPorEmail{To: s.cfg.Alerts.Email.To}
	// O estado do meio só existe depois que alguma câmera ligou o alerta.
	if c := s.mgr.Alertas(); c != nil {
		for _, m := range c.NotifierStates() {
			if m.Name == alert.NotifierEmail && !m.FailingSince.IsZero() {
				a.FailingSince, a.Error = &m.FailingSince, m.Err
			}
		}
	}
	return a
}
