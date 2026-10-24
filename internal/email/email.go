// Package email envia os e-mails do dwnvr pelo servidor SMTP da seção smtp
// do dwnvr.yaml. Só com a biblioteca padrão: sem dependência nova e sem C.
package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
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

// Mensagem é um e-mail de texto puro. Para leva só endereços, sem nome:
// "fulano@exemplo.com".
type Mensagem struct {
	Para    []string
	Assunto string
	Texto   string
}

// confere recusa o que viraria outro cabeçalho: um assunto com quebra de
// linha, ou um destinatário com nome e vírgula, escreveria por cima da
// mensagem.
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
	return nil
}

// montar escreve a mensagem no formato da RFC 5322. O texto vai em
// quoted-printable: o acento passa por servidor que só aceita 7 bits, e a
// linha longa, como a do link, quebra sem estragar.
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
	cabecalho("Content-Type", "text/plain; charset=utf-8")
	cabecalho("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(m.Texto)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
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
