package email

import (
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

const senhaDoTeste = "troque-me"

// cliente monta o SMTP apontado para o servidor falso, aceitando o
// certificado dele.
func cliente(t *testing.T, f *servidorFalso) *SMTP {
	t.Helper()
	raizes := novoServidorFalso(t, f)
	cfg := config.SMTP{Host: "127.0.0.1", Port: f.porta, Security: f.seguranca,
		From: "dwnvr <dwnvr@exemplo.com>"}
	if f.usuario != "" {
		cfg.Username, cfg.Password = f.usuario, senhaDoTeste
	}
	s, err := NovoSMTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.raizes = raizes
	return s
}

// O link do convite tem 43 caracteres de token: a linha passa dos 76 do
// quoted-printable e precisa voltar inteira. O token é repetido de propósito,
// para não parecer uma chave de verdade ao betterleaks.
var linkDoTeste = "https://nvr.exemplo.com/#convite?token=" + strings.Repeat("token-falso", 4)[:43]

var mensagemDoTeste = Mensagem{
	Para:    []string{"fulano@exemplo.com"},
	Assunto: "Defina sua senha no dwnvr, João",
	Texto:   "Olá, João Silva.\n\nAbra o link abaixo:\n\n" + linkDoTeste + "\n\nSeu usuário: joao.silva\n",
}

// TestEnviaNosTresModos: a mensagem chega inteira, com o acento e o link
// intactos, sem TLS, com STARTTLS e com TLS direto.
func TestEnviaNosTresModos(t *testing.T) {
	for _, seg := range []string{"none", "starttls", "tls"} {
		t.Run(seg, func(t *testing.T) {
			f := &servidorFalso{seguranca: seg, usuario: "dwnvr@exemplo.com", senha: senhaDoTeste}
			if err := cliente(t, f).Enviar(context.Background(), mensagemDoTeste); err != nil {
				t.Fatal(err)
			}
			recebidas, logins := f.recebido()
			if len(recebidas) != 1 {
				t.Fatalf("%d mensagens recebidas, esperada 1", len(recebidas))
			}
			r := recebidas[0]
			if r.emTLS != (seg != "none") {
				t.Errorf("em TLS %v com security %s", r.emTLS, seg)
			}
			if len(logins) != 1 || logins[0] != "dwnvr@exemplo.com" {
				t.Errorf("logins %v", logins)
			}
			if !strings.HasPrefix(r.de, "FROM:<dwnvr@exemplo.com>") ||
				len(r.para) != 1 || r.para[0] != "TO:<fulano@exemplo.com>" {
				t.Errorf("envelope de %q para %v", r.de, r.para)
			}
			confereMensagem(t, r.dados)
		})
	}
}

// confereMensagem lê o que chegou como um leitor de e-mail leria.
func confereMensagem(t *testing.T, dados string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(dados))
	if err != nil {
		t.Fatal(err)
	}
	de, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil || de.Name != "dwnvr" || de.Address != "dwnvr@exemplo.com" {
		t.Errorf("From %q (%v)", msg.Header.Get("From"), err)
	}
	if got := msg.Header.Get("To"); got != "fulano@exemplo.com" {
		t.Errorf("To %q", got)
	}
	assunto, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || assunto != mensagemDoTeste.Assunto {
		t.Errorf("Subject %q, lido %q (%v)", msg.Header.Get("Subject"), assunto, err)
	}
	for _, h := range []string{"Date", "Message-ID"} {
		if msg.Header.Get(h) == "" {
			t.Errorf("sem %s", h)
		}
	}
	if !strings.HasSuffix(msg.Header.Get("Message-ID"), "@exemplo.com>") {
		t.Errorf("Message-ID %q fora do domínio do remetente", msg.Header.Get("Message-ID"))
	}
	if got := msg.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}
	corpo, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	if texto := strings.ReplaceAll(string(corpo), "\r\n", "\n"); texto != mensagemDoTeste.Texto {
		t.Errorf("corpo\n%q\nesperado\n%q", texto, mensagemDoTeste.Texto)
	}
}

// TestSemSTARTTLSASenhaNaoSai: o servidor que não oferece STARTTLS não
// recebe a senha nem a mensagem.
func TestSemSTARTTLSASenhaNaoSai(t *testing.T) {
	f := &servidorFalso{seguranca: "starttls", semSTARTTLS: true, usuario: "dwnvr@exemplo.com",
		senha: senhaDoTeste}
	err := cliente(t, f).Enviar(context.Background(), mensagemDoTeste)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("erro %v, esperado o do STARTTLS", err)
	}
	if recebidas, logins := f.recebido(); len(recebidas) != 0 || len(logins) != 0 {
		t.Errorf("%d mensagens e %d logins chegaram sem TLS", len(recebidas), len(logins))
	}
}

// TestErrosDoServidorChegamComoVieram: o admin lê a resposta do servidor na
// tela, com o código, para saber o que corrigir.
func TestErrosDoServidorChegamComoVieram(t *testing.T) {
	for _, c := range []struct {
		nome   string
		f      *servidorFalso
		espera []string
	}{
		{"senha errada", &servidorFalso{seguranca: "tls", usuario: "dwnvr@exemplo.com", senha: "outra"},
			[]string{"recusou o login: 535 5.7.8 usuario e senha recusados 5.7.8 veja a ajuda"}},
		{"destinatário", &servidorFalso{seguranca: "tls", recusaPara: "fulano@exemplo.com"},
			[]string{"recusou o destinatário fulano@exemplo.com", "550 5.1.1"}},
	} {
		err := cliente(t, c.f).Enviar(context.Background(), mensagemDoTeste)
		for _, e := range c.espera {
			if err == nil || !strings.Contains(err.Error(), e) {
				t.Errorf("%s: erro %v, esperado com %q", c.nome, err, e)
			}
		}
		if recebidas, _ := c.f.recebido(); len(recebidas) != 0 {
			t.Errorf("%s: mensagem entregue mesmo com o erro", c.nome)
		}
	}
}

// TestServidorMudoEstouraOPrazo: o envio não fica preso num servidor que
// aceita a conexão e não responde.
func TestServidorMudoEstouraOPrazo(t *testing.T) {
	s := cliente(t, &servidorFalso{seguranca: "starttls", mudo: true})
	s.prazo = 200 * time.Millisecond
	inicio := time.Now()
	err := s.Enviar(context.Background(), mensagemDoTeste)
	if err == nil || !strings.Contains(err.Error(), "não respondeu em 200ms") {
		t.Errorf("erro %v, esperado o do prazo", err)
	}
	if d := time.Since(inicio); d > 2*time.Second {
		t.Errorf("esperou %v com prazo de 200ms", d)
	}
}

// TestCertificadoDesconhecidoRecusa: o TLS confere o certificado; um
// autoassinado que ninguém aceitou não passa.
func TestCertificadoDesconhecidoRecusa(t *testing.T) {
	f := &servidorFalso{seguranca: "tls", usuario: "dwnvr@exemplo.com", senha: senhaDoTeste}
	s := cliente(t, f)
	s.raizes = nil // as do sistema, que não conhecem o certificado falso
	if err := s.Enviar(context.Background(), mensagemDoTeste); err == nil {
		t.Fatal("enviou para um certificado que ninguém aceitou")
	}
	if _, logins := f.recebido(); len(logins) != 0 {
		t.Error("a senha saiu antes de o certificado ser conferido")
	}
}

// TestMensagemQueViraCabecalhoRecusa: assunto com quebra de linha ou
// destinatário com nome escreveriam outro cabeçalho; nada é enviado.
func TestMensagemQueViraCabecalhoRecusa(t *testing.T) {
	f := &servidorFalso{seguranca: "tls"}
	s := cliente(t, f)
	for _, m := range []Mensagem{
		{Para: []string{"fulano@exemplo.com"}, Assunto: "oi\r\nBcc: outro@exemplo.com", Texto: "x"},
		{Para: []string{"Fulano <fulano@exemplo.com>"}, Assunto: "oi", Texto: "x"},
		{Para: []string{"fulano@exemplo.com, outro@exemplo.com"}, Assunto: "oi", Texto: "x"},
		{Assunto: "oi", Texto: "x"},
		comAnexo(Anexo{Nome: "a.jpg\r\nBcc: outro@exemplo.com", Tipo: "image/jpeg"}),
		comAnexo(Anexo{Nome: "", Tipo: "image/jpeg"}),
		comAnexo(Anexo{Nome: "a.jpg", Tipo: "image/jpeg\r\nBcc: outro@exemplo.com"}),
		comAnexo(Anexo{Nome: "a.jpg", Tipo: "image/jpeg; name=b.exe"}),
		comAnexo(Anexo{Nome: "a.jpg", Tipo: ""}),
	} {
		if err := s.Enviar(context.Background(), m); err == nil {
			t.Errorf("enviou %+v", m)
		}
	}
	if recebidas, _ := f.recebido(); len(recebidas) != 0 {
		t.Errorf("%d mensagens chegaram", len(recebidas))
	}
}

func comAnexo(a Anexo) Mensagem {
	return Mensagem{Para: []string{"fulano@exemplo.com"}, Assunto: "oi", Texto: "x", Anexos: []Anexo{a}}
}

// TestDesligadoNaoMonta: o main só monta o envio com a seção conferida.
func TestDesligadoNaoMonta(t *testing.T) {
	for _, cfg := range []config.SMTP{{}, {Host: "smtp.exemplo.com", Recusa: "smtp.from vazio"}} {
		if _, err := NovoSMTP(cfg); err == nil {
			t.Errorf("montou com %+v", cfg)
		}
	}
}
