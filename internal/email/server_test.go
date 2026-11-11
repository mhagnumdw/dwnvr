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

// fakeServer is a fake SMTP, on loopback, for the test to see what the
// client sends without leaving the machine. It only speaks what net/smtp
// uses.
type fakeServer struct {
	// security is how it takes the connection: "none", "starttls" or "tls".
	security string
	// noSTARTTLS hides STARTTLS from EHLO, even in "starttls".
	noSTARTTLS bool
	// silent accepts the connection and answers nothing.
	silent bool
	// user and password are the accepted login; an empty user doesn't offer AUTH.
	user, password string
	// rejectRcpt is the recipient RCPT rejects.
	rejectRcpt string

	tls  *tls.Config
	port int

	mu       sync.Mutex
	received []receivedMsg
	logins   []string // the user of each AUTH tried, accepted or not
}

type receivedMsg struct {
	from  string
	to    []string
	data  string
	inTLS bool
}

// newFakeServer starts the server and returns the authorities the client
// needs to accept its certificate.
func newFakeServer(t *testing.T, f *fakeServer) *x509.CertPool {
	t.Helper()
	cert, roots := fakeCertificate(t)
	f.tls = &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return roots
}

func (f *fakeServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if f.silent {
		_, _ = io.Copy(io.Discard, conn) // until the client gives up
		return
	}
	inTLS := f.security == "tls"
	if inTLS {
		conn = tls.Server(conn, f.tls)
	}
	tp := textproto.NewConn(conn)
	reply := func(line string) { _ = tp.PrintfLine("%s", line) }
	reply("220 falso ESMTP")
	r := receivedMsg{inTLS: inTLS}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(cmd) {
		case "EHLO", "HELO":
			ext := []string{"falso"}
			if f.security == "starttls" && !inTLS && !f.noSTARTTLS {
				ext = append(ext, "STARTTLS")
			}
			if f.user != "" {
				ext = append(ext, "AUTH PLAIN")
			}
			ext = append(ext, "8BITMIME")
			for i, e := range ext {
				sep := "-"
				if i == len(ext)-1 {
					sep = " "
				}
				reply("250" + sep + e)
			}
		case "STARTTLS":
			reply("220 pode")
			tc := tls.Server(conn, f.tls)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, inTLS, r.inTLS = tc, true, true
			tp = textproto.NewConn(conn)
		case "AUTH":
			mechanism, credential, _ := strings.Cut(arg, " ")
			b, _ := base64.StdEncoding.DecodeString(credential)
			parts := strings.Split(string(b), "\x00")
			if len(parts) == 3 {
				f.mu.Lock()
				f.logins = append(f.logins, parts[1])
				f.mu.Unlock()
			}
			if mechanism == "PLAIN" && len(parts) == 3 && parts[1] == f.user && parts[2] == f.password {
				reply("235 2.7.0 aceito")
			} else {
				// On two lines, like Gmail's.
				reply("535-5.7.8 usuario e senha recusados")
				reply("535 5.7.8 veja a ajuda")
			}
		case "MAIL":
			r.from = arg
			reply("250 ok")
		case "RCPT":
			if f.rejectRcpt != "" && strings.Contains(arg, f.rejectRcpt) {
				reply("550 5.1.1 caixa inexistente")
				continue
			}
			r.to = append(r.to, arg)
			reply("250 ok")
		case "DATA":
			reply("354 pode mandar")
			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			r.data = string(data)
			f.mu.Lock()
			f.received = append(f.received, r)
			f.mu.Unlock()
			reply("250 2.0.0 na fila")
		case "QUIT":
			reply("221 tchau")
			return
		case "RSET", "NOOP":
			reply("250 ok")
		default:
			reply("502 não conheço")
		}
	}
}

func (f *fakeServer) receivedSoFar() ([]receivedMsg, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]receivedMsg(nil), f.received...), append([]string(nil), f.logins...)
}

// fakeCertificate generates, on the spot, a self-signed certificate for 127.0.0.1.
func fakeCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
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
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(c)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}
