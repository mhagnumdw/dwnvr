package config

import (
	"strings"
	"testing"
)

// TestSMTPWithoutSectionStaysOff: whoever doesn't configure it pays nothing,
// and gets no warning for it.
func TestSMTPWithoutSectionStaysOff(t *testing.T) {
	cfg, err := loadYAML(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.Enabled() || cfg.SMTP.Problem != "" {
		t.Errorf("smtp %+v, expected off and without a problem", cfg.SMTP)
	}
}

// TestSMTPFillsInDefaults: the Gmail example with only host, login and password.
func TestSMTPFillsInDefaults(t *testing.T) {
	cfg, err := loadYAML(t, `
smtp:
  host: smtp.gmail.com
  username: dwnvr@exemplo.com
  password: troque-me
`)
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.SMTP
	if !s.Enabled() {
		t.Fatalf("smtp off: %s", s.Problem)
	}
	if s.Security != "starttls" || s.Port != 587 || s.From != "dwnvr@exemplo.com" {
		t.Errorf("security %q, port %d, from %q; expected starttls, 587 and the username",
			s.Security, s.Port, s.From)
	}
}

// TestSMTPPortFollowsSecurity: each security has its port, and a written one
// wins over the default.
func TestSMTPPortFollowsSecurity(t *testing.T) {
	for _, c := range []struct {
		yaml string
		port int
	}{
		{"security: tls", 465},
		{"security: none", 25},
		{"security: tls\n  port: 2465", 2465},
	} {
		cfg, err := loadYAML(t, "smtp:\n  host: smtp.exemplo.com\n  from: dwnvr@exemplo.com\n  "+c.yaml+"\n")
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.SMTP.Enabled() || cfg.SMTP.Port != c.port {
			t.Errorf("%q: port %d, problem %q; expected %d", c.yaml, cfg.SMTP.Port, cfg.SMTP.Problem, c.port)
		}
	}
}

// TestSMTPProblemDisablesWithReason: each problem turns e-mail off and says
// which field to fix.
func TestSMTPProblemDisablesWithReason(t *testing.T) {
	for _, c := range []struct {
		name, yaml, field string
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
		cfg, err := loadYAML(t, "smtp:\n"+c.yaml+"\n")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if cfg.SMTP.Enabled() || !strings.Contains(cfg.SMTP.Problem, c.field) {
			t.Errorf("%s: on %v, problem %q; expected off citing %s",
				c.name, cfg.SMTP.Enabled(), cfg.SMTP.Problem, c.field)
		}
	}
}

// TestSMTPWithoutTLSOnLocalhost: a local relay, without TLS and with login,
// is the case net/smtp accepts.
func TestSMTPWithoutTLSOnLocalhost(t *testing.T) {
	cfg, err := loadYAML(t, `
smtp:
  host: 127.0.0.1
  security: none
  username: dwnvr@exemplo.com
  password: troque-me
`)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SMTP.Enabled() {
		t.Errorf("smtp off: %s", cfg.SMTP.Problem)
	}
}

func loadYAML(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	cfg, _, err := load(t, "storage:\n  root: /tmp/x\n"+yaml, "")
	return cfg, err
}
