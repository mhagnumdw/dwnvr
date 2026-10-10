package usuarios

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArquivoQueNaoExisteEhCadastroVazio(t *testing.T) {
	c, avisos, err := Abrir(filepath.Join(t.TempDir(), "usuarios.json"), "admin")
	if err != nil || len(avisos) != 0 || c.Quantos() != 0 {
		t.Fatalf("err=%v avisos=%v quantos=%d", err, avisos, c.Quantos())
	}
	if _, ok := c.Buscar("maria"); ok {
		t.Error("achou alguém num cadastro vazio")
	}
}

func TestAlterarGravaERelê(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	c, _, _ := Abrir(caminho, "admin")
	err := c.Alterar(func(a *Arquivo) error {
		a.Dono.Nome = "Dono"
		a.Usuarios = append(a.Usuarios, Usuario{Usuario: "maria", Nome: "Maria", Papel: PapelComum, Senha: "$x$y"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("modo %v, esperava 0600: o arquivo tem as senhas guardadas", info.Mode().Perm())
	}

	de, _, err := Abrir(caminho, "admin")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := de.Buscar("maria")
	if !ok || u.Nome != "Maria" || u.Papel != PapelComum || u.Senha != "$x$y" || de.Dono().Nome != "Dono" {
		t.Errorf("relido diferente do gravado: %+v (achou=%v), dono %+v", u, ok, de.Dono())
	}
}

// Alteração que falha não muda nada, nem no arquivo nem em memória.
func TestAlterarQueFalhaNaoMudaNada(t *testing.T) {
	c, _, _ := Abrir(filepath.Join(t.TempDir(), "usuarios.json"), "admin")
	err := c.Alterar(func(a *Arquivo) error {
		a.Usuarios = append(a.Usuarios, Usuario{Usuario: "maria", Papel: PapelComum})
		return errors.New("recusado")
	})
	if err == nil || c.Quantos() != 0 {
		t.Errorf("err=%v quantos=%d", err, c.Quantos())
	}
	if _, err := os.Stat(c.caminho); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("gravou o arquivo mesmo com a alteração recusada: %v", err)
	}
}

// Entrada inválida fica de fora da busca e vira aviso, mas continua no
// arquivo: a próxima gravação não a apaga.
func TestEntradaInvalidaFicaDeForaSemSumir(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	conteudo := `{"usuarios": [
		{"usuario": "maria", "nome": "Maria", "papel": "comum"},
		{"usuario": "admin", "nome": "Outro dono", "papel": "admin"},
		{"usuario": "Joao", "nome": "João", "papel": "comum"},
		{"usuario": "maria", "nome": "Outra Maria", "papel": "admin"},
		{"usuario": "ana", "nome": "Ana", "papel": "chefe"},
		{"usuario": "bia", "nome": "Bia", "papel": "admin"}
	]}`
	if err := os.WriteFile(caminho, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}

	c, avisos, err := Abrir(caminho, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(avisos) != 4 {
		t.Errorf("esperava 4 avisos, vieram %d: %v", len(avisos), avisos)
	}
	for _, quer := range []string{"#1", "#2", "#3", "#4"} {
		if !strings.Contains(strings.Join(avisos, "\n"), quer) {
			t.Errorf("nenhum aviso da entrada %s: %v", quer, avisos)
		}
	}

	if u, ok := c.Buscar("maria"); !ok || u.Nome != "Maria" {
		t.Errorf("no repetido vale a primeira: %+v %v", u, ok)
	}
	if u, ok := c.Buscar("bia"); !ok || u.Papel != PapelAdmin {
		t.Errorf("bia: %+v %v", u, ok)
	}
	for _, fora := range []string{"admin", "Joao", "ana"} {
		if _, ok := c.Buscar(fora); ok {
			t.Errorf("%q deveria ter ficado de fora", fora)
		}
	}

	if err := c.Alterar(func(*Arquivo) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if de, _, _ := Abrir(caminho, "admin"); de.Quantos() != 6 {
		t.Errorf("a gravação apagou entradas: sobraram %d de 6", de.Quantos())
	}
}

// Arquivo ilegível não impede o boot, mas o cadastro vem vazio e não grava
// por cima.
func TestArquivoIlegivelNaoEhSobrescrito(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	if err := os.WriteFile(caminho, []byte(`{"usuarios": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Abrir(caminho, "admin")
	if err == nil || c == nil || c.Quantos() != 0 {
		t.Fatalf("err=%v c=%v", err, c)
	}
	if err := c.Alterar(func(*Arquivo) error { return nil }); err == nil {
		t.Error("gravou por cima do arquivo ilegível")
	}
	if b, _ := os.ReadFile(caminho); string(b) != `{"usuarios": [` {
		t.Errorf("o arquivo mudou: %q", b)
	}
}

func TestValidarUsuario(t *testing.T) {
	for _, bom := range []string{"maria", "joao.silva", "ze_2", "a-b", strings.Repeat("a", 32)} {
		if err := ValidarUsuario(bom); err != nil {
			t.Errorf("%q recusado: %v", bom, err)
		}
	}
	for _, ruim := range []string{"", "Maria", "joão", "a b", "a/b", strings.Repeat("a", 33)} {
		if ValidarUsuario(ruim) == nil {
			t.Errorf("%q aceito", ruim)
		}
	}
}
