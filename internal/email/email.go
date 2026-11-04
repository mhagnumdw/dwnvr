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

// Mensagem é um e-mail de texto, com anexos ou sem, e com uma versão em HTML
// ou sem. Para leva só endereços, sem nome: "fulano@exemplo.com".
type Mensagem struct {
	Para    []string
	Assunto string
	// Texto vai sempre: é a mensagem inteira sem HTML, e com HTML é o que
	// mostra o leitor de e-mail que não mostra HTML.
	Texto string
	// HTML, quando há, é o que o leitor de e-mail mostra no lugar do Texto. É
	// o documento inteiro, já escapado por quem o montou.
	HTML   string
	Anexos []Anexo
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
	// NoCorpo, quando preenchido, faz o anexo aparecer dentro do HTML, e não
	// como arquivo solto: o HTML o mostra com <img src="cid:NoCorpo">. Só
	// vale com HTML, e só com letras, dígitos e ".-_@".
	NoCorpo string
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
		if a.NoCorpo != "" && (m.HTML == "" || !idValido(a.NoCorpo)) {
			return fmt.Errorf("anexo %q no corpo com id %q", a.Nome, a.NoCorpo)
		}
	}
	return nil
}

// idValido aceita o id de um anexo no corpo só com o que não precisa de
// escape nem no cabeçalho Content-ID nem no cid: do HTML.
func idValido(id string) bool {
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune(".-_@", c) {
			return false
		}
	}
	return true
}

func disposicao(a Anexo) string {
	tipo := "attachment"
	if a.NoCorpo != "" {
		tipo = "inline"
	}
	return mime.FormatMediaType(tipo, map[string]string{"filename": a.Nome})
}

// montar escreve a mensagem no formato da RFC 5322. O texto e o HTML vão em
// quoted-printable: o acento passa por servidor que só aceita 7 bits, e a
// linha longa, como a do link, quebra sem estragar.
//
// Sem HTML e sem anexo, o texto é a mensagem inteira, como sempre foi. O
// resto se monta por camadas, cada uma só quando há o que pôr nela:
//
//	multipart/mixed          os anexos soltos, depois do corpo
//	  multipart/related      o HTML e os anexos que ele mostra no corpo
//	    multipart/alternative o texto e o HTML: o leitor mostra o último que sabe
//	      text/plain
//	      text/html
//	    image/jpeg (inline)
//	  image/jpeg (attachment)
func montar(de *mail.Address, m Mensagem, agora time.Time) ([]byte, error) {
	id, err := idDaMensagem(de.Address)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	cabecalho := func(nome, valor string) { fmt.Fprintf(&b, "%s: %s\r\n", nome, valor) }
	cabecalho("From", de.String())
	// Uma linha por endereço: a RFC 5322 limita a linha a 998 caracteres, e
	// com uns 35 endereços numa linha só o servidor rigoroso recusaria a
	// mensagem inteira. A quebra seguida de espaço continua o mesmo campo, e
	// com um endereço só nada muda.
	cabecalho("To", strings.Join(m.Para, ",\r\n "))
	cabecalho("Subject", mime.QEncoding.Encode("utf-8", m.Assunto))
	cabecalho("Date", agora.Format(time.RFC1123Z))
	cabecalho("Message-ID", id)
	cabecalho("MIME-Version", "1.0")

	raiz := corpoDe(m)
	cabecalho("Content-Type", raiz.tipo)
	if raiz.codificacao != "" {
		cabecalho("Content-Transfer-Encoding", raiz.codificacao)
	}
	b.WriteString("\r\n")
	if err := raiz.escreve(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// parte é um pedaço da mensagem: o cabeçalho dele e quem escreve o conteúdo.
// A raiz tem o cabeçalho escrito no da mensagem; as outras, dentro do
// multipart de cima.
type parte struct {
	tipo, codificacao, disposicao, id string
	escreve                           func(io.Writer) error
}

func (p parte) cabecalho() textproto.MIMEHeader {
	h := textproto.MIMEHeader{"Content-Type": {p.tipo}}
	if p.codificacao != "" {
		h.Set("Content-Transfer-Encoding", p.codificacao)
	}
	if p.disposicao != "" {
		h.Set("Content-Disposition", p.disposicao)
	}
	if p.id != "" {
		h.Set("Content-ID", "<"+p.id+">")
	}
	return h
}

// corpoDe arma as camadas de montar.
func corpoDe(m Mensagem) parte {
	corpo := textoQP("text/plain; charset=utf-8", m.Texto)
	var soltos, noCorpo []parte
	for _, a := range m.Anexos {
		if a.NoCorpo != "" {
			noCorpo = append(noCorpo, anexo(a))
		} else {
			soltos = append(soltos, anexo(a))
		}
	}
	if m.HTML != "" {
		corpo = multipartDe("multipart/alternative", corpo, textoQP("text/html; charset=utf-8", m.HTML))
		if len(noCorpo) > 0 {
			corpo = multipartDe("multipart/related", append([]parte{corpo}, noCorpo...)...)
		}
	}
	if len(soltos) > 0 {
		corpo = multipartDe("multipart/mixed", append([]parte{corpo}, soltos...)...)
	}
	return corpo
}

func textoQP(tipo, texto string) parte {
	return parte{tipo: tipo, codificacao: "quoted-printable",
		escreve: func(w io.Writer) error { return escreveTexto(w, texto) }}
}

func anexo(a Anexo) parte {
	return parte{tipo: a.Tipo, codificacao: "base64", disposicao: disposicao(a), id: a.NoCorpo,
		escreve: func(w io.Writer) error { return escreveBase64(w, a.Dados) }}
}

// multipartDe junta as filhas num multipart. O separador é sorteado agora,
// porque vai no Content-Type, que é escrito antes do conteúdo.
func multipartDe(tipo string, filhas ...parte) parte {
	separador := multipart.NewWriter(io.Discard).Boundary()
	return parte{
		tipo: mime.FormatMediaType(tipo, map[string]string{"boundary": separador}),
		escreve: func(w io.Writer) error {
			mw := multipart.NewWriter(w)
			if err := mw.SetBoundary(separador); err != nil {
				return err
			}
			for _, f := range filhas {
				pw, err := mw.CreatePart(f.cabecalho())
				if err != nil {
					return err
				}
				if err := f.escreve(pw); err != nil {
					return err
				}
			}
			return mw.Close()
		},
	}
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
