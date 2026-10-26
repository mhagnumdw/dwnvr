package config

import (
	"fmt"
	"net"
	"net/mail"
	"slices"
	"strings"
)

// SMTP é o servidor que envia os e-mails do dwnvr, como o link de convite.
// Opcional: sem Host, o dwnvr não envia nada, e a tela esconde o que é de
// e-mail. Serve qualquer provedor que fale SMTP; nada aqui é de um só.
type SMTP struct {
	Host string `yaml:"host"`
	// Port zero é a padrão da Security (ver PortaSMTPPadrao).
	Port int `yaml:"port"`
	// Security é starttls (o padrão), tls ou none (ver SegurancasSMTP).
	Security string `yaml:"security"`
	// Username e Password vão juntos: os dois vazios são o servidor que não
	// pede login, como o de um relay na rede local.
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// From é o remetente, como "dwnvr <alertas@exemplo.com>". Vazio é o
	// Username, quando ele é um endereço.
	From string `yaml:"from"`

	// Recusa é por que a seção, preenchida, não serve, ou vazio. Com ela o
	// e-mail fica desligado até alguém corrigir o dwnvr.yaml: não há padrão
	// que substitua um servidor errado.
	Recusa string `yaml:"-"`
}

// Ligado diz se há servidor configurado e se ele passou na conferência.
func (s SMTP) Ligado() bool { return s.Host != "" && s.Recusa == "" }

// SegurancasSMTP são os valores aceitos em smtp.security.
var SegurancasSMTP = []string{"starttls", "tls", "none"}

// confere completa os padrões da seção e diz o que a impede de funcionar, ou
// "" se nada. A seção inteira vazia não é erro: é o e-mail desligado.
func (s *SMTP) confere() string {
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
		s.Security = SegurancasSMTP[0]
	}
	if !slices.Contains(SegurancasSMTP, s.Security) {
		n := len(SegurancasSMTP)
		return fmt.Sprintf("smtp.security %q fora do aceito: use %s ou %s", s.Security,
			strings.Join(SegurancasSMTP[:n-1], ", "), SegurancasSMTP[n-1])
	}
	if s.Port == 0 {
		s.Port = PortaSMTPPadrao[s.Security]
	}
	if !FaixaPortaSMTP.Contem(int64(s.Port)) {
		return fmt.Sprintf("smtp.port %d fora do aceito: use %s", s.Port, FaixaPortaSMTP)
	}

	if (s.Username == "") != (s.Password == "") {
		return "smtp.username e smtp.password vão juntos: preencha os dois, ou nenhum se o servidor não pede login"
	}
	// O net/smtp só manda a senha com TLS ou para a própria máquina, e confere
	// pelo nome escrito aqui: estes três são os que ele aceita.
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
