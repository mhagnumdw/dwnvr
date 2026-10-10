package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

// A troca da própria senha: pede a atual, derruba as outras sessões e deixa a
// de quem trocou de pé, com um cookie novo.
func TestTrocarASenhaPelaConta(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s)
	conv, err := s.usuarios.Criar("maria", "Maria")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.usuarios.DefinirSenha(context.Background(), conv.Token, "uma senha boa"); err != nil {
		t.Fatal(err)
	}
	outroAparelho := cookieDe(t, s, "maria")

	rec, _ := pedirComo(t, s, "maria", http.MethodPost, "/api/conta/senha",
		map[string]string{"atual": "senha errada", "nova": "outra senha boa"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("senha atual errada: status %d, esperava 400", rec.Code)
	}
	if !vale(s, outroAparelho.Value) {
		t.Error("a tentativa errada derrubou a sessão")
	}

	rec, resp := pedirComo(t, s, "maria", http.MethodPost, "/api/conta/senha",
		map[string]string{"atual": "uma senha boa", "nova": "outra senha boa"})
	if rec.Code != http.StatusOK {
		t.Fatalf("trocar: status %d, %s", rec.Code, rec.Body)
	}
	// O último cookie é o que vale: o cookie de teste vence em uma hora, e o
	// requireAuth o renova antes da troca.
	cs := rec.Result().Cookies()
	if len(cs) == 0 || !vale(s, cs[len(cs)-1].Value) {
		t.Errorf("quem trocou ficou sem sessão: %v", cs)
	}
	if vale(s, outroAparelho.Value) {
		t.Error("o outro aparelho continua entrando com a senha velha")
	}
	if p, _ := resp["pessoa"].(map[string]any); p["usuario"] != "maria" {
		t.Errorf("pessoa na resposta: %s", rec.Body)
	}

	// O dono troca a senha no dwnvr.yaml.
	if rec, _ := pedirComo(t, s, "admin", http.MethodPost, "/api/conta/senha",
		map[string]string{"atual": "senha", "nova": "outra senha boa"}); rec.Code != http.StatusConflict {
		t.Errorf("dono: status %d, esperava 409", rec.Code)
	}
}

// fotoDeTeste é um JPEG cinza quadrado.
func fotoDeTeste(t *testing.T, lado int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, lado, lado)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// enviarFoto manda o JPEG cru, como a tela manda.
func enviarFoto(t *testing.T, s *Server, usuario string, foto []byte) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/conta/avatar", bytes.NewReader(foto))
	req.Header.Set("Content-Type", "image/jpeg")
	req.AddCookie(cookieDe(t, s, usuario))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

func TestNomeEAvatarPelaConta(t *testing.T) {
	jose := usuarios.Usuario{Usuario: "jose", Nome: "José", Papel: usuarios.PapelComum, Senha: "$guardada$do-jose"}
	s, _ := testServer(t)
	comAutenticacao(t, s, maria, jose)

	rec, resp := pedirComo(t, s, "maria", http.MethodGet, "/api/conta", nil)
	if rec.Code != http.StatusOK || resp["senhaMinima"] != float64(usuarios.SenhaMinima) {
		t.Errorf("conta: status %d, %s", rec.Code, rec.Body)
	}

	rec, resp = pedirComo(t, s, "maria", http.MethodPost, "/api/conta/nome", map[string]string{"nome": "Maria Clara"})
	if p, _ := resp["pessoa"].(map[string]any); rec.Code != http.StatusOK || p["nome"] != "Maria Clara" {
		t.Errorf("nome: status %d, %s", rec.Code, rec.Body)
	}
	if rec, _ := pedirComo(t, s, "admin", http.MethodPost, "/api/conta/nome", map[string]string{"nome": "Dono"}); rec.Code != http.StatusOK {
		t.Errorf("nome do dono: status %d, %s", rec.Code, rec.Body)
	}
	if p, _ := s.quem("admin"); p.Nome != "Dono" {
		t.Errorf("o dono continua %q", p.Nome)
	}

	rec, resp = enviarFoto(t, s, "maria", fotoDeTeste(t, usuarios.LadoDoAvatar))
	p, _ := resp["pessoa"].(map[string]any)
	id, _ := p["avatar"].(string)
	if rec.Code != http.StatusOK || id == "" {
		t.Fatalf("foto: status %d, %s", rec.Code, rec.Body)
	}
	if rec, _ := enviarFoto(t, s, "maria", make([]byte, usuarios.TetoDoAvatar+1)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("foto acima do teto: status %d, esperava 413", rec.Code)
	}
	if rec, _ := enviarFoto(t, s, "maria", []byte("não é foto")); rec.Code != http.StatusBadRequest {
		t.Errorf("foto inválida: status %d, esperava 400", rec.Code)
	}

	// A própria pessoa e o admin veem a foto; os outros, não.
	for usuario, quer := range map[string]int{"maria": http.StatusOK, "admin": http.StatusOK, "jose": http.StatusNotFound} {
		rec, _ := pedirComo(t, s, usuario, http.MethodGet, "/api/avatar?id="+id, nil)
		if rec.Code != quer {
			t.Errorf("%s vendo a foto da maria: status %d, esperava %d", usuario, rec.Code, quer)
		}
		if quer == http.StatusOK && (rec.Header().Get("Content-Type") != "image/jpeg" ||
			rec.Header().Get("X-Content-Type-Options") != "nosniff") {
			t.Errorf("%s: headers %v", usuario, rec.Header())
		}
	}

	// O admin tira a foto dos outros; o comum não.
	if rec, _ := pedirComo(t, s, "jose", http.MethodDelete, "/api/usuarios/avatar?usuario=maria", nil); rec.Code != http.StatusForbidden {
		t.Errorf("comum tirando a foto de outro: status %d", rec.Code)
	}
	if rec, _ := pedirComo(t, s, "admin", http.MethodDelete, "/api/usuarios/avatar?usuario=maria", nil); rec.Code != http.StatusOK {
		t.Errorf("admin tirando a foto: status %d, %s", rec.Code, rec.Body)
	}
	if rec, _ := pedirComo(t, s, "maria", http.MethodGet, "/api/avatar?id="+id, nil); rec.Code != http.StatusNotFound {
		t.Errorf("foto tirada ainda é servida: status %d", rec.Code)
	}

	if rec, _ := enviarFoto(t, s, "jose", fotoDeTeste(t, 64)); rec.Code != http.StatusOK {
		t.Fatalf("foto do josé: status %d", rec.Code)
	}
	rec, resp = pedirComo(t, s, "jose", http.MethodDelete, "/api/conta/avatar", nil)
	if p, _ := resp["pessoa"].(map[string]any); rec.Code != http.StatusOK || p["avatar"] != nil {
		t.Errorf("tirar a própria foto: status %d, %s", rec.Code, rec.Body)
	}
}

// Sem autenticação não há de quem ser a conta nem cadastro: quem alcança a
// tela é admin, e nada que grave no usuarios.json ou em avatares/ passa.
func TestSemAutenticacaoNadaSeGrava(t *testing.T) {
	s, _ := testServer(t)
	rotas := []string{
		"GET /api/conta", "POST /api/conta/senha", "POST /api/conta/nome",
		"PUT /api/conta/avatar", "DELETE /api/conta/avatar",
		"POST /api/usuarios/link", "DELETE /api/usuarios?usuario=maria",
		"DELETE /api/usuarios/avatar?usuario=maria",
	}
	for _, r := range rotas {
		metodo, rota, _ := strings.Cut(r, " ")
		if rec, _ := pedirComo(t, s, "", metodo, rota, map[string]string{}); rec.Code != http.StatusConflict {
			t.Errorf("%s sem autenticação: status %d, esperava 409", r, rec.Code)
		}
	}
	if _, err := os.Stat(s.cfg.UsuariosPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("o usuarios.json foi gravado sem autenticação: %v", err)
	}
}
