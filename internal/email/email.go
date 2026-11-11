// Package email sends dwnvr's e-mails through the SMTP server in the smtp
// section of dwnvr.yaml. Standard library only: no new dependency and no C.
package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Sender hands a ready message to the mail server. It promises to return
// within SendTimeout, and the error carries the server's answer as it came:
// it's what the admin reads on screen to fix the configuration, so its text
// is in pt-BR until i18n. Handed to the server is not delivered to the
// person's inbox: what happens after that (spam, full mailbox) dwnvr doesn't
// see.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Message is a text e-mail, with or without attachments, and with or without
// an HTML version. To carries only addresses, no name: "fulano@exemplo.com".
type Message struct {
	To      []string
	Subject string
	// Text always goes: it's the whole message without HTML, and with HTML
	// it's what a mail reader that doesn't show HTML displays.
	Text string
	// HTML, when present, is what the mail reader shows instead of Text. It's
	// the whole document, already escaped by whoever built it.
	HTML        string
	Attachments []Attachment
}

// Attachment is a file that goes along with the text, like the alert frame.
// It goes in base64, like every e-mail attachment: SMTP only carries text in
// short lines, and the mail reader decodes it back. It costs a third more in
// size.
type Attachment struct {
	// Name is how it shows up to the recipient: "deteccao.jpg".
	Name string
	// Type is the MIME type, without parameters: "image/jpeg".
	Type string
	Data []byte
	// InlineID, when set, makes the attachment show up inside the HTML, and
	// not as a loose file: the HTML shows it with <img src="cid:InlineID">.
	// Only valid with HTML, and only with letters, digits and ".-_@".
	InlineID string
}

// check rejects what would become another header: a subject with a line
// break, a recipient with a name and a comma, or an attachment name and type
// that don't fit in its header, would write over the message.
func (m Message) check() error {
	if len(m.To) == 0 {
		return errors.New("mensagem sem destinatário")
	}
	for _, p := range m.To {
		if a, err := mail.ParseAddress(p); err != nil || a.Address != p {
			return fmt.Errorf("destinatário %q não é um endereço de e-mail", p)
		}
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("assunto com quebra de linha")
	}
	for _, a := range m.Attachments {
		// FormatMediaType returns empty for what doesn't fit in the header; a
		// name with accents it encodes (RFC 2231).
		if a.Name == "" || strings.ContainsAny(a.Name, "\r\n") || disposition(a) == "" {
			return fmt.Errorf("anexo com nome %q", a.Name)
		}
		if mime.FormatMediaType(a.Type, nil) == "" {
			return fmt.Errorf("anexo %q com tipo %q", a.Name, a.Type)
		}
		if a.InlineID != "" && (m.HTML == "" || !validID(a.InlineID)) {
			return fmt.Errorf("anexo %q no corpo com id %q", a.Name, a.InlineID)
		}
	}
	return nil
}

// validID accepts an inline attachment id only with what needs no escaping,
// neither in the Content-ID header nor in the HTML cid:.
func validID(id string) bool {
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune(".-_@", c) {
			return false
		}
	}
	return true
}

func disposition(a Attachment) string {
	ctype := "attachment"
	if a.InlineID != "" {
		ctype = "inline"
	}
	return mime.FormatMediaType(ctype, map[string]string{"filename": a.Name})
}

// build writes the message in RFC 5322 format. The text and the HTML go in
// quoted-printable: accents survive a server that only takes 7 bits, and a
// long line, like the link's, wraps without breaking.
//
// Without HTML and without attachments, the text is the whole message, as it
// always was. The rest is built in layers, each only when there's something to
// put in it:
//
//	multipart/mixed          the loose attachments, after the body
//	  multipart/related      the HTML and the attachments it shows inline
//	    multipart/alternative the text and the HTML: the reader shows the last one it can
//	      text/plain
//	      text/html
//	    image/jpeg (inline)
//	  image/jpeg (attachment)
func build(from *mail.Address, m Message, now time.Time) ([]byte, error) {
	id, err := messageID(from.Address)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", from.String())
	// One line per address: RFC 5322 limits a line to 998 characters, and with
	// some 35 addresses on a single line a strict server would reject the
	// whole message. A line break followed by a space continues the same
	// field, and with a single address nothing changes.
	header("To", strings.Join(m.To, ",\r\n "))
	// The subject is folded into lines too: the alert's carries the areas, and
	// with long accented names it would go past 998 characters on one line.
	b.WriteString(folded("Subject", mime.QEncoding.Encode("utf-8", m.Subject)))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", id)
	header("MIME-Version", "1.0")

	root := bodyOf(m)
	header("Content-Type", root.ctype)
	if root.encoding != "" {
		header("Content-Transfer-Encoding", root.encoding)
	}
	b.WriteString("\r\n")
	if err := root.write(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// folded writes a header broken at the spaces, so no line goes past 76
// characters, the RFC 2047 limit for a line with an encoded word; the hard
// RFC 5322 limit is 998. A line break followed by a space continues the same
// field, and the reader joins the lines losing nothing. The encoded value only
// has spaces between the encoded words, of up to 75 characters each, and the
// reader ignores the space between them. The first word that doesn't fit after
// the name goes to the next line; a word longer than the line stays whole.
func folded(name, value string) string {
	const maxLine = 76
	var b strings.Builder
	b.WriteString(name + ":")
	line := len(name) + 1
	for word := range strings.SplitSeq(value, " ") {
		if line+1+len(word) > maxLine {
			b.WriteString("\r\n")
			line = 0
		}
		b.WriteString(" " + word)
		line += 1 + len(word)
	}
	b.WriteString("\r\n")
	return b.String()
}

// part is a piece of the message: its header and what writes the content. The
// root has its header written in the message's; the others, inside the
// enclosing multipart.
type part struct {
	ctype, encoding, disposition, id string
	write                            func(io.Writer) error
}

func (p part) header() textproto.MIMEHeader {
	h := textproto.MIMEHeader{"Content-Type": {p.ctype}}
	if p.encoding != "" {
		h.Set("Content-Transfer-Encoding", p.encoding)
	}
	if p.disposition != "" {
		h.Set("Content-Disposition", p.disposition)
	}
	if p.id != "" {
		h.Set("Content-ID", "<"+p.id+">")
	}
	return h
}

// bodyOf assembles build's layers.
func bodyOf(m Message) part {
	body := textQP("text/plain; charset=utf-8", m.Text)
	var loose, inline []part
	for _, a := range m.Attachments {
		if a.InlineID != "" {
			inline = append(inline, attachmentPart(a))
		} else {
			loose = append(loose, attachmentPart(a))
		}
	}
	if m.HTML != "" {
		body = multipartOf("multipart/alternative", body, textQP("text/html; charset=utf-8", m.HTML))
		if len(inline) > 0 {
			body = multipartOf("multipart/related", append([]part{body}, inline...)...)
		}
	}
	if len(loose) > 0 {
		body = multipartOf("multipart/mixed", append([]part{body}, loose...)...)
	}
	return body
}

func textQP(ctype, text string) part {
	return part{ctype: ctype, encoding: "quoted-printable",
		write: func(w io.Writer) error { return writeText(w, text) }}
}

func attachmentPart(a Attachment) part {
	return part{ctype: a.Type, encoding: "base64", disposition: disposition(a), id: a.InlineID,
		write: func(w io.Writer) error { return writeBase64(w, a.Data) }}
}

// multipartOf joins the children in a multipart. The boundary is drawn now,
// because it goes in the Content-Type, which is written before the content.
func multipartOf(ctype string, children ...part) part {
	boundary := multipart.NewWriter(io.Discard).Boundary()
	return part{
		ctype: mime.FormatMediaType(ctype, map[string]string{"boundary": boundary}),
		write: func(w io.Writer) error {
			mw := multipart.NewWriter(w)
			if err := mw.SetBoundary(boundary); err != nil {
				return err
			}
			for _, f := range children {
				pw, err := mw.CreatePart(f.header())
				if err != nil {
					return err
				}
				if err := f.write(pw); err != nil {
					return err
				}
			}
			return mw.Close()
		},
	}
}

func writeText(w io.Writer, text string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(text)); err != nil {
		return err
	}
	return qp.Close()
}

// writeBase64 breaks the base64 into Base64LineLen lines: the library's
// encoder writes a single line, and the server rejects long lines.
func writeBase64(w io.Writer, data []byte) error {
	s := base64.StdEncoding.EncodeToString(data)
	for len(s) > 0 {
		n := min(len(s), Base64LineLen)
		if _, err := io.WriteString(w, s[:n]+"\r\n"); err != nil {
			return err
		}
		s = s[n:]
	}
	return nil
}

// messageID draws a Message-ID in the sender's domain, as RFC 5322 asks:
// unique in the world, and owned by whoever wrote it.
func messageID(sender string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	_, domain, _ := strings.Cut(sender, "@")
	return "<" + hex.EncodeToString(b) + "@" + domain + ">", nil
}
