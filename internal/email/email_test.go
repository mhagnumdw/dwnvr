package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
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

// TestMuitosDestinatarios: a lista longa sai com uma linha por endereço no
// To, nenhuma acima dos 998 caracteres da RFC 5322, e o leitor de e-mail lê
// todos de volta.
func TestMuitosDestinatarios(t *testing.T) {
	m := mensagemDoTeste
	m.Para = nil
	for i := range 60 {
		m.Para = append(m.Para, fmt.Sprintf("pessoa.com.um.nome.comprido.%02d@exemplo.com", i))
	}
	de, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := montar(de, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i, linha := range strings.Split(string(b), "\r\n") {
		if len(linha) > 998 {
			t.Fatalf("linha %d com %d caracteres", i+1, len(linha))
		}
	}
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	lidos, err := msg.Header.AddressList("To")
	if err != nil || len(lidos) != len(m.Para) {
		t.Fatalf("%d endereços lidos (%v), esperava %d", len(lidos), err, len(m.Para))
	}
	for i, a := range lidos {
		if a.Address != m.Para[i] {
			t.Errorf("endereço %d: %q, esperava %q", i, a.Address, m.Para[i])
		}
	}
}

// TestAssuntoLongo: o assunto comprido, com acento ou sem, sai em várias
// linhas, nenhuma acima dos 76 caracteres da RFC 2047, e o leitor de e-mail o
// lê de volta inteiro. O do alerta comum já passa dos 76 na primeira palavra
// codificada.
func TestAssuntoLongo(t *testing.T) {
	de, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	for nome, assunto := range map[string]string{
		// 30 palavras de 40 letras passam dos 998 caracteres numa linha só.
		"acento": strings.TrimSpace(strings.Repeat(strings.Repeat("ã", 40)+" ", 30)),
		"ascii":  strings.TrimSpace(strings.Repeat(strings.Repeat("a", 40)+" ", 30)),
		"alerta": "[Alerta] Garagem(Portão, Calçada e Rampa): pessoa e veículo às 14:32",
	} {
		m := mensagemDoTeste
		m.Assunto = assunto
		b, err := montar(de, m, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		cabecalho, _, _ := strings.Cut(string(b), "\r\n\r\n")
		noAssunto := false
		for linha := range strings.SplitSeq(cabecalho, "\r\n") {
			noAssunto = strings.HasPrefix(linha, "Subject:") || noAssunto && strings.HasPrefix(linha, " ")
			if noAssunto && len(linha) > 76 {
				t.Errorf("%s: linha do assunto com %d caracteres: %q", nome, len(linha), linha)
			}
		}
		msg, err := mail.ReadMessage(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		lido, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
		if err != nil || lido != m.Assunto {
			t.Errorf("%s: assunto lido %q (%v), esperado %q", nome, lido, err, m.Assunto)
		}
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

// TestComHTMLEImagemNoCorpo: com HTML, um anexo no corpo e outro solto, a
// mensagem sai em camadas (ver montar), e um leitor de e-mail acha em cada
// uma o que ela leva: o texto e o HTML inteiros, a imagem pelo Content-ID que
// o HTML cita, e o anexo solto por último.
func TestComHTMLEImagemNoCorpo(t *testing.T) {
	m := mensagemDoTeste
	m.HTML = `<p>Olá, <img src="cid:quadro@dwnvr"></p>`
	m.Anexos = []Anexo{
		{Nome: "quadro.jpg", Tipo: "image/jpeg", Dados: []byte("jpeg"), NoCorpo: "quadro@dwnvr"},
		{Nome: "solto.txt", Tipo: "text/plain", Dados: []byte("solto")},
	}
	de, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := montar(de, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}

	misto := partesDe(t, "multipart/mixed", msg.Header.Get("Content-Type"), msg.Body)
	relacionado := partesDe(t, "multipart/related", misto[0].Header.Get("Content-Type"), misto[0].leitor())
	alternativo := partesDe(t, "multipart/alternative", relacionado[0].Header.Get("Content-Type"), relacionado[0].leitor())
	if len(misto) != 2 || len(relacionado) != 2 || len(alternativo) != 2 {
		t.Fatalf("partes: %d no mixed, %d no related, %d no alternative", len(misto), len(relacionado), len(alternativo))
	}
	for i, want := range []struct{ tipo, corpo string }{
		{"text/plain; charset=utf-8", m.Texto},
		{"text/html; charset=utf-8", m.HTML},
	} {
		p := alternativo[i]
		corpo, _ := io.ReadAll(quotedprintable.NewReader(p.leitor()))
		if p.Header.Get("Content-Type") != want.tipo || strings.ReplaceAll(string(corpo), "\r\n", "\n") != want.corpo {
			t.Errorf("parte %d do alternative: %q com %q", i, p.Header.Get("Content-Type"), corpo)
		}
	}
	img := relacionado[1]
	if img.Header.Get("Content-ID") != "<quadro@dwnvr>" || !strings.HasPrefix(img.Header.Get("Content-Disposition"), "inline;") {
		t.Errorf("imagem com Content-ID %q e Content-Disposition %q",
			img.Header.Get("Content-ID"), img.Header.Get("Content-Disposition"))
	}
	if solto := misto[1]; solto.nome() != "solto.txt" ||
		!strings.HasPrefix(solto.Header.Get("Content-Disposition"), "attachment;") {
		t.Errorf("anexo solto %q com %q", solto.nome(), solto.Header.Get("Content-Disposition"))
	}
}

// TestSoHTMLSemAnexo: só com o HTML, a raiz é o alternative.
func TestSoHTMLSemAnexo(t *testing.T) {
	m := mensagemDoTeste
	m.HTML = "<p>Olá</p>"
	de, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := montar(de, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if partes := partesDe(t, "multipart/alternative", msg.Header.Get("Content-Type"), msg.Body); len(partes) != 2 {
		t.Errorf("%d partes no alternative", len(partes))
	}
}

// TestAnexoNoCorpoSoComHTML: sem HTML não há onde mostrar o anexo no corpo,
// e o id com o que não cabe no Content-ID é recusado.
func TestAnexoNoCorpoSoComHTML(t *testing.T) {
	m := mensagemDoTeste
	m.Anexos = []Anexo{{Nome: "q.jpg", Tipo: "image/jpeg", NoCorpo: "quadro"}}
	if m.confere() == nil {
		t.Error("anexo no corpo sem HTML passou")
	}
	m.HTML = "<p></p>"
	if err := m.confere(); err != nil {
		t.Errorf("com HTML: %v", err)
	}
	m.Anexos[0].NoCorpo = "quadro>\r\nBcc: x"
	if m.confere() == nil {
		t.Error("id com quebra de linha passou")
	}
}

// parteLida é uma parte de multipart já lida inteira: o leitor da parte
// seguinte invalida a anterior.
type parteLida struct {
	Header textproto.MIMEHeader
	dados  []byte
}

func (p parteLida) leitor() io.Reader { return bytes.NewReader(p.dados) }

func (p parteLida) nome() string {
	_, params, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	return params["filename"]
}

// partesDe lê as partes de um multipart do tipo esperado.
func partesDe(t *testing.T, tipo, contentType string, corpo io.Reader) []parteLida {
	t.Helper()
	got, params, err := mime.ParseMediaType(contentType)
	if err != nil || got != tipo {
		t.Fatalf("Content-Type %q, esperado %s (%v)", contentType, tipo, err)
	}
	r := multipart.NewReader(corpo, params["boundary"])
	var partes []parteLida
	for {
		p, err := r.NextRawPart()
		if errors.Is(err, io.EOF) {
			return partes
		}
		if err != nil {
			t.Fatal(err)
		}
		dados, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		partes = append(partes, parteLida{Header: p.Header, dados: dados})
	}
}
