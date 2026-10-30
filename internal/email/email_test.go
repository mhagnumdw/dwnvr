package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestSemAnexoSaiComoAntes: a mensagem sem anexo é a mesma, byte a byte, de
// antes de o anexo existir. Ela é a do link de definir a senha e a do teste do
// Diagnóstico. O esperado foi capturado do montar anterior ao anexo; só o
// Message-ID, sorteado, fica de fora.
func TestSemAnexoSaiComoAntes(t *testing.T) {
	de, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := montar(de, mensagemDoTeste, time.Date(2026, 10, 27, 14, 32, 0, 0, time.FixedZone("", -3*3600)))
	if err != nil {
		t.Fatal(err)
	}
	got := regexp.MustCompile(`Message-ID: <[0-9a-f]{32}@`).ReplaceAllString(string(b), "Message-ID: <ID@")
	want := "From: \"dwnvr\" <dwnvr@exemplo.com>\r\n" +
		"To: fulano@exemplo.com\r\n" +
		"Subject: =?utf-8?q?Defina_sua_senha_no_dwnvr,_Jo=C3=A3o?=\r\n" +
		"Date: Tue, 27 Oct 2026 14:32:00 -0300\r\n" +
		"Message-ID: <ID@exemplo.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Ol=C3=A1, Jo=C3=A3o Silva.\r\n\r\nAbra o link abaixo:\r\n\r\n" +
		"https://nvr.exemplo.com/#convite?token=3Dtoken-falsotoken-falsotoken-falsot=\r\noken-fals\r\n\r\n" +
		"Seu usu=C3=A1rio: joao.silva\r\n"
	if got != want {
		t.Errorf("mensagem sem anexo mudou:\n%q\nesperada\n%q", got, want)
	}
}

// TestEnviaComAnexo: com dois destinatários e dois anexos, chega uma mensagem
// só, e um leitor de e-mail tira dela o texto e cada anexo inteiros, com o
// nome com acento. Toda linha cabe no limite do base64.
func TestEnviaComAnexo(t *testing.T) {
	// Todos os 256 valores de byte, como num JPEG, várias vezes: o anexo
	// passa de muitas linhas.
	quadro := bytes.Repeat(func() []byte {
		b := make([]byte, 256)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}(), 40)
	m := mensagemDoTeste
	m.Para = []string{"fulano@exemplo.com", "ciclana@exemplo.com"}
	m.Anexos = []Anexo{
		{Nome: "detecção às 14h32.jpg", Tipo: "image/jpeg", Dados: quadro},
		{Nome: "vazio.txt", Tipo: "text/plain", Dados: nil},
	}

	f := &servidorFalso{seguranca: "tls"}
	if err := cliente(t, f).Enviar(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	recebidas, _ := f.recebido()
	if len(recebidas) != 1 || len(recebidas[0].para) != 2 {
		t.Fatalf("recebidas %+v, esperada 1 para os 2", recebidas)
	}
	// O SMTP recusa linha com mais de 998 caracteres. O servidor falso lê
	// pelo textproto, que já tira o \r de cada linha.
	dados := recebidas[0].dados
	for i, linha := range strings.Split(dados, "\n") {
		if len(linha) > 998 {
			t.Fatalf("linha %d com %d caracteres", i+1, len(linha))
		}
	}

	msg, err := mail.ReadMessage(strings.NewReader(dados))
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.Header.Get("To"); got != "fulano@exemplo.com, ciclana@exemplo.com" {
		t.Errorf("To %q", got)
	}
	tipo, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || tipo != "multipart/mixed" {
		t.Fatalf("Content-Type %q (%v)", msg.Header.Get("Content-Type"), err)
	}
	partes := multipart.NewReader(msg.Body, params["boundary"])

	texto := proximaParte(t, partes)
	if texto.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("parte do texto com Content-Type %q", texto.Header.Get("Content-Type"))
	}
	corpo, err := io.ReadAll(quotedprintable.NewReader(texto))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(string(corpo), "\r\n", "\n"); got != m.Texto {
		t.Errorf("texto\n%q\nesperado\n%q", got, m.Texto)
	}

	for _, a := range m.Anexos {
		p := proximaParte(t, partes)
		if p.FileName() != a.Nome || p.Header.Get("Content-Type") != a.Tipo {
			t.Errorf("anexo %q de tipo %q, esperado %q de tipo %q",
				p.FileName(), p.Header.Get("Content-Type"), a.Nome, a.Tipo)
		}
		bruto, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, linha := range strings.Split(string(bruto), "\n") {
			if len(linha) > LinhaDoBase64 {
				t.Errorf("anexo %q: linha %d com %d caracteres", a.Nome, i+1, len(linha))
			}
		}
		// O decodificador do base64 pula as quebras de linha.
		lido, err := base64.StdEncoding.DecodeString(string(bruto))
		if err != nil || !bytes.Equal(lido, a.Dados) {
			t.Errorf("anexo %q com %d bytes, esperados %d (%v)", a.Nome, len(lido), len(a.Dados), err)
		}
	}
	if _, err := partes.NextPart(); err != io.EOF {
		t.Errorf("parte a mais: %v", err)
	}
}

// proximaParte lê a parte seguinte sem a decodificação automática do Go: o
// NextPart tira o quoted-printable sozinho, e o teste quer ver o que chegou.
func proximaParte(t *testing.T, partes *multipart.Reader) *multipart.Part {
	t.Helper()
	p, err := partes.NextRawPart()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
