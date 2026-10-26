package config

import (
	"strings"
	"testing"
)

// TestSMTPSemSecaoFicaDesligado: quem não configura não paga nada, e não
// recebe aviso por isso.
func TestSMTPSemSecaoFicaDesligado(t *testing.T) {
	cfg, err := carregaYaml(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.Ligado() || cfg.SMTP.Recusa != "" {
		t.Errorf("smtp %+v, esperado desligado e sem recusa", cfg.SMTP)
	}
}

// TestSMTPCompletaOsPadroes: o exemplo do Gmail só com host, login e senha.
func TestSMTPCompletaOsPadroes(t *testing.T) {
	cfg, err := carregaYaml(t, `
smtp:
  host: smtp.gmail.com
  username: dwnvr@exemplo.com
  password: troque-me
`)
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.SMTP
	if !s.Ligado() {
		t.Fatalf("smtp desligado: %s", s.Recusa)
	}
	if s.Security != "starttls" || s.Port != 587 || s.From != "dwnvr@exemplo.com" {
		t.Errorf("security %q, port %d, from %q; esperado starttls, 587 e o username",
			s.Security, s.Port, s.From)
	}
}

// TestSMTPPortaSegueASeguranca: cada security tem a sua porta, e a escrita
// vale mais que a padrão.
func TestSMTPPortaSegueASeguranca(t *testing.T) {
	for _, c := range []struct {
		yaml  string
		porta int
	}{
		{"security: tls", 465},
		{"security: none", 25},
		{"security: tls\n  port: 2465", 2465},
	} {
		cfg, err := carregaYaml(t, "smtp:\n  host: smtp.exemplo.com\n  from: dwnvr@exemplo.com\n  "+c.yaml+"\n")
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.SMTP.Ligado() || cfg.SMTP.Port != c.porta {
			t.Errorf("%q: porta %d, recusa %q; esperado %d", c.yaml, cfg.SMTP.Port, cfg.SMTP.Recusa, c.porta)
		}
	}
}

// TestSMTPRecusaODesligaComOMotivo: cada problema desliga o e-mail e diz
// qual campo corrigir.
func TestSMTPRecusaODesligaComOMotivo(t *testing.T) {
	for _, c := range []struct {
		nome, yaml, campo string
	}{
		{"sem host", "  username: dwnvr@exemplo.com\n  password: troque-me", "smtp.host"},
		{"host com porta", "  host: smtp.gmail.com:587\n  from: dwnvr@exemplo.com", "smtp.port"},
		{"host com espaço", "  host: smtp gmail com\n  from: dwnvr@exemplo.com", "smtp.host"},
		{"security estranho", "  host: smtp.gmail.com\n  security: ssl\n  from: dwnvr@exemplo.com", "smtp.security"},
		{"porta fora", "  host: smtp.gmail.com\n  port: 70000\n  from: dwnvr@exemplo.com", "smtp.port"},
		{"usuário sem senha", "  host: smtp.gmail.com\n  username: dwnvr@exemplo.com", "smtp.password"},
		{"senha sem usuário", "  host: smtp.gmail.com\n  password: troque-me\n  from: dwnvr@exemplo.com", "smtp.username"},
		{"sem remetente", "  host: smtp.exemplo.com\n  username: apikey\n  password: troque-me", "smtp.from"},
		{"remetente ruim", "  host: smtp.gmail.com\n  from: dwnvr", "smtp.from"},
		{"senha sem TLS", "  host: smtp.exemplo.com\n  security: none\n  username: dwnvr@exemplo.com\n  password: troque-me", "smtp.security"},
	} {
		cfg, err := carregaYaml(t, "smtp:\n"+c.yaml+"\n")
		if err != nil {
			t.Fatalf("%s: %v", c.nome, err)
		}
		if cfg.SMTP.Ligado() || !strings.Contains(cfg.SMTP.Recusa, c.campo) {
			t.Errorf("%s: ligado %v, recusa %q; esperado desligado citando %s",
				c.nome, cfg.SMTP.Ligado(), cfg.SMTP.Recusa, c.campo)
		}
	}
}

// TestSMTPSemTLSNaPropriaMaquina: o relay local, sem TLS e com login, é o
// caso que o net/smtp aceita.
func TestSMTPSemTLSNaPropriaMaquina(t *testing.T) {
	cfg, err := carregaYaml(t, `
smtp:
  host: 127.0.0.1
  security: none
  username: dwnvr@exemplo.com
  password: troque-me
`)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SMTP.Ligado() {
		t.Errorf("smtp desligado: %s", cfg.SMTP.Recusa)
	}
}

func carregaYaml(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	cfg, _, err := carrega(t, "storage:\n  root: /tmp/x\n"+yaml, "")
	return cfg, err
}
