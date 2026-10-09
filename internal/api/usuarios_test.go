package api

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

// pedirComo faz a requisição pelo Handler, com o cookie de quem pede (ou sem
// cookie, com usuario vazio), e devolve o status e o corpo lido como JSON.
func pedirComo(t *testing.T, s *Server, usuario, metodo, rota string, corpo any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var leitor io.Reader
	if corpo != nil {
		b, _ := json.Marshal(corpo)
		leitor = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(metodo, rota, leitor)
	if usuario != "" {
		req.AddCookie(cookieDe(t, s, usuario))
	}
	if s.client == nil {
		s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"}) // o Handler monta o proxy do live com ele
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

// Do admin criar até a pessoa entrar com a senha que ela mesma definiu.
func TestConviteDaTelaAoLogin(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s)

	rec, resp := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios",
		map[string]string{"usuario": "maria", "nome": "Maria"})
	if rec.Code != http.StatusOK {
		t.Fatalf("criar: status %d, %s", rec.Code, rec.Body)
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatalf("criar não devolveu o token: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "tokenSha256") {
		t.Errorf("a resposta traz o hash do token: %s", rec.Body)
	}

	rec, resp = pedirComo(t, s, "", http.MethodPost, "/api/convite/conferir", map[string]string{"token": token})
	if rec.Code != http.StatusOK || resp["nome"] != "Maria" || resp["senhaMinima"] != float64(usuarios.SenhaMinima) {
		t.Errorf("conferir: status %d, %s", rec.Code, rec.Body)
	}

	rec, _ = pedirComo(t, s, "", http.MethodPost, "/api/convite", map[string]string{"token": token, "senha": "curta"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("senha curta: status %d, esperava 400", rec.Code)
	}

	rec, _ = pedirComo(t, s, "", http.MethodPost, "/api/convite", map[string]string{"token": token, "senha": "uma senha boa"})
	if rec.Code != http.StatusOK {
		t.Fatalf("definir a senha: status %d, %s", rec.Code, rec.Body)
	}
	c, err := http.ParseSetCookie(rec.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatalf("definir a senha não abriu a sessão: %v", err)
	}
	if p, _, ok := s.tokenPessoa(c.Value); !ok || p.Usuario != "maria" || p.Papel != usuarios.PapelComum {
		t.Errorf("sessão aberta para %+v (ok=%v)", p, ok)
	}

	// Uso único.
	rec, _ = pedirComo(t, s, "", http.MethodPost, "/api/convite", map[string]string{"token": token, "senha": "outra senha boa"})
	if rec.Code != http.StatusGone {
		t.Errorf("link usado de novo: status %d, esperava 410", rec.Code)
	}

	rec, resp = pedirComo(t, s, "admin", http.MethodGet, "/api/usuarios", nil)
	lista, _ := resp["usuarios"].([]any)
	if rec.Code != http.StatusOK || len(lista) != 1 || lista[0].(map[string]any)["situacao"] != "ativo" {
		t.Errorf("listar: status %d, %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "pbkdf2") {
		t.Errorf("a lista traz a senha guardada: %s", rec.Body)
	}
}

func TestCriarUsuarioRecusa(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s, maria)
	for _, corpo := range []map[string]string{
		{"usuario": "maria", "nome": "Outra"}, // repetido
		{"usuario": "admin", "nome": "Dono"},  // o dono
		{"usuario": "Zé", "nome": "Zé"},       // fora do formato
		{"usuario": "ze", "nome": ""},
	} {
		if rec, _ := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios", corpo); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d, esperava 400", corpo, rec.Code)
		}
	}
	if rec, _ := pedirComo(t, s, "admin", http.MethodDelete, "/api/usuarios?usuario=ninguem", nil); rec.Code != http.StatusNotFound {
		t.Errorf("remover quem não existe: status %d", rec.Code)
	}
}

// Sem autenticação não há cadastro: a tela não grava, e link nenhum vale.
func TestSemAutenticacaoNaoHaCadastro(t *testing.T) {
	s, _ := testServer(t)
	if rec, _ := pedirComo(t, s, "", http.MethodPost, "/api/usuarios",
		map[string]string{"usuario": "maria", "nome": "Maria"}); rec.Code != http.StatusConflict {
		t.Errorf("criar sem autenticação: status %d, esperava 409", rec.Code)
	}
	if rec, _ := pedirComo(t, s, "", http.MethodPost, "/api/convite/conferir",
		map[string]string{"token": "x"}); rec.Code != http.StatusGone {
		t.Errorf("conferir sem autenticação: status %d, esperava 410", rec.Code)
	}
	if rec, resp := pedirComo(t, s, "", http.MethodGet, "/api/usuarios", nil); rec.Code != http.StatusOK || resp["authRequired"] != false {
		t.Errorf("listar sem autenticação: status %d, %s", rec.Code, rec.Body)
	}
}

// apertoDeMao é o pedido de websocket, com o cookie da pessoa. A chave é a do
// exemplo da RFC 6455, montada a partir do nonce dela para o detector de
// segredos do commit não a tomar por uma credencial.
func apertoDeMao(rota string, c *http.Cookie) string {
	chave := base64.StdEncoding.EncodeToString([]byte("the sample nonce"))
	return "GET " + rota + " HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + chave + "\r\nCookie: " + c.String() + "\r\n\r\n"
}

// abrirAviso abre o aviso de sessão com o cookie da pessoa, num servidor de
// verdade: o httptest.ResponseRecorder não faz hijack.
func abrirAviso(t *testing.T, srv *httptest.Server, c *http.Cookie) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_, _ = io.WriteString(conn, apertoDeMao("/api/session/ws", c))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols ||
		resp.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" { // o exemplo da RFC 6455
		t.Fatalf("aperto de mão: %d, accept %q", resp.StatusCode, resp.Header.Get("Sec-WebSocket-Accept"))
	}
	return conn
}

// fecha diz se o servidor fechou a conexão dentro do prazo.
func fecha(conn net.Conn, prazo time.Duration) bool {
	_ = conn.SetReadDeadline(time.Now().Add(prazo))
	_, err := conn.Read(make([]byte, 1))
	return errors.Is(err, io.EOF)
}

// Gerar link novo ou remover derruba na hora o que a pessoa tem aberto, e só
// dela.
func TestDerrubaAsConexoesDaPessoa(t *testing.T) {
	s, _ := testServer(t)
	ze := maria
	ze.Usuario, ze.Nome = "ze", "Zé"
	comAutenticacao(t, s, maria, ze)
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	daMaria := abrirAviso(t, srv, cookieDe(t, s, "maria"))
	doZe := abrirAviso(t, srv, cookieDe(t, s, "ze"))
	if fecha(daMaria, 100*time.Millisecond) {
		t.Fatal("o aviso fechou sozinho")
	}

	if rec, _ := pedirComo(t, s, "admin", http.MethodPost, "/api/usuarios/link",
		map[string]string{"usuario": "maria"}); rec.Code != http.StatusOK {
		t.Fatalf("link novo: status %d, %s", rec.Code, rec.Body)
	}
	if !fecha(daMaria, 2*time.Second) {
		t.Error("o aviso da maria continuou aberto depois do link novo")
	}
	if fecha(doZe, 100*time.Millisecond) {
		t.Error("o aviso do zé caiu junto")
	}

	if rec, _ := pedirComo(t, s, "admin", http.MethodDelete, "/api/usuarios?usuario=ze", nil); rec.Code != http.StatusOK {
		t.Fatalf("remover: status %d", rec.Code)
	}
	if !fecha(doZe, 2*time.Second) {
		t.Error("o aviso do zé continuou aberto depois de removido")
	}
}

// O websocket do ao vivo também entra na conta: o proxy anota a conexão que o
// ReverseProxy toma.
func TestDerrubaOAoVivo(t *testing.T) {
	// Um go2rtc de mentira, que aceita o websocket e fica parado.
	falso := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		_, _ = io.Copy(io.Discard, rw)
	}))
	defer falso.Close()

	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: falso.URL})
	comAutenticacao(t, s, maria)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, apertoDeMao("/api/live/ws?src=cam_teste", cookieDe(t, s, "maria")))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("ao vivo: status %d", resp.StatusCode)
	}

	if rec, _ := pedirComo(t, s, "admin", http.MethodDelete, "/api/usuarios?usuario=maria", nil); rec.Code != http.StatusOK {
		t.Fatalf("remover: status %d", rec.Code)
	}
	if !fecha(conn, 2*time.Second) {
		t.Error("o ao vivo continuou aberto depois de a pessoa ser removida")
	}
}
