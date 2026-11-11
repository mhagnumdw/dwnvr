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

// TestWithoutAttachmentComesOutAsBefore: a message without attachments is
// the same, byte for byte, as before attachments existed. It's the
// set-password link message and the Diagnostics test one. The expected value
// was captured from build before attachments; only the Message-ID, drawn at
// random, is left out.
func TestWithoutAttachmentComesOutAsBefore(t *testing.T) {
	from, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := build(from, testMessage, time.Date(2026, 10, 27, 14, 32, 0, 0, time.FixedZone("", -3*3600)))
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
		t.Errorf("message without attachment changed:\n%q\nwant\n%q", got, want)
	}
}

// TestManyRecipients: a long list goes out with one line per address in To,
// none over the 998 characters of RFC 5322, and the mail reader reads all of
// them back.
func TestManyRecipients(t *testing.T) {
	m := testMessage
	m.To = nil
	for i := range 60 {
		m.To = append(m.To, fmt.Sprintf("pessoa.com.um.nome.comprido.%02d@exemplo.com", i))
	}
	from, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := build(from, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line %d with %d characters", i+1, len(line))
		}
	}
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	parsedParts, err := msg.Header.AddressList("To")
	if err != nil || len(parsedParts) != len(m.To) {
		t.Fatalf("%d addresses read (%v), want %d", len(parsedParts), err, len(m.To))
	}
	for i, a := range parsedParts {
		if a.Address != m.To[i] {
			t.Errorf("address %d: %q, want %q", i, a.Address, m.To[i])
		}
	}
}

// TestLongSubject: a long subject, with or without accents, goes out in
// several lines, none over the 76 characters of RFC 2047, and the mail reader
// reads it back whole. The usual alert's already goes past 76 on the first
// encoded word.
func TestLongSubject(t *testing.T) {
	from, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	for name, subject := range map[string]string{
		// 30 words of 40 letters go past 998 characters on a single line.
		"accents": strings.TrimSpace(strings.Repeat(strings.Repeat("ã", 40)+" ", 30)),
		"ascii":   strings.TrimSpace(strings.Repeat(strings.Repeat("a", 40)+" ", 30)),
		"alert":   "[Alerta] Garagem(Portão, Calçada e Rampa): pessoa e veículo às 14:32",
	} {
		m := testMessage
		m.Subject = subject
		b, err := build(from, m, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		header, _, _ := strings.Cut(string(b), "\r\n\r\n")
		inSubject := false
		for line := range strings.SplitSeq(header, "\r\n") {
			inSubject = strings.HasPrefix(line, "Subject:") || inSubject && strings.HasPrefix(line, " ")
			if inSubject && len(line) > 76 {
				t.Errorf("%s: subject line with %d characters: %q", name, len(line), line)
			}
		}
		msg, err := mail.ReadMessage(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
		if err != nil || parsed != m.Subject {
			t.Errorf("%s: subject read %q (%v), expected %q", name, parsed, err, m.Subject)
		}
	}
}

// TestSendsWithAttachment: with two recipients and two attachments, a
// single message arrives, and a mail reader takes from it the text and each
// attachment whole, with the accented name. Every line fits the base64
// limit.
func TestSendsWithAttachment(t *testing.T) {
	// Every one of the 256 byte values, as in a JPEG, several times: the
	// attachment spans many lines.
	frame := bytes.Repeat(func() []byte {
		b := make([]byte, 256)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}(), 40)
	m := testMessage
	m.To = []string{"fulano@exemplo.com", "ciclana@exemplo.com"}
	m.Attachments = []Attachment{
		{Name: "detecção às 14h32.jpg", Type: "image/jpeg", Data: frame},
		{Name: "vazio.txt", Type: "text/plain", Data: nil},
	}

	f := &fakeServer{security: "tls"}
	if err := client(t, f).Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	received, _ := f.receivedSoFar()
	if len(received) != 1 || len(received[0].to) != 2 {
		t.Fatalf("received %+v, want 1 for both", received)
	}
	// SMTP rejects lines longer than 998 characters. The fake server reads with
	// textproto, which already strips the \r from each line.
	data := received[0].data
	for i, line := range strings.Split(data, "\n") {
		if len(line) > 998 {
			t.Fatalf("line %d with %d characters", i+1, len(line))
		}
	}

	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.Header.Get("To"); got != "fulano@exemplo.com, ciclana@exemplo.com" {
		t.Errorf("To %q", got)
	}
	ctype, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || ctype != "multipart/mixed" {
		t.Fatalf("Content-Type %q (%v)", msg.Header.Get("Content-Type"), err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])

	text := nextPart(t, parts)
	if text.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("text part with Content-Type %q", text.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(quotedprintable.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(string(body), "\r\n", "\n"); got != m.Text {
		t.Errorf("text\n%q\nwant\n%q", got, m.Text)
	}

	for _, a := range m.Attachments {
		p := nextPart(t, parts)
		if p.FileName() != a.Name || p.Header.Get("Content-Type") != a.Type {
			t.Errorf("attachment %q of type %q, expected %q of type %q",
				p.FileName(), p.Header.Get("Content-Type"), a.Name, a.Type)
		}
		raw, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if len(line) > Base64LineLen {
				t.Errorf("attachment %q: line %d with %d characters", a.Name, i+1, len(line))
			}
		}
		// The base64 decoder skips the line breaks.
		parsed, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil || !bytes.Equal(parsed, a.Data) {
			t.Errorf("attachment %q with %d bytes, expected %d (%v)", a.Name, len(parsed), len(a.Data), err)
		}
	}
	if _, err := parts.NextPart(); err != io.EOF {
		t.Errorf("extra part: %v", err)
	}
}

// nextPart reads the next part without Go's automatic decoding: NextPart
// strips the quoted-printable on its own, and the test wants to see what
// arrived.
func nextPart(t *testing.T, parts *multipart.Reader) *multipart.Part {
	t.Helper()
	p, err := parts.NextRawPart()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestWithHTMLAndInlineImage: with HTML, one inline attachment and one
// loose, the message goes out in layers (see build), and a mail reader finds
// in each what it carries: the whole text and HTML, the image by the
// Content-ID the HTML cites, and the loose attachment last.
func TestWithHTMLAndInlineImage(t *testing.T) {
	m := testMessage
	m.HTML = `<p>Olá, <img src="cid:quadro@dwnvr"></p>`
	m.Attachments = []Attachment{
		{Name: "quadro.jpg", Type: "image/jpeg", Data: []byte("jpeg"), InlineID: "quadro@dwnvr"},
		{Name: "solto.txt", Type: "text/plain", Data: []byte("solto")},
	}
	from, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := build(from, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}

	mixed := partsOf(t, "multipart/mixed", msg.Header.Get("Content-Type"), msg.Body)
	related := partsOf(t, "multipart/related", mixed[0].Header.Get("Content-Type"), mixed[0].reader())
	alternative := partsOf(t, "multipart/alternative", related[0].Header.Get("Content-Type"), related[0].reader())
	if len(mixed) != 2 || len(related) != 2 || len(alternative) != 2 {
		t.Fatalf("parts: %d in mixed, %d in related, %d in alternative", len(mixed), len(related), len(alternative))
	}
	for i, want := range []struct{ ctype, body string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		p := alternative[i]
		body, _ := io.ReadAll(quotedprintable.NewReader(p.reader()))
		if p.Header.Get("Content-Type") != want.ctype || strings.ReplaceAll(string(body), "\r\n", "\n") != want.body {
			t.Errorf("alternative part %d: %q with %q", i, p.Header.Get("Content-Type"), body)
		}
	}
	img := related[1]
	if img.Header.Get("Content-ID") != "<quadro@dwnvr>" || !strings.HasPrefix(img.Header.Get("Content-Disposition"), "inline;") {
		t.Errorf("image with Content-ID %q and Content-Disposition %q",
			img.Header.Get("Content-ID"), img.Header.Get("Content-Disposition"))
	}
	if loose := mixed[1]; loose.name() != "solto.txt" ||
		!strings.HasPrefix(loose.Header.Get("Content-Disposition"), "attachment;") {
		t.Errorf("loose attachment %q with %q", loose.name(), loose.Header.Get("Content-Disposition"))
	}
}

// TestHTMLOnlyWithoutAttachment: with HTML only, the root is the alternative.
func TestHTMLOnlyWithoutAttachment(t *testing.T) {
	m := testMessage
	m.HTML = "<p>Olá</p>"
	from, _ := mail.ParseAddress("dwnvr <dwnvr@exemplo.com>")
	b, err := build(from, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if parts := partsOf(t, "multipart/alternative", msg.Header.Get("Content-Type"), msg.Body); len(parts) != 2 {
		t.Errorf("%d parts in alternative", len(parts))
	}
}

// TestInlineAttachmentOnlyWithHTML: without HTML there's nowhere to show
// an inline attachment, and an id with what doesn't fit in Content-ID is
// rejected.
func TestInlineAttachmentOnlyWithHTML(t *testing.T) {
	m := testMessage
	m.Attachments = []Attachment{{Name: "q.jpg", Type: "image/jpeg", InlineID: "quadro"}}
	if m.check() == nil {
		t.Error("inline attachment without HTML got through")
	}
	m.HTML = "<p></p>"
	if err := m.check(); err != nil {
		t.Errorf("with HTML: %v", err)
	}
	m.Attachments[0].InlineID = "quadro>\r\nBcc: x"
	if m.check() == nil {
		t.Error("id with a line break got through")
	}
}

// readPart is a multipart part already read whole: the next part's reader
// invalidates the previous one.
type readPart struct {
	Header textproto.MIMEHeader
	data   []byte
}

func (p readPart) reader() io.Reader { return bytes.NewReader(p.data) }

func (p readPart) name() string {
	_, params, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	return params["filename"]
}

// partsOf reads the parts of a multipart of the expected type.
func partsOf(t *testing.T, ctype, contentType string, body io.Reader) []readPart {
	t.Helper()
	got, params, err := mime.ParseMediaType(contentType)
	if err != nil || got != ctype {
		t.Fatalf("Content-Type %q, expected %s (%v)", contentType, ctype, err)
	}
	r := multipart.NewReader(body, params["boundary"])
	var parts []readPart
	for {
		p, err := r.NextRawPart()
		if errors.Is(err, io.EOF) {
			return parts
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, readPart{Header: p.Header, data: data})
	}
}
