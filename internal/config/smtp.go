package config

import (
	"fmt"
	"net"
	"net/mail"
	"slices"
	"strings"
)

// SMTP is the server that sends dwnvr's e-mails, like the invite link.
// Optional: without Host, dwnvr sends nothing, and the screen hides what's
// e-mail related. Any provider that speaks SMTP works; nothing here is
// specific to one.
type SMTP struct {
	Host string `yaml:"host"`
	// Port zero is the Security's default (see DefaultSMTPPort).
	Port int `yaml:"port"`
	// Security is starttls (the default), tls or none (see SMTPSecurityModes).
	Security string `yaml:"security"`
	// Username and Password go together: both empty is a server that doesn't
	// ask for login, like a relay on the local network.
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// From is the sender, like "dwnvr <alertas@exemplo.com>". Empty means the
	// Username, when it's an address.
	From string `yaml:"from"`

	// Problem is why the section, filled in, doesn't work, or empty. With it
	// e-mail stays off until someone fixes dwnvr.yaml: there's no default to
	// stand in for a wrong server. In pt-BR, because Diagnostics shows it.
	Problem string `yaml:"-"`
}

// Enabled says whether a server is configured and passed the check.
func (s SMTP) Enabled() bool { return s.Host != "" && s.Problem == "" }

// SMTPSecurityModes are the values accepted in smtp.security.
var SMTPSecurityModes = []string{"starttls", "tls", "none"}

// check fills in the section's defaults and says what keeps it from working,
// or "" if nothing. The whole section empty isn't an error: it's e-mail
// turned off.
func (s *SMTP) check() string {
	if s.Host == "" {
		if *s != (SMTP{}) {
			return "smtp.host vazio: sem ele não há para onde enviar"
		}
		return ""
	}
	if strings.ContainsAny(s.Host, " /") {
		return fmt.Sprintf("smtp.host %q não é um nome nem um IP", s.Host)
	}
	if _, _, err := net.SplitHostPort(s.Host); err == nil {
		return fmt.Sprintf("smtp.host %q: a porta vai em smtp.port", s.Host)
	}

	if s.Security == "" {
		s.Security = SMTPSecurityModes[0]
	}
	if !slices.Contains(SMTPSecurityModes, s.Security) {
		n := len(SMTPSecurityModes)
		return fmt.Sprintf("smtp.security %q fora do aceito: use %s ou %s", s.Security,
			strings.Join(SMTPSecurityModes[:n-1], ", "), SMTPSecurityModes[n-1])
	}
	if s.Port == 0 {
		s.Port = DefaultSMTPPort[s.Security]
	}
	if !SMTPPortRange.Contains(int64(s.Port)) {
		return fmt.Sprintf("smtp.port %d fora do aceito: use %s", s.Port, SMTPPortRange)
	}

	if (s.Username == "") != (s.Password == "") {
		return "smtp.username e smtp.password vão juntos: preencha os dois, ou nenhum se o servidor não pede login"
	}
	// net/smtp only sends the password over TLS or to the local machine, and
	// checks it by the name written here: these three are the ones it
	// accepts.
	local := s.Host == "localhost" || s.Host == "127.0.0.1" || s.Host == "::1"
	if s.Security == "none" && s.Username != "" && !local {
		return fmt.Sprintf("smtp.security none com login em %s: a senha só sai com TLS; use starttls ou tls",
			s.Host)
	}

	if s.From == "" {
		if _, err := mail.ParseAddress(s.Username); err != nil {
			return "smtp.from vazio, e o smtp.username não é um endereço de e-mail: diga o remetente no smtp.from"
		}
		s.From = s.Username
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		return fmt.Sprintf("smtp.from %q não é um endereço de e-mail", s.From)
	}
	return ""
}
