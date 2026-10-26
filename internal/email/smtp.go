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

// SMTP envia pelo servidor da seção smtp do dwnvr.yaml. Uma conexão por
// envio: o dwnvr manda poucos e-mails, e manter uma aberta seria pagar o
// tempo todo pelo que se usa às vezes.
type SMTP struct {
	cfg config.SMTP
	de  *mail.Address

	// raizes são as autoridades aceitas no TLS; nil é a do sistema. Só o teste
	// troca, para aceitar o certificado do servidor falso.
	raizes *x509.CertPool
	prazo  time.Duration
	agora  func() time.Time
}

// NovoSMTP prepara o envio. cfg é a seção já conferida pelo config.Load, e
// tem de estar Ligado.
func NovoSMTP(cfg config.SMTP) (*SMTP, error) {
	if !cfg.Ligado() {
		return nil, errors.New("smtp desligado")
	}
	de, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp.from: %w", err)
	}
	return &SMTP{cfg: cfg, de: de, prazo: PrazoDoEnvio, agora: time.Now}, nil
}

// Enviar abre a conexão, faz o login e entrega a mensagem. Com security
// starttls, o servidor que não oferece o STARTTLS fica sem a mensagem e sem a
// senha: nada sai sem TLS.
func (s *SMTP) Enviar(ctx context.Context, m Mensagem) error {
	if err := m.confere(); err != nil {
		return err
	}
	msg, err := montar(s.de, m, s.agora())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.prazo)
	defer cancel()
	err = s.entregar(ctx, m.Para, msg)
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("o servidor %s não respondeu em %v: %w", s.endereco(), s.prazo, err)
	}
	return err
}

func (s *SMTP) endereco() string {
	return net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
}

func (s *SMTP) tls() *tls.Config {
	return &tls.Config{ServerName: s.cfg.Host, RootCAs: s.raizes, MinVersion: tls.VersionTLS12}
}

func (s *SMTP) entregar(ctx context.Context, para []string, msg []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.endereco())
	if err != nil {
		return fmt.Errorf("conectando em %s: %w", s.endereco(), err)
	}
	// O net/smtp não conhece contexto: o prazo vai na conexão, e o
	// cancelamento (a tela que desistiu) a fecha.
	if fim, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(fim)
	}
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()

	if s.cfg.Security == "tls" {
		tc := tls.Client(conn, s.tls())
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("abrindo o TLS com %s: %w", s.endereco(), err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("falando com %s: %w", s.endereco(), err)
	}
	defer func() { _ = c.Close() }()

	if s.cfg.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s não oferece STARTTLS: nada foi enviado, nem a senha", s.endereco())
		}
		if err := c.StartTLS(s.tls()); err != nil {
			return fmt.Errorf("abrindo o TLS com %s: %w", s.endereco(), err)
		}
	}
	if s.cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("%s não aceita login, e o smtp.username está preenchido", s.endereco())
		}
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("o servidor recusou o login: %w", comoVeio(err))
		}
	}

	if err := c.Mail(s.de.Address); err != nil {
		return fmt.Errorf("o servidor recusou o remetente %s: %w", s.de.Address, comoVeio(err))
	}
	for _, p := range para {
		if err := c.Rcpt(p); err != nil {
			return fmt.Errorf("o servidor recusou o destinatário %s: %w", p, comoVeio(err))
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("o servidor recusou a mensagem: %w", comoVeio(err))
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("enviando a mensagem: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("o servidor recusou a mensagem: %w", comoVeio(err))
	}
	// Com o 250 do fim da mensagem, ela já é do servidor: o QUIT que falhar
	// não a desfaz.
	_ = c.Quit()
	return nil
}

// comoVeio escreve a resposta do servidor do jeito que ela veio, como
// "535 5.7.8 Username and Password not accepted": o erro do textproto põe a
// mensagem entre aspas, e a de várias linhas fica numa só.
func comoVeio(err error) error {
	var te *textproto.Error
	if !errors.As(err, &te) {
		return err
	}
	return fmt.Errorf("%03d %s", te.Code, strings.ReplaceAll(te.Msg, "\n", " "))
}
