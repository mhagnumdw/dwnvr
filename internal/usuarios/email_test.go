package usuarios

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEmailSoOEndereco: o e-mail é um endereço só, sem nome e sem lista, e
// o espaço em volta sai.
func TestEmailSoOEndereco(t *testing.T) {
	c := cadastroVazio(t)
	conv, err := c.Criar("maria", "Maria", "  maria@exemplo.com ")
	if err != nil {
		t.Fatal(err)
	}
	if conv.Usuario.Email != "maria@exemplo.com" || conv.Usuario.EmailValido() != "maria@exemplo.com" {
		t.Errorf("e-mail %q", conv.Usuario.Email)
	}
	var recusado Recusado
	for i, email := range []string{
		"maria",
		"Maria <maria@exemplo.com>",
		"maria@exemplo.com, joao@exemplo.com",
		strings.Repeat("a", TamanhoMaximoDoEmail) + "@exemplo.com",
	} {
		if _, err := c.Criar("pessoa"+string(rune('a'+i)), "Pessoa", email); !errors.As(err, &recusado) {
			t.Errorf("%q: erro %v, esperava Recusado", email, err)
		}
	}
}

// TestEmailUnico: duas pessoas não têm o mesmo e-mail, nem com outra caixa,
// para um futuro login pelo e-mail saber quem entra.
func TestEmailUnico(t *testing.T) {
	c := cadastroVazio(t)
	if _, err := c.Criar("maria", "Maria", "Maria@Exemplo.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Criar("joao", "João", ""); err != nil {
		t.Fatal(err)
	}
	var recusado Recusado
	if _, err := c.Criar("ana", "Ana", "maria@exemplo.com"); !errors.As(err, &recusado) {
		t.Errorf("criar com o e-mail da Maria: %v", err)
	}
	if _, err := c.TrocarEmail("joao", "MARIA@exemplo.com"); !errors.As(err, &recusado) {
		t.Errorf("trocar para o e-mail da Maria: %v", err)
	}
	// O próprio e-mail, com outra caixa, não conflita com ele mesmo.
	if _, err := c.TrocarEmail("maria", "maria@exemplo.com"); err != nil {
		t.Errorf("a Maria trocando a caixa do próprio e-mail: %v", err)
	}
}

// TestTrocarEmail: troca e apaga sem mexer no link nem na senha.
func TestTrocarEmail(t *testing.T) {
	c := cadastroVazio(t)
	conv, err := c.Criar("maria", "Maria", "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.TrocarEmail("maria", "maria@exemplo.com")
	if err != nil || u.Email != "maria@exemplo.com" {
		t.Fatalf("trocar: %+v, %v", u, err)
	}
	if u.Link == nil || u.Link.TokenSha256 != conv.Usuario.Link.TokenSha256 {
		t.Error("trocar o e-mail mexeu no link")
	}
	if u, err = c.TrocarEmail("maria", ""); err != nil || u.Email != "" {
		t.Errorf("apagar: %+v, %v", u, err)
	}
	if _, err := c.TrocarEmail("ninguem", "x@exemplo.com"); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("pessoa que não existe: %v", err)
	}
}

// TestEmailRuimNoArquivoNaoSome: o e-mail escrito errado à mão vira aviso e
// conta como sem e-mail, mas fica no arquivo, como a entrada inválida.
func TestEmailRuimNoArquivoNaoSome(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	arquivo := `{"owner":{},"users":[
		{"username":"maria","name":"Maria","role":"viewer","email":"Maria <maria@exemplo.com>","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"joao","name":"João","role":"viewer","email":"joao@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"ana","name":"Ana","role":"viewer","email":"JOAO@exemplo.com","createdAt":"2026-10-24T10:00:00Z"}
	]}`
	if err := os.WriteFile(caminho, []byte(arquivo), 0o600); err != nil {
		t.Fatal(err)
	}
	c, avisos, err := Abrir(caminho, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(avisos) != 2 || !strings.Contains(avisos[0], "maria") || !strings.Contains(avisos[1], `o mesmo de "joao"`) {
		t.Errorf("avisos %q", avisos)
	}
	maria, _ := c.Buscar("maria")
	if maria.EmailValido() != "" {
		t.Errorf("e-mail ruim valendo: %q", maria.EmailValido())
	}

	// O repetido também conta: ninguém mais pega o e-mail do João enquanto a
	// cópia da Ana estiver no arquivo, nem ele mesmo.
	var recusado Recusado
	if _, err := c.TrocarEmail("joao", "joao@exemplo.com"); !errors.As(err, &recusado) {
		t.Errorf("regravar o e-mail repetido: %v", err)
	}

	// Gravar outra pessoa não apaga o e-mail ruim do arquivo.
	if _, err := c.TrocarEmail("ana", ""); err != nil {
		t.Fatal(err)
	}
	relido, _, err := Abrir(caminho, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if maria, _ := relido.Buscar("maria"); maria.Email != "Maria <maria@exemplo.com>" {
		t.Errorf("o e-mail ruim virou %q no arquivo", maria.Email)
	}
}

// TestBuscarPorEmail: acha sem diferença de caixa; o e-mail repetido no
// arquivo e a entrada ignorada não acham ninguém.
func TestBuscarPorEmail(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	arquivo := `{"owner":{},"users":[
		{"username":"maria","name":"Maria","role":"viewer","email":"Maria@Exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"joao","name":"João","role":"viewer","email":"joao@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"ana","name":"Ana","role":"viewer","email":"JOAO@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"Zé","name":"Zé","role":"viewer","email":"ze@exemplo.com","createdAt":"2026-10-24T10:00:00Z"}
	]}`
	if err := os.WriteFile(caminho, []byte(arquivo), 0o600); err != nil {
		t.Fatal(err)
	}
	c, avisos, err := Abrir(caminho, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := c.BuscarPorEmail("maria@exemplo.com"); !ok || u.Usuario != "maria" {
		t.Errorf("maria pelo e-mail: %+v, %v", u, ok)
	}
	for _, email := range []string{"joao@exemplo.com", "ze@exemplo.com", "ninguem@exemplo.com", ""} {
		if u, ok := c.BuscarPorEmail(email); ok {
			t.Errorf("%q achou %q", email, u.Usuario)
		}
	}
	if !slices.ContainsFunc(avisos, func(a string) bool { return strings.Contains(a, "nenhum dos dois entra pelo e-mail") }) {
		t.Errorf("o aviso do repetido não diz que ninguém entra pelo e-mail: %q", avisos)
	}

	// Tirar o repetido pela tela tira o aviso e solta o e-mail, sem reiniciar.
	if _, err := c.TrocarEmail("ana", ""); err != nil {
		t.Fatal(err)
	}
	if u, ok := c.BuscarPorEmail("JOAO@exemplo.com"); !ok || u.Usuario != "joao" {
		t.Errorf("joao depois de tirar o repetido: %+v, %v", u, ok)
	}
	if av := c.Avisos(); len(av) != 1 || !strings.Contains(av[0], `"Zé"`) {
		t.Errorf("avisos depois de tirar o repetido: %q", av)
	}
}
