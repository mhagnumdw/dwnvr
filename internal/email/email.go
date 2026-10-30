// Package email envia os e-mails do dwnvr pelo servidor SMTP da seção smtp
// do dwnvr.yaml. Só com a biblioteca padrão: sem dependência nova e sem C.
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

// Enviador entrega uma mensagem pronta ao servidor de e-mail. Promete voltar
// em até PrazoDoEnvio, e o erro traz a resposta do servidor do jeito que ela
// veio: é o que o admin lê na tela para corrigir a configuração. Entregue ao
// servidor não é entregue na caixa da pessoa: o que acontece depois (spam,
// caixa cheia) o dwnvr não vê.
type Enviador interface {
	Enviar(ctx context.Context, m Mensagem) error
}

// Mensagem é um e-mail de texto, com anexos ou sem. Para leva só endereços,
// sem nome: "fulano@exemplo.com".
type Mensagem struct {
	Para    []string
	Assunto string
	Texto   string
	Anexos  []Anexo
}

// Anexo é um arquivo que vai junto do texto, como o quadro do alerta. Ele vai
// em base64, como todo anexo de e-mail: o SMTP só leva texto em linhas
// curtas, e o leitor de e-mail decodifica de volta. Custa um terço a mais no
// tamanho.
type Anexo struct {
	// Nome é como ele aparece para quem recebe: "deteccao.jpg".
	Nome string
	// Tipo é o tipo MIME, sem parâmetros: "image/jpeg".
	Tipo  string
	Dados []byte
}

// confere recusa o que viraria outro cabeçalho: um assunto com quebra de
// linha, um destinatário com nome e vírgula, ou o nome e o tipo de um anexo
// que não cabem no cabeçalho dele, escreveriam por cima da mensagem.
func (m Mensagem) confere() error {
	if len(m.Para) == 0 {
		return errors.New("mensagem sem destinatário")
	}
	for _, p := range m.Para {
		if a, err := mail.ParseAddress(p); err != nil || a.Address != p {
			return fmt.Errorf("destinatário %q não é um endereço de e-mail", p)
		}
	}
	if strings.ContainsAny(m.Assunto, "\r\n") {
		return errors.New("assunto com quebra de linha")
	}
	for _, a := range m.Anexos {
		// O FormatMediaType devolve vazio para o que não cabe no cabeçalho;
		// o nome com acento ele codifica (RFC 2231).
		if a.Nome == "" || strings.ContainsAny(a.Nome, "\r\n") || disposicao(a) == "" {
			return fmt.Errorf("anexo com nome %q", a.Nome)
		}
		if mime.FormatMediaType(a.Tipo, nil) == "" {
			return fmt.Errorf("anexo %q com tipo %q", a.Nome, a.Tipo)
		}
	}
	return nil
}

func disposicao(a Anexo) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": a.Nome})
}

// montar escreve a mensagem no formato da RFC 5322. O texto vai em
// quoted-printable: o acento passa por servidor que só aceita 7 bits, e a
// linha longa, como a do link, quebra sem estragar.
//
// Sem anexo, o texto é a mensagem inteira, como sempre foi. Com anexo, ela
// vira multipart/mixed: o texto primeiro, e depois cada anexo.
func montar(de *mail.Address, m Mensagem, agora time.Time) ([]byte, error) {
	id, err := idDaMensagem(de.Address)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	cabecalho := func(nome, valor string) { fmt.Fprintf(&b, "%s: %s\r\n", nome, valor) }
	cabecalho("From", de.String())
	cabecalho("To", strings.Join(m.Para, ", "))
	cabecalho("Subject", mime.QEncoding.Encode("utf-8", m.Assunto))
	cabecalho("Date", agora.Format(time.RFC1123Z))
	cabecalho("Message-ID", id)
	cabecalho("MIME-Version", "1.0")

	if len(m.Anexos) == 0 {
		cabecalho("Content-Type", "text/plain; charset=utf-8")
		cabecalho("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := escreveTexto(&b, m.Texto); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}

	partes := multipart.NewWriter(&b)
	cabecalho("Content-Type", mime.FormatMediaType("multipart/mixed",
		map[string]string{"boundary": partes.Boundary()}))
	b.WriteString("\r\n")
	texto, err := partes.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	if err := escreveTexto(texto, m.Texto); err != nil {
		return nil, err
	}
	for _, a := range m.Anexos {
		parte, err := partes.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {a.Tipo},
			"Content-Disposition":       {disposicao(a)},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, err
		}
		if err := escreveBase64(parte, a.Dados); err != nil {
			return nil, err
		}
	}
	if err := partes.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func escreveTexto(w io.Writer, texto string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(texto)); err != nil {
		return err
	}
	return qp.Close()
}

// escreveBase64 quebra o base64 em linhas de LinhaDoBase64: o encoder da
// biblioteca escreve uma linha só, e o servidor recusa linha longa.
func escreveBase64(w io.Writer, dados []byte) error {
	s := base64.StdEncoding.EncodeToString(dados)
	for len(s) > 0 {
		n := min(len(s), LinhaDoBase64)
		if _, err := io.WriteString(w, s[:n]+"\r\n"); err != nil {
			return err
		}
		s = s[n:]
	}
	return nil
}

// idDaMensagem sorteia um Message-ID no domínio do remetente, como pede a
// RFC 5322: único no mundo, e de quem o escreveu.
func idDaMensagem(remetente string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	_, dominio, _ := strings.Cut(remetente, "@")
	return "<" + hex.EncodeToString(b) + "@" + dominio + ">", nil
}
