package usuarios

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"testing"
)

// comSenha cria a pessoa e define a senha dela pelo link.
func comSenha(t *testing.T, c *Cadastro, usuario, senha string) Usuario {
	t.Helper()
	conv, err := c.Criar(usuario, usuario)
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.DefinirSenha(context.Background(), conv.Token, senha)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTrocarSenha(t *testing.T) {
	c := cadastroVazio(t)
	antes := comSenha(t, c, "maria", "uma senha boa")
	ctx := context.Background()

	var recusado Recusado
	if _, err := c.TrocarSenha(ctx, "maria", "senha errada", "outra senha boa"); !errors.As(err, &recusado) {
		t.Errorf("senha atual errada: %v", err)
	}
	if _, err := c.TrocarSenha(ctx, "maria", "uma senha boa", "curta"); !errors.As(err, &recusado) {
		t.Errorf("senha nova curta: %v", err)
	}
	if _, err := c.TrocarSenha(ctx, "jose", "uma senha boa", "outra senha boa"); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("quem não existe: %v", err)
	}

	u, err := c.TrocarSenha(ctx, "maria", "uma senha boa", "outra senha boa")
	if err != nil {
		t.Fatal(err)
	}
	if u.Senha == antes.Senha {
		t.Error("a senha guardada não mudou")
	}
	if ok, _ := ConferirSenha(ctx, "outra senha boa", u.Senha); !ok {
		t.Error("a senha nova não confere")
	}

	// Sem senha (link novo em aberto) não há o que trocar.
	if _, err := c.NovoLink("maria"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TrocarSenha(ctx, "maria", "outra senha boa", "mais uma senha"); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("depois do link novo: %v", err)
	}
}

func TestMudarNome(t *testing.T) {
	c := cadastroVazio(t)
	if _, err := c.Criar("maria", "Maria"); err != nil {
		t.Fatal(err)
	}
	if nome, err := c.MudarNome("maria", "  Maria Clara "); err != nil || nome != "Maria Clara" {
		t.Errorf("mudar: %q, %v", nome, err)
	}
	if u, _ := c.Buscar("maria"); u.Nome != "Maria Clara" {
		t.Errorf("no cadastro: %q", u.Nome)
	}
	// O dono não está no Usuarios: o nome dele vai para a seção própria.
	if _, err := c.MudarNome("admin", "Dono da Casa"); err != nil || c.Dono().Nome != "Dono da Casa" {
		t.Errorf("dono: %q, %v", c.Dono().Nome, err)
	}
	var recusado Recusado
	if _, err := c.MudarNome("maria", "   "); !errors.As(err, &recusado) {
		t.Errorf("nome vazio: %v", err)
	}
	if _, err := c.MudarNome("jose", "José"); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("quem não existe: %v", err)
	}
}

// fotoDeTeste é um JPEG cinza de lado x altura.
func fotoDeTeste(t *testing.T, lado, altura int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, lado, altura)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestConferirAvatar(t *testing.T) {
	boa := fotoDeTeste(t, LadoDoAvatar, LadoDoAvatar)
	casos := map[string][]byte{
		"retangular":    fotoDeTeste(t, LadoDoAvatar, LadoDoAvatar-1),
		"grande demais": fotoDeTeste(t, LadoDoAvatar+1, LadoDoAvatar+1),
		"truncada":      boa[:len(boa)/2],
		"não é JPEG":    []byte("\x89PNG\r\n\x1a\n"),
		"passa do teto": append(append([]byte{}, boa...), make([]byte, TetoDoAvatar)...),
	}
	for nome, b := range casos {
		var recusado Recusado
		if err := ConferirAvatar(b); !errors.As(err, &recusado) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if err := ConferirAvatar(boa); err != nil {
		t.Errorf("a boa: %v", err)
	}
	if err := ConferirAvatar(fotoDeTeste(t, 64, 64)); err != nil {
		t.Errorf("menor que o lado: %v", err)
	}
}

func TestAvatar(t *testing.T) {
	c := cadastroVazio(t)
	if _, err := c.Criar("maria", "Maria"); err != nil {
		t.Fatal(err)
	}
	foto := fotoDeTeste(t, LadoDoAvatar, LadoDoAvatar)

	id1, err := c.TrocarAvatar("maria", foto)
	if err != nil {
		t.Fatal(err)
	}
	caminho, dona, err := c.Avatar(id1)
	if err != nil || dona != "maria" {
		t.Fatalf("achar a foto: %q, %v", dona, err)
	}
	if b, _ := os.ReadFile(caminho); !bytes.Equal(b, foto) {
		t.Error("a foto guardada não é a enviada")
	}

	// Trocar apaga a anterior, e a URL antiga deixa de servir.
	id2, err := c.TrocarAvatar("maria", foto)
	if err != nil || id2 == id1 {
		t.Fatalf("trocar: %q, %v", id2, err)
	}
	if _, err := os.Stat(caminho); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a foto anterior ficou no disco: %v", err)
	}
	if _, _, err := c.Avatar(id1); !errors.Is(err, ErrSemAvatar) {
		t.Errorf("o id antigo ainda acha foto: %v", err)
	}

	// O dono também tem.
	idDono, err := c.TrocarAvatar("admin", foto)
	if err != nil {
		t.Fatal(err)
	}
	if _, dono, _ := c.Avatar(idDono); dono != "admin" || c.Dono().Avatar != idDono {
		t.Errorf("foto do dono: %q", dono)
	}

	if _, err := c.TrocarAvatar("jose", foto); !errors.Is(err, ErrNaoExiste) {
		t.Errorf("quem não existe: %v", err)
	}
	if _, _, err := c.Avatar("../usuarios"); !errors.Is(err, ErrSemAvatar) {
		t.Errorf("id fora do formato: %v", err)
	}

	caminho2, _, _ := c.Avatar(id2)
	if err := c.TirarAvatar("maria"); err != nil {
		t.Fatal(err)
	}
	if u, _ := c.Buscar("maria"); u.Avatar != "" {
		t.Errorf("o id ficou no cadastro: %q", u.Avatar)
	}
	if _, err := os.Stat(caminho2); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a foto tirada ficou no disco: %v", err)
	}
}

func TestRemoverApagaOAvatar(t *testing.T) {
	c := cadastroVazio(t)
	if _, err := c.Criar("maria", "Maria"); err != nil {
		t.Fatal(err)
	}
	id, err := c.TrocarAvatar("maria", fotoDeTeste(t, 32, 32))
	if err != nil {
		t.Fatal(err)
	}
	caminho, _, _ := c.Avatar(id)
	if err := c.Remover("maria"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(caminho); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a foto de quem saiu ficou no disco: %v", err)
	}
}
