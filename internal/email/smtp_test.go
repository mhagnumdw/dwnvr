package email

import (
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

const testPassword = "troque-me"

// client builds the SMTP pointed at the fake server, accepting its certificate.
func client(t *testing.T, f *fakeServer) *SMTP {
	t.Helper()
	roots := newFakeServer(t, f)
	cfg := config.SMTP{Host: "127.0.0.1", Port: f.port, Security: f.security,
		From: "dwnvr <dwnvr@exemplo.com>"}
	if f.user != "" {
		cfg.Username, cfg.Password = f.user, testPassword
	}
	s, err := NewSMTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.roots = roots
	return s
}

// The invite link has 43 characters of token: the line goes past
// quoted-printable's 76 and must come back whole. The token is repeated on
// purpose, so it doesn't look like a real key to betterleaks.
var testLink = "https://nvr.exemplo.com/#convite?token=" + strings.Repeat("token-falso", 4)[:43]

var testMessage = Message{
	To:      []string{"fulano@exemplo.com"},
	Subject: "Defina sua senha no dwnvr, João",
	Text:    "Olá, João Silva.\n\nAbra o link abaixo:\n\n" + testLink + "\n\nSeu usuário: joao.silva\n",
}

// TestSendsInAllThreeModes: the message arrives whole, with the accent and
// the link intact, without TLS, with STARTTLS and with implicit TLS.
func TestSendsInAllThreeModes(t *testing.T) {
	for _, seg := range []string{"none", "starttls", "tls"} {
		t.Run(seg, func(t *testing.T) {
			f := &fakeServer{security: seg, user: "dwnvr@exemplo.com", password: testPassword}
			if err := client(t, f).Send(context.Background(), testMessage); err != nil {
				t.Fatal(err)
			}
			received, logins := f.receivedSoFar()
			if len(received) != 1 {
				t.Fatalf("%d messages received, want 1", len(received))
			}
			r := received[0]
			if r.inTLS != (seg != "none") {
				t.Errorf("in TLS %v with security %s", r.inTLS, seg)
			}
			if len(logins) != 1 || logins[0] != "dwnvr@exemplo.com" {
				t.Errorf("logins %v", logins)
			}
			if !strings.HasPrefix(r.from, "FROM:<dwnvr@exemplo.com>") ||
				len(r.to) != 1 || r.to[0] != "TO:<fulano@exemplo.com>" {
				t.Errorf("envelope from %q to %v", r.from, r.to)
			}
			checkMessage(t, r.data)
		})
	}
}

// checkMessage reads what arrived as a mail reader would.
func checkMessage(t *testing.T, data string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil || from.Name != "dwnvr" || from.Address != "dwnvr@exemplo.com" {
		t.Errorf("From %q (%v)", msg.Header.Get("From"), err)
	}
	if got := msg.Header.Get("To"); got != "fulano@exemplo.com" {
		t.Errorf("To %q", got)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != testMessage.Subject {
		t.Errorf("Subject %q, read %q (%v)", msg.Header.Get("Subject"), subject, err)
	}
	for _, h := range []string{"Date", "Message-ID"} {
		if msg.Header.Get(h) == "" {
			t.Errorf("no %s", h)
		}
	}
	if !strings.HasSuffix(msg.Header.Get("Message-ID"), "@exemplo.com>") {
		t.Errorf("Message-ID %q outside the sender's domain", msg.Header.Get("Message-ID"))
	}
	if got := msg.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	if text := strings.ReplaceAll(string(body), "\r\n", "\n"); text != testMessage.Text {
		t.Errorf("body\n%q\nwant\n%q", text, testMessage.Text)
	}
}

// TestWithoutSTARTTLSThePasswordStaysHome: a server that doesn't offer
// STARTTLS gets neither the password nor the message.
func TestWithoutSTARTTLSThePasswordStaysHome(t *testing.T) {
	f := &fakeServer{security: "starttls", noSTARTTLS: true, user: "dwnvr@exemplo.com",
		password: testPassword}
	err := client(t, f).Send(context.Background(), testMessage)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("error %v, expected the STARTTLS one", err)
	}
	if received, logins := f.receivedSoFar(); len(received) != 0 || len(logins) != 0 {
		t.Errorf("%d messages and %d logins arrived without TLS", len(received), len(logins))
	}
}

// TestServerErrorsComeAsTheyCame: the admin reads the server's answer on
// screen, with the code, to know what to fix.
func TestServerErrorsComeAsTheyCame(t *testing.T) {
	for _, c := range []struct {
		name string
		f    *fakeServer
		want []string
	}{
		{"wrong password", &fakeServer{security: "tls", user: "dwnvr@exemplo.com", password: "senha errada"},
			[]string{"recusou o login: 535 5.7.8 usuario e senha recusados 5.7.8 veja a ajuda"}},
		{"recipient", &fakeServer{security: "tls", rejectRcpt: "fulano@exemplo.com"},
			[]string{"recusou o destinatário fulano@exemplo.com", "550 5.1.1"}},
	} {
		err := client(t, c.f).Send(context.Background(), testMessage)
		for _, e := range c.want {
			if err == nil || !strings.Contains(err.Error(), e) {
				t.Errorf("%s: error %v, expected with %q", c.name, err, e)
			}
		}
		if received, _ := c.f.receivedSoFar(); len(received) != 0 {
			t.Errorf("%s: message delivered despite the error", c.name)
		}
	}
}

// TestSilentServerHitsTheTimeout: sending doesn't get stuck on a server
// that accepts the connection and doesn't answer.
func TestSilentServerHitsTheTimeout(t *testing.T) {
	s := client(t, &fakeServer{security: "starttls", silent: true})
	s.timeout = 200 * time.Millisecond
	start := time.Now()
	err := s.Send(context.Background(), testMessage)
	if err == nil || !strings.Contains(err.Error(), "não respondeu em 200ms") {
		t.Errorf("error %v, expected the timeout one", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("waited %v with a 200ms timeout", d)
	}
}

// TestUnknownCertificateFails: TLS checks the certificate; a self-signed
// one nobody accepted doesn't get through.
func TestUnknownCertificateFails(t *testing.T) {
	f := &fakeServer{security: "tls", user: "dwnvr@exemplo.com", password: testPassword}
	s := client(t, f)
	s.roots = nil // the system's, which don't know the fake certificate
	if err := s.Send(context.Background(), testMessage); err == nil {
		t.Fatal("sent to a certificate nobody accepted")
	}
	if _, logins := f.receivedSoFar(); len(logins) != 0 {
		t.Error("the password left before the certificate was checked")
	}
}

// TestMessageThatBecomesHeaderFails: a subject with a line break or a
// recipient with a name would write another header; nothing is sent.
func TestMessageThatBecomesHeaderFails(t *testing.T) {
	f := &fakeServer{security: "tls"}
	s := client(t, f)
	for _, m := range []Message{
		{To: []string{"fulano@exemplo.com"}, Subject: "oi\r\nBcc: outro@exemplo.com", Text: "x"},
		{To: []string{"Fulano <fulano@exemplo.com>"}, Subject: "oi", Text: "x"},
		{To: []string{"fulano@exemplo.com, outro@exemplo.com"}, Subject: "oi", Text: "x"},
		{Subject: "oi", Text: "x"},
		withAttachment(Attachment{Name: "a.jpg\r\nBcc: outro@exemplo.com", Type: "image/jpeg"}),
		withAttachment(Attachment{Name: "", Type: "image/jpeg"}),
		withAttachment(Attachment{Name: "a.jpg", Type: "image/jpeg\r\nBcc: outro@exemplo.com"}),
		withAttachment(Attachment{Name: "a.jpg", Type: "image/jpeg; name=b.exe"}),
		withAttachment(Attachment{Name: "a.jpg", Type: ""}),
	} {
		if err := s.Send(context.Background(), m); err == nil {
			t.Errorf("sent %+v", m)
		}
	}
	if received, _ := f.receivedSoFar(); len(received) != 0 {
		t.Errorf("%d messages arrived", len(received))
	}
}

func withAttachment(a Attachment) Message {
	return Message{To: []string{"fulano@exemplo.com"}, Subject: "oi", Text: "x", Attachments: []Attachment{a}}
}

// TestDisabledDoesNotBuild: main only builds the sender with the checked section.
func TestDisabledDoesNotBuild(t *testing.T) {
	for _, cfg := range []config.SMTP{{}, {Host: "smtp.exemplo.com", Problem: "smtp.from vazio"}} {
		if _, err := NewSMTP(cfg); err == nil {
			t.Errorf("built with %+v", cfg)
		}
	}
}
