package email

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

// servidorFalso é um SMTP de mentira, no loopback, para o teste ver o que o
// cliente manda sem sair da máquina. Fala só o que o net/smtp usa.
type servidorFalso struct {
	// seguranca é como ele recebe a conexão: "none", "starttls" ou "tls".
	seguranca string
	// semSTARTTLS esconde o STARTTLS do EHLO, mesmo em "starttls".
	semSTARTTLS bool
	// mudo aceita a conexão e não responde nada.
	mudo bool
	// usuario e senha são o login aceito; usuario vazio não oferece AUTH.
	usuario, senha string
	// recusaPara é o destinatário que o RCPT recusa.
	recusaPara string

	tls   *tls.Config
	porta int

	mu        sync.Mutex
	recebidas []recebida
	logins    []string // o usuário de cada AUTH tentado, aceito ou não
}

type recebida struct {
	de    string
	para  []string
	dados string
	emTLS bool
}

// novoServidorFalso sobe o servidor e devolve as autoridades que o cliente
// precisa para aceitar o certificado dele.
func novoServidorFalso(t *testing.T, f *servidorFalso) *x509.CertPool {
	t.Helper()
	cert, raizes := certificadoFalso(t)
	f.tls = &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f.porta = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.atender(conn)
		}
	}()
	return raizes
}

func (f *servidorFalso) atender(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if f.mudo {
		_, _ = io.Copy(io.Discard, conn) // até o cliente desistir
		return
	}
	emTLS := f.seguranca == "tls"
	if emTLS {
		conn = tls.Server(conn, f.tls)
	}
	tp := textproto.NewConn(conn)
	responde := func(linha string) { _ = tp.PrintfLine("%s", linha) }
	responde("220 falso ESMTP")
	r := recebida{emTLS: emTLS}
	for {
		linha, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(linha, " ")
		switch strings.ToUpper(cmd) {
		case "EHLO", "HELO":
			ext := []string{"falso"}
			if f.seguranca == "starttls" && !emTLS && !f.semSTARTTLS {
				ext = append(ext, "STARTTLS")
			}
			if f.usuario != "" {
				ext = append(ext, "AUTH PLAIN")
			}
			ext = append(ext, "8BITMIME")
			for i, e := range ext {
				sep := "-"
				if i == len(ext)-1 {
					sep = " "
				}
				responde("250" + sep + e)
			}
		case "STARTTLS":
			responde("220 pode")
			tc := tls.Server(conn, f.tls)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, emTLS, r.emTLS = tc, true, true
			tp = textproto.NewConn(conn)
		case "AUTH":
			mecanismo, credencial, _ := strings.Cut(arg, " ")
			b, _ := base64.StdEncoding.DecodeString(credencial)
			partes := strings.Split(string(b), "\x00")
			if len(partes) == 3 {
				f.mu.Lock()
				f.logins = append(f.logins, partes[1])
				f.mu.Unlock()
			}
			if mecanismo == "PLAIN" && len(partes) == 3 && partes[1] == f.usuario && partes[2] == f.senha {
				responde("235 2.7.0 aceito")
			} else {
				// Em duas linhas, como a do Gmail.
				responde("535-5.7.8 usuario e senha recusados")
				responde("535 5.7.8 veja a ajuda")
			}
		case "MAIL":
			r.de = arg
			responde("250 ok")
		case "RCPT":
			if f.recusaPara != "" && strings.Contains(arg, f.recusaPara) {
				responde("550 5.1.1 caixa inexistente")
				continue
			}
			r.para = append(r.para, arg)
			responde("250 ok")
		case "DATA":
			responde("354 pode mandar")
			dados, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			r.dados = string(dados)
			f.mu.Lock()
			f.recebidas = append(f.recebidas, r)
			f.mu.Unlock()
			responde("250 2.0.0 na fila")
		case "QUIT":
			responde("221 tchau")
			return
		case "RSET", "NOOP":
			responde("250 ok")
		default:
			responde("502 não conheço")
		}
	}
}

func (f *servidorFalso) recebido() ([]recebida, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recebida(nil), f.recebidas...), append([]string(nil), f.logins...)
}

// certificadoFalso gera, na hora, um certificado autoassinado para o
// 127.0.0.1.
func certificadoFalso(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	chave, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	modelo := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "servidor falso"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, modelo, modelo, &chave.PublicKey, chave)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	raizes := x509.NewCertPool()
	raizes.AddCert(c)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: chave}, raizes
}
