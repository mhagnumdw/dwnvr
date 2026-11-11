package email

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// SMTP sends through the server in the smtp section of dwnvr.yaml. One
// connection per send: dwnvr sends few e-mails, and keeping one open would
// mean paying all the time for something used now and then.
type SMTP struct {
	cfg  config.SMTP
	from *mail.Address

	// roots are the authorities accepted for TLS; nil means the system's.
	// Only the test swaps it, to accept the fake server's certificate.
	roots   *x509.CertPool
	timeout time.Duration
	now     func() time.Time
}

// NewSMTP prepares sending. cfg is the section already checked by
// config.Load, and must be Enabled.
func NewSMTP(cfg config.SMTP) (*SMTP, error) {
	if !cfg.Enabled() {
		return nil, errors.New("smtp disabled")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp.from: %w", err)
	}
	return &SMTP{cfg: cfg, from: from, timeout: SendTimeout, now: time.Now}, nil
}

// Send opens the connection, logs in and hands over the message. With
// security starttls, a server that doesn't offer STARTTLS gets neither the
// message nor the password: nothing goes out without TLS.
//
// Its errors are sentences for the admin on the Diagnostics screen, in pt-BR
// until i18n.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if err := m.check(); err != nil {
		return err
	}
	msg, err := build(s.from, m, s.now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	err = s.deliver(ctx, m.To, msg)
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("o servidor %s não respondeu em %v: %w", s.address(), s.timeout, err)
	}
	return err
}

func (s *SMTP) address() string {
	return net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
}

func (s *SMTP) tls() *tls.Config {
	return &tls.Config{ServerName: s.cfg.Host, RootCAs: s.roots, MinVersion: tls.VersionTLS12}
}

func (s *SMTP) deliver(ctx context.Context, to []string, msg []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.address())
	if err != nil {
		return fmt.Errorf("conectando em %s: %w", s.address(), err)
	}
	// net/smtp knows nothing about contexts: the deadline goes on the
	// connection, and cancellation (the screen that gave up) closes it.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()

	if s.cfg.Security == "tls" {
		tc := tls.Client(conn, s.tls())
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("abrindo o TLS com %s: %w", s.address(), err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("falando com %s: %w", s.address(), err)
	}
	defer func() { _ = c.Close() }()

	if s.cfg.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s não oferece STARTTLS: nada foi enviado, nem a senha", s.address())
		}
		if err := c.StartTLS(s.tls()); err != nil {
			return fmt.Errorf("abrindo o TLS com %s: %w", s.address(), err)
		}
	}
	if s.cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("%s não aceita login, e o smtp.username está preenchido", s.address())
		}
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("o servidor recusou o login: %w", asReceived(err))
		}
	}

	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("o servidor recusou o remetente %s: %w", s.from.Address, asReceived(err))
	}
	for _, p := range to {
		if err := c.Rcpt(p); err != nil {
			return fmt.Errorf("o servidor recusou o destinatário %s: %w", p, asReceived(err))
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("o servidor recusou a mensagem: %w", asReceived(err))
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("enviando a mensagem: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("o servidor recusou a mensagem: %w", asReceived(err))
	}
	// With the 250 at the end of the message, it already belongs to the
	// server: a failed QUIT doesn't undo it.
	_ = c.Quit()
	return nil
}

// asReceived writes the server's answer the way it came, like
// "535 5.7.8 Username and Password not accepted": the textproto error puts the
// message in quotes, and a multi-line one ends up on a single line.
func asReceived(err error) error {
	var te *textproto.Error
	if !errors.As(err, &te) {
		return err
	}
	return fmt.Errorf("%03d %s", te.Code, strings.ReplaceAll(te.Msg, "\n", " "))
}
