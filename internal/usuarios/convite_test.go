package usuarios

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cadastroVazio(t *testing.T) *Cadastro {
	t.Helper()
	c, _, err := Abrir(filepath.Join(t.TempDir(), "usuarios.json"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// O caminho inteiro: criar, abrir o link, definir a senha e entrar com ela.
func TestConviteDoCriarAoLogin(t *testing.T) {
	c := cadastroVazio(t)
	conv, err := c.Criar("maria", "  Maria  ", "")
	if err != nil {
		t.Fatal(err)
	}
	if conv.Usuario.Nome != "Maria" || conv.Usuario.Papel != PapelViewer || conv.Usuario.Senha != "" {
		t.Errorf("criada como %+v", conv.Usuario)
	}
	if got := conv.Usuario.Situacao(time.Now()); got != "invitePending" {
		t.Errorf("situação %q, esperava invitePending", got)
	}

	// O arquivo guarda o hash do token, nunca o token.
	b, _ := os.ReadFile(c.caminho)
	if strings.Contains(string(b), conv.Token) {
		t.Error("o token foi parar no usuarios.json")
	}

	if u, err := c.Conferir(conv.Token); err != nil || u.Usuario != "maria" {
		t.Fatalf("conferir: %+v, %v", u, err)
	}
	if _, err := c.Conferir(conv.Token + "x"); !errors.Is(err, ErrLinkInvalido) {
		t.Errorf("token errado: %v", err)
	}

	u, err := c.DefinirSenha(context.Background(), conv.Token, "uma senha boa")
	if err != nil {
		t.Fatal(err)
	}
	if u.Link != nil || u.Situacao(time.Now()) != "active" || u.AtivoDesde.IsZero() {
		t.Errorf("depois de definir: %+v", u)
	}
	if ok, err := ConferirSenha(context.Background(), "uma senha boa", u.Senha); !ok || err != nil {
		t.Errorf("a senha definida não confere: ok=%v err=%v", ok, err)
	}

	// Uso único.
	if _, err := c.DefinirSenha(context.Background(), conv.Token, "outra senha boa"); !errors.Is(err, ErrLinkInvalido) {
		t.Errorf("o link valeu duas vezes: %v", err)
	}
}

func TestCriarRecusa(t *testing.T) {
	c := cadastroVazio(t)
	if _, err := c.Criar("maria", "Maria", ""); err != nil {
		t.Fatal(err)
	}
	casos := map[string][3]string{ // usuário, nome e o campo recusado
		"repetido":          {"maria", "Outra Maria", CampoUsuario},
		"o dono":            {"admin", "Admin", CampoUsuario},
		"fora do formato":   {"Maria", "Maria", CampoUsuario},
		"sem nome":          {"jose", "   ", CampoNome},
		"nome comprido":     {"jose", strings.Repeat("a", TamanhoMaximoDoNome+1), CampoNome},
		"nome com controle": {"jose", "Jo\x00sé", CampoNome},
	}
	for nome, caso := range casos {
		_, err := c.Criar(caso[0], caso[1], "")
		conferirRecusa(t, nome, err, caso[2])
	}
	if n := len(c.Listar()); n != 1 {
		t.Errorf("%d pessoas no cadastro, esperava só a maria", n)
	}
}

// Link novo apaga a senha e invalida o link anterior: só o mais recente vale.
func TestNovoLinkApagaASenhaEOLinkAnterior(t *testing.T) {
	c := cadastroVazio(t)
	primeiro, _ := c.Criar("maria", "Maria", "")
	if _, err := c.DefinirSenha(context.Background(), primeiro.Token, "uma senha boa"); err != nil {
		t.Fatal(err)
	}

	segundo, err := c.NovoLink("maria")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := c.Buscar("maria")
	if u.Senha != "" {
		t.Error("a senha continuou valendo depois do link novo")
	}
	if !u.AtivoDesde.IsZero() {
		t.Error("o ativo desde continuou depois do link novo")
	}

	terceiro, _ := c.NovoLink("maria")
	if _, err := c.Conferir(segundo.Token); !errors.Is(err, ErrLinkInvalido) {
		t.Errorf("o link substituído ainda vale: %v", err)
	}
	if _, err := c.Conferir(terceiro.Token); err != nil {
		t.Errorf("o link mais novo não vale: %v", err)
	}

	if _, err := c.NovoLink("ninguem"); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("link para quem não existe: %v", err)
	}
}

func TestLinkVence(t *testing.T) {
	c := cadastroVazio(t)
	conv, _ := c.Criar("maria", "Maria", "")

	c.agora = func() time.Time { return time.Now().Add(ValidadeDoLink + time.Second) }
	if _, err := c.Conferir(conv.Token); !errors.Is(err, ErrLinkInvalido) {
		t.Errorf("link vencido aceito: %v", err)
	}
	if _, err := c.DefinirSenha(context.Background(), conv.Token, "uma senha boa"); !errors.Is(err, ErrLinkInvalido) {
		t.Errorf("senha definida com link vencido: %v", err)
	}
	u, _ := c.Buscar("maria")
	if got := u.Situacao(c.agora()); got != "inviteExpired" {
		t.Errorf("situação %q, esperava inviteExpired", got)
	}
}

func TestSenhaForaDoTamanho(t *testing.T) {
	c := cadastroVazio(t)
	conv, _ := c.Criar("maria", "Maria", "")
	// Conta caracteres, e não bytes: "ação" tem 4 caracteres e 6 bytes.
	for _, s := range []string{"1234567", "açãoaçã", strings.Repeat("a", SenhaMaxima+1)} {
		var recusado Recusado
		if _, err := c.DefinirSenha(context.Background(), conv.Token, s); !errors.As(err, &recusado) {
			t.Errorf("senha de %d caracteres: %v", len([]rune(s)), err)
		}
	}
	// A recusa não gasta o link.
	if _, err := c.Conferir(conv.Token); err != nil {
		t.Errorf("a senha recusada gastou o link: %v", err)
	}
}

func TestRemover(t *testing.T) {
	c := cadastroVazio(t)
	for _, u := range [][2]string{{"maria", "Maria"}, {"jose", "José"}} {
		if _, err := c.Criar(u[0], u[1], ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Remover("maria"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Buscar("maria"); ok {
		t.Error("a maria continua no cadastro")
	}
	if _, ok := c.Buscar("jose"); !ok {
		t.Error("o josé sumiu junto")
	}
	if err := c.Remover("maria"); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("remover de novo: %v", err)
	}
}

// conferirRecusa confere que err é a recusa de um formulário com o campo
// recusado, e só ele.
func conferirRecusa(t *testing.T, caso string, err error, campo string) {
	t.Helper()
	var recusas Recusas
	if !errors.As(err, &recusas) {
		t.Errorf("%s: err=%v, esperava Recusas", caso, err)
		return
	}
	if _, ok := recusas[campo]; !ok || len(recusas) != 1 {
		t.Errorf("%s: recusas %q, esperava só o campo %q", caso, recusas, campo)
	}
}

// TestCriarRecusaCadaCampo: com mais de um campo errado, vem a recusa de cada
// um, para a tela pôr cada mensagem embaixo do seu campo.
func TestCriarRecusaCadaCampo(t *testing.T) {
	c := cadastroVazio(t)
	_, err := c.Criar("Maria", "", "maria")
	var recusas Recusas
	if !errors.As(err, &recusas) || len(recusas) != 3 {
		t.Fatalf("err=%v, esperava as recusas dos 3 campos", err)
	}
	// A primeira, na ordem da tela (nome, usuário, e-mail), é a mensagem de
	// quem mostra uma só.
	if recusas.Error() != recusas[CampoNome] {
		t.Errorf("Error() = %q, esperava a do nome", recusas.Error())
	}
}

// TestConferirNovoEhAReguaDoCriar: o que a conferência aceita, o Criar aceita;
// o que ela recusa, ele recusa com a mesma mensagem. E ela não grava nada.
func TestConferirNovoEhAReguaDoCriar(t *testing.T) {
	c := cadastroVazio(t)
	if _, err := c.Criar("maria", "Maria", "maria@exemplo.com"); err != nil {
		t.Fatal(err)
	}
	casos := [][3]string{
		{"joao", "João", "joao@exemplo.com"},
		{"joao", "João", ""},
		{"maria", "Outra Maria", "outra@exemplo.com"},
		{"admin", "Admin", ""},
		{"Joao", " ", "MARIA@exemplo.com"},
		{"ana", "Ana", "Ana <ana@exemplo.com>"},
	}
	for _, caso := range casos {
		conferida, err := c.ConferirNovo(caso[0], caso[1], caso[2])
		if err != nil {
			t.Fatal(err)
		}
		if n := c.Quantos(); n != 1 {
			t.Fatalf("%q: a conferência gravou (%d entradas)", caso, n)
		}
		_, err = c.Criar(caso[0], caso[1], caso[2])
		var criada Recusas
		errors.As(err, &criada)
		if (len(conferida) == 0 && err != nil) || !maps.Equal(conferida, criada) {
			t.Errorf("%q: conferida %q, Criar %v", caso, conferida, err)
		}
		if err == nil {
			if err := c.Remover(caso[0]); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestConferirNovoContaAEntradaIgnorada: o login de uma entrada inválida do
// arquivo não está na lista da tela, mas o Criar o recusa, e a conferência
// também.
func TestConferirNovoContaAEntradaIgnorada(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	arquivo := `{"owner":{},"users":[
		{"username":"maria","name":"Maria","role":"chefe","email":"maria@exemplo.com","createdAt":"2026-10-24T10:00:00Z"}
	]}`
	if err := os.WriteFile(caminho, []byte(arquivo), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Abrir(caminho, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Listar()) != 0 {
		t.Fatal("a entrada de papel desconhecido devia ficar de fora")
	}
	// O login e o e-mail da mesma entrada: o e-mail também é recusado, e não
	// fica verde só porque o login proposto é o dela.
	if r, _ := c.ConferirNovo("maria", "Maria", "MARIA@exemplo.com"); r[CampoUsuario] == "" || r[CampoEmail] == "" {
		t.Errorf("recusas %q, esperava o usuário e o e-mail", r)
	}
}

// TestConferirEmail: a régua do TrocarEmail, sem gravar.
func TestConferirEmail(t *testing.T) {
	c := cadastroVazio(t)
	for _, u := range [][3]string{{"maria", "Maria", "maria@exemplo.com"}, {"joao", "João", ""}} {
		if _, err := c.Criar(u[0], u[1], u[2]); err != nil {
			t.Fatal(err)
		}
	}
	casos := map[string]struct {
		usuario, email string
		recusa         bool
	}{
		"o da Maria":             {"joao", "Maria@Exemplo.com", true},
		"fora do formato":        {"joao", "joao", true},
		"vazio apaga":            {"joao", "", false},
		"novo":                   {"joao", "joao@exemplo.com", false},
		"o próprio, outra caixa": {"maria", "MARIA@exemplo.com", false},
	}
	for nome, caso := range casos {
		r, err := c.ConferirEmail(caso.usuario, caso.email)
		if err != nil {
			t.Fatalf("%s: %v", nome, err)
		}
		if (r[CampoEmail] != "") != caso.recusa || len(r) > 1 {
			t.Errorf("%s: recusas %q", nome, r)
		}
	}
	if u, _ := c.Buscar("joao"); u.Email != "" {
		t.Errorf("a conferência gravou o e-mail %q", u.Email)
	}
	if _, err := c.ConferirEmail("ninguem", "x@exemplo.com"); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("pessoa que não existe: %v", err)
	}
}

// TestConferirComArquivoIlegivel: com o usuarios.json ilegível o cadastro em
// memória está vazio, e a conferência devolve o erro em vez de "tudo certo".
func TestConferirComArquivoIlegivel(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	if err := os.WriteFile(caminho, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Abrir(caminho, "admin")
	if err == nil {
		t.Fatal("o arquivo quebrado devia ser ilegível")
	}
	if _, err := c.ConferirNovo("maria", "Maria", ""); err == nil {
		t.Error("ConferirNovo com o arquivo ilegível não deu erro")
	}
	if _, err := c.ConferirEmail("maria", ""); err == nil || errors.Is(err, ErrNaoExiste) {
		t.Errorf("ConferirEmail com o arquivo ilegível: %v, esperava o erro do arquivo", err)
	}
}
