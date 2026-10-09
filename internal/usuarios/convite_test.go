package usuarios

import (
	"context"
	"errors"
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
	conv, err := c.Criar("maria", "  Maria  ")
	if err != nil {
		t.Fatal(err)
	}
	if conv.Usuario.Nome != "Maria" || conv.Usuario.Papel != PapelComum || conv.Usuario.Senha != "" {
		t.Errorf("criada como %+v", conv.Usuario)
	}
	if got := conv.Usuario.Situacao(time.Now()); got != "linkAberto" {
		t.Errorf("situação %q, esperava linkAberto", got)
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
	if u.Link != nil || u.Situacao(time.Now()) != "ativo" {
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
	if _, err := c.Criar("maria", "Maria"); err != nil {
		t.Fatal(err)
	}
	casos := map[string][2]string{
		"repetido":          {"maria", "Outra Maria"},
		"o dono":            {"admin", "Admin"},
		"fora do formato":   {"Maria", "Maria"},
		"sem nome":          {"jose", "   "},
		"nome comprido":     {"jose", strings.Repeat("a", TamanhoMaximoDoNome+1)},
		"nome com controle": {"jose", "Jo\x00sé"},
	}
	for nome, caso := range casos {
		var recusado Recusado
		if _, err := c.Criar(caso[0], caso[1]); !errors.As(err, &recusado) {
			t.Errorf("%s: err=%v, esperava Recusado", nome, err)
		}
	}
	if n := len(c.Listar()); n != 1 {
		t.Errorf("%d pessoas no cadastro, esperava só a maria", n)
	}
}

// Link novo apaga a senha e invalida o link anterior: só o mais recente vale.
func TestNovoLinkApagaASenhaEOLinkAnterior(t *testing.T) {
	c := cadastroVazio(t)
	primeiro, _ := c.Criar("maria", "Maria")
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
	conv, _ := c.Criar("maria", "Maria")

	c.agora = func() time.Time { return time.Now().Add(ValidadeDoLink + time.Second) }
	if _, err := c.Conferir(conv.Token); !errors.Is(err, ErrLinkInvalido) {
		t.Errorf("link vencido aceito: %v", err)
	}
	if _, err := c.DefinirSenha(context.Background(), conv.Token, "uma senha boa"); !errors.Is(err, ErrLinkInvalido) {
		t.Errorf("senha definida com link vencido: %v", err)
	}
	u, _ := c.Buscar("maria")
	if got := u.Situacao(c.agora()); got != "linkVencido" {
		t.Errorf("situação %q, esperava linkVencido", got)
	}
}

func TestSenhaForaDoTamanho(t *testing.T) {
	c := cadastroVazio(t)
	conv, _ := c.Criar("maria", "Maria")
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
		if _, err := c.Criar(u[0], u[1]); err != nil {
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
