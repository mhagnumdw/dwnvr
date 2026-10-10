package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/usuarios"
)

// comAutenticacao liga a autenticação, com o dono "admin", e cadastra as
// pessoas dadas no usuarios.json.
func comAutenticacao(t *testing.T, s *Server, gente ...usuarios.Usuario) {
	t.Helper()
	s.cfg.Server.Username, s.cfg.Server.Password = "admin", "senha"
	// Reaberto com o dono, como no boot: é ele que o cadastro recusa criar.
	cad, _, err := usuarios.Abrir(s.cfg.UsuariosPath(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	s.usuarios = cad
	err = s.usuarios.Alterar(func(a *usuarios.Arquivo) error {
		a.Usuarios = append(a.Usuarios, gente...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Para os testes de sessão, a senha guardada não precisa ser um hash de
// verdade: a sessão só a usa para amarrar o cookie.
var maria = usuarios.Usuario{Usuario: "maria", Nome: "Maria", Papel: usuarios.PapelComum, Senha: "$guardada$da-maria"}

// cookieDe é o cookie que a pessoa recebe, valendo por mais uma hora.
func cookieDe(t *testing.T, s *Server, usuario string) *http.Cookie {
	t.Helper()
	p, ok := s.quem(usuario)
	if !ok {
		t.Fatalf("%q não existe", usuario)
	}
	return &http.Cookie{Name: sessionCookie, Value: s.signToken(p, time.Now().Add(time.Hour).Unix())}
}

func vale(s *Server, tok string) bool {
	_, _, ok := s.tokenPessoa(tok)
	return ok
}

func TestTokenDeSessao(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s, maria)
	dono, _ := s.quem("admin")

	valido := s.signToken(dono, time.Now().Add(time.Hour).Unix())
	if p, _, ok := s.tokenPessoa(valido); !ok || p.Usuario != "admin" || !p.Dono {
		t.Errorf("token recém-assinado: %+v, ok=%v", p, ok)
	}

	if vale(s, s.signToken(dono, time.Now().Add(-time.Hour).Unix())) {
		t.Error("token expirado foi aceito")
	}

	// Esticar o prazo ou trocar de pessoa tem que invalidar: o HMAC cobre o
	// login e o prazo.
	login, resto, _ := strings.Cut(valido, ".")
	_, sig, _ := strings.Cut(resto, ".")
	if vale(s, login+".99999999999."+sig) {
		t.Error("prazo esticado foi aceito")
	}
	if vale(s, strings.Replace(valido, login, "bWFyaWE", 1)) { // "maria"
		t.Error("o token do dono com o login da maria foi aceito")
	}

	// Assinatura de outro segredo não pode valer.
	outro := &Server{cfg: s.cfg, usuarios: s.usuarios, sessionSecret: []byte("outro-segredo-de-32-bytes!!!!!!!")}
	if vale(s, outro.signToken(dono, time.Now().Add(time.Hour).Unix())) {
		t.Error("token assinado com outro segredo foi aceito")
	}

	for _, ruim := range []string{"", "semponto", "abc.def", ".", "123.", "YWRtaW4.abc.def", "!!.123.abc", ".."} {
		if vale(s, ruim) {
			t.Errorf("token malformado %q foi aceito", ruim)
		}
	}
}

// O cookie de cada pessoa é amarrado à credencial dela: trocar a senha de uma,
// ou removê-la, derruba só ela.
func TestSessaoDerrubaSoAPessoa(t *testing.T) {
	joao := usuarios.Usuario{Usuario: "joao", Nome: "João", Papel: usuarios.PapelComum, Senha: "$guardada$do-joao"}
	alterar := func(s *Server, f func(*usuarios.Arquivo)) {
		t.Helper()
		if err := s.usuarios.Alterar(func(a *usuarios.Arquivo) error { f(a); return nil }); err != nil {
			t.Fatal(err)
		}
	}

	for _, caso := range []struct {
		nome    string
		mudar   func(s *Server)
		caiu    string
		ficaram []string
	}{
		{"senha nova da maria", func(s *Server) {
			alterar(s, func(a *usuarios.Arquivo) { a.Usuarios[0].Senha = "$guardada$nova" })
		}, "maria", []string{"admin", "joao"}},
		{"maria sem senha, à espera de um link novo", func(s *Server) {
			alterar(s, func(a *usuarios.Arquivo) { a.Usuarios[0].Senha = "" })
		}, "maria", []string{"admin", "joao"}},
		{"maria removida", func(s *Server) {
			alterar(s, func(a *usuarios.Arquivo) { a.Usuarios = a.Usuarios[1:] })
		}, "maria", []string{"admin", "joao"}},
		{"senha nova do dono", func(s *Server) { s.cfg.Server.Password += "-nova" }, "admin", []string{"maria", "joao"}},
	} {
		s, _ := testServer(t)
		comAutenticacao(t, s, maria, joao)
		cookies := map[string]string{}
		for _, u := range []string{"admin", "maria", "joao"} {
			cookies[u] = cookieDe(t, s, u).Value
		}

		caso.mudar(s)
		if vale(s, cookies[caso.caiu]) {
			t.Errorf("%s: o cookie de %s continuou valendo", caso.nome, caso.caiu)
		}
		for _, u := range caso.ficaram {
			if !vale(s, cookies[u]) {
				t.Errorf("%s: o cookie de %s caiu junto", caso.nome, u)
			}
		}
	}
}

// Trocar o usuário ou a senha do dono e reiniciar derruba as sessões dele,
// porque a chave que assina o cookie sai do .session-secret junto com a
// credencial. Reiniciar com a mesma credencial não derruba ninguém. Passa pelo
// New, que é o caminho do boot.
func TestTrocarACredencialDoDonoDerrubaAsSessoesDele(t *testing.T) {
	segredo := []byte("segredo-de-teste-com-32-bytes!!!")
	caminho := filepath.Join(t.TempDir(), "usuarios.json")
	subir := func(usuario, senha string) *Server {
		cfg := &config.Config{}
		cfg.Server.Username, cfg.Server.Password = usuario, senha
		cad, _, err := usuarios.Abrir(caminho, usuario)
		if err != nil {
			t.Fatal(err)
		}
		return New(cfg, nil, nil, nil, nil, cad, segredo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	antes := subir("admin", "senha")
	dono, _ := antes.quem("admin")
	cookie := antes.signToken(dono, time.Now().Add(time.Hour).Unix())

	for _, caso := range []struct {
		nome           string
		usuario, senha string
		vale           bool
	}{
		{"mesma credencial", "admin", "senha", true},
		{"senha nova", "admin", "senha-nova", false},
		{"usuário novo", "dono", "senha", false},
	} {
		if ok := vale(subir(caso.usuario, caso.senha), cookie); ok != caso.vale {
			t.Errorf("%s: o cookie de antes vale=%v, esperava %v", caso.nome, ok, caso.vale)
		}
	}
}

func TestRequireAuth(t *testing.T) {
	s, _ := testServer(t)
	var quem pessoa
	chamou := false
	h := s.requireAuth(func(w http.ResponseWriter, r *http.Request) { chamou, quem = true, pessoaDe(r) })

	// Sem credencial configurada a autenticação fica desligada, e quem chega
	// faz tudo.
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !chamou || !quem.admin() {
		t.Errorf("sem credenciais: chamou=%v, pessoa %+v", chamou, quem)
	}

	comAutenticacao(t, s, maria)
	chamou = false
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if chamou || rec.Code != http.StatusUnauthorized {
		t.Errorf("com credenciais, sem cookie: chamou=%v status=%d", chamou, rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(cookieDe(t, s, "maria"))
	h(httptest.NewRecorder(), req)
	if !chamou || quem.Usuario != "maria" || quem.Papel != usuarios.PapelComum {
		t.Errorf("cookie da maria: chamou=%v, pessoa %+v", chamou, quem)
	}
}

// Cada rota declara o papel que exige. O usuário comum é recusado com 403 em
// tudo que é da tela Câmeras e do diagnóstico do servidor, mesmo sem a aba na
// tela. Passa pelo Handler, que é onde o papel de cada rota é declarado.
func TestPapelPorRota(t *testing.T) {
	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	comAutenticacao(t, s, maria)
	h := s.Handler()

	pedir := func(usuario, metodo, rota string) int {
		req := httptest.NewRequest(metodo, rota, nil)
		req.AddCookie(cookieDe(t, s, usuario))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	soAdmin := []string{
		"POST /api/cameras", "DELETE /api/cameras?id=cam_teste", "GET /api/streams/probe?src=x",
		"POST /api/go2rtc/restart", "GET /api/health", "POST /api/reconnects/reset",
		"GET /api/health/servidor", "DELETE /api/rec?cam=cam_teste",
		"GET /api/usuarios", "POST /api/usuarios", "POST /api/usuarios/link",
		"DELETE /api/usuarios?usuario=maria", "DELETE /api/usuarios/avatar?usuario=maria",
	}
	for _, r := range soAdmin {
		metodo, rota, _ := strings.Cut(r, " ")
		if code := pedir("maria", metodo, rota); code != http.StatusForbidden {
			t.Errorf("comum em %s: status %d, esperava 403", r, code)
		}
	}
	if !s.knownCamera("cam_teste") {
		t.Error("a câmera sumiu com o pedido recusado")
	}

	// O que é de todos passa: aqui, só não pode ser 401 nem 403.
	deTodos := []string{
		"GET /api/cameras", "GET /api/rec/days?cam=cam_teste", "GET /api/deteccoes/dias",
		"GET /api/rec/timeline?cam=cam_teste&day=2026-10-09",
	}
	for _, r := range deTodos {
		metodo, rota, _ := strings.Cut(r, " ")
		if code := pedir("maria", metodo, rota); code == http.StatusForbidden || code == http.StatusUnauthorized {
			t.Errorf("comum em %s: status %d", r, code)
		}
	}
	if code := pedir("admin", http.MethodPost, "/api/reconnects/reset"); code != http.StatusOK {
		t.Errorf("admin em /api/reconnects/reset: status %d", code)
	}
}

// O usuário comum recebe só as câmeras e o detector: nada do go2rtc, das
// órfãs, do padrão, das faixas nem do caminho no disco.
func TestCamerasEnxutoParaOComum(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s, maria)
	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	req.AddCookie(cookieDe(t, s, "maria"))
	rec := httptest.NewRecorder()
	// Sem go2rtc no testServer: se o comum o consultasse, o teste cairia.
	s.requireAuth(s.handleCameras)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 2 || resp["cameras"] == nil || resp["detector"] == nil {
		t.Errorf("chaves inesperadas: %s", rec.Body.String())
	}
	var cams []map[string]any
	if err := json.Unmarshal(resp["cameras"], &cams); err != nil || len(cams) != 1 {
		t.Fatalf("câmeras: %s (%v)", resp["cameras"], err)
	}
	if cams[0]["id"] != "cam_teste" || cams[0]["dir"] != nil {
		t.Errorf("câmera do comum: %v", cams[0])
	}
}

// Um valor estragado no usuarios.json (editado à mão, de outra versão) não
// deixa ninguém entrar, e o log diz o problema sem copiar o valor guardado.
func TestLoginComSenhaGuardadaEstragada(t *testing.T) {
	s, _ := testServer(t)
	var log strings.Builder
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	estragada := maria
	estragada.Senha = "$pbkdf2-sha256$i=99999999999$c2FsdA$aGFzaA"
	comAutenticacao(t, s, estragada)

	tentativa := "qualquer uma"
	corpo, _ := json.Marshal(map[string]string{"username": "maria", "password": tentativa})
	rec := httptest.NewRecorder()
	s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(corpo))))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, esperava 401", rec.Code)
	}
	if !strings.Contains(log.String(), "senha guardada ilegível") {
		t.Errorf("o log não aponta o valor estragado: %s", log.String())
	}
	if strings.Contains(log.String(), "99999999999") {
		t.Errorf("o log copiou o valor guardado: %s", log.String())
	}
}

func TestLogin(t *testing.T) {
	s, _ := testServer(t)
	guardada, err := usuarios.GerarSenha(context.Background(), "senha da maria")
	if err != nil {
		t.Fatal(err)
	}
	m := maria
	m.Senha = guardada
	comAutenticacao(t, s, m, usuarios.Usuario{Usuario: "ze", Nome: "Zé", Papel: usuarios.PapelComum})

	entrar := func(usuario, senha string) (*httptest.ResponseRecorder, pessoa) {
		corpo, _ := json.Marshal(map[string]string{"username": usuario, "password": senha})
		rec := httptest.NewRecorder()
		s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(corpo))))
		var resp struct {
			Pessoa pessoa `json:"pessoa"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec, resp.Pessoa
	}

	for _, caso := range []struct {
		usuario, senha string
		papel          usuarios.Papel
	}{{"admin", "senha", usuarios.PapelAdmin}, {"maria", "senha da maria", usuarios.PapelComum}} {
		rec, p := entrar(caso.usuario, caso.senha)
		if rec.Code != http.StatusOK || p.Usuario != caso.usuario || p.Papel != caso.papel {
			t.Errorf("%s: status %d, pessoa %+v", caso.usuario, rec.Code, p)
			continue
		}
		c, err := http.ParseSetCookie(rec.Header().Get("Set-Cookie"))
		if err != nil {
			t.Fatal(err)
		}
		if p, _, ok := s.tokenPessoa(c.Value); !ok || p.Usuario != caso.usuario {
			t.Errorf("%s: o cookie do login não vale: %+v", caso.usuario, p)
		}
		if strings.Contains(rec.Body.String(), guardada) || strings.Contains(rec.Body.String(), `"senha"`) {
			t.Errorf("%s: a resposta do login traz a senha: %s", caso.usuario, rec.Body.String())
		}
	}

	for _, caso := range [][2]string{
		{"maria", "senha"},          // a senha do dono não serve para a maria
		{"admin", "senha da maria"}, // nem a da maria para o dono
		{"ninguem", "senha"},
		{"ze", ""}, // ainda não definiu a senha
		{"", ""},
	} {
		if rec, _ := entrar(caso[0], caso[1]); rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("%q com %q: status %d, Set-Cookie %q", caso[0], caso[1], rec.Code, rec.Header().Get("Set-Cookie"))
		}
	}
}

// O /api/session diz quem está logado, sem a credencial.
func TestSessionDizQuemEsta(t *testing.T) {
	s, _ := testServer(t)
	sessao := func(c *http.Cookie) (resp struct {
		AuthRequired  bool    `json:"authRequired"`
		Authenticated bool    `json:"authenticated"`
		Pessoa        *pessoa `json:"pessoa"`
	}, corpo string) {
		req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
		if c != nil {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		s.handleSession(rec, req)
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp, rec.Body.String()
	}

	if r, _ := sessao(nil); r.AuthRequired || !r.Authenticated || r.Pessoa == nil || !r.Pessoa.admin() {
		t.Errorf("sem autenticação: %+v", r)
	}

	comAutenticacao(t, s, maria)
	if r, _ := sessao(nil); !r.AuthRequired || r.Authenticated || r.Pessoa != nil {
		t.Errorf("sem cookie: %+v", r)
	}
	r, corpo := sessao(cookieDe(t, s, "maria"))
	if !r.Authenticated || r.Pessoa == nil || *r.Pessoa != (pessoa{Usuario: "maria", Nome: "Maria", Papel: usuarios.PapelComum}) {
		t.Errorf("maria: %s", corpo)
	}
	if strings.Contains(corpo, maria.Senha) {
		t.Errorf("a resposta traz a senha guardada: %s", corpo)
	}
	if r, _ := sessao(cookieDe(t, s, "admin")); r.Pessoa == nil || !r.Pessoa.Dono || r.Pessoa.Nome != "admin" {
		t.Errorf("dono: %+v", r.Pessoa)
	}
}

// A sessão em uso se renova: passada a metade do prazo, a resposta traz um
// cookie novo com o prazo inteiro, da mesma pessoa. Antes disso, nada de
// Set-Cookie, para não regravar o cookie a cada requisição. Vale nas rotas
// protegidas e no /api/session, que é o que a tela chama ao abrir.
func TestSessaoSeRenovaComOUso(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s, maria)
	p, _ := s.quem("maria")
	protegida := s.requireAuth(func(w http.ResponseWriter, r *http.Request) {})

	for _, rota := range []struct {
		nome string
		h    http.HandlerFunc
	}{{"requireAuth", protegida}, {"/api/session", s.handleSession}} {
		for _, caso := range []struct {
			falta  time.Duration
			renova bool
		}{
			{sessionTTL - time.Hour, false},
			{sessionRenew + time.Hour, false},
			{sessionRenew - time.Hour, true},
			{time.Hour, true},
		} {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set("X-Forwarded-Proto", "https")
			req.AddCookie(&http.Cookie{Name: sessionCookie,
				Value: s.signToken(p, time.Now().Add(caso.falta).Unix())})
			rec := httptest.NewRecorder()
			rota.h(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s, faltando %v: status %d", rota.nome, caso.falta, rec.Code)
			}
			setCookie := rec.Header().Get("Set-Cookie")
			if !caso.renova {
				if setCookie != "" {
					t.Errorf("%s, faltando %v: renovou cedo demais: %s", rota.nome, caso.falta, setCookie)
				}
				continue
			}
			c, err := http.ParseSetCookie(setCookie)
			if err != nil {
				t.Fatalf("%s, faltando %v: sem cookie novo (%v)", rota.nome, caso.falta, err)
			}
			de, expires, ok := s.tokenPessoa(c.Value)
			if !ok || de.Usuario != "maria" {
				t.Fatalf("%s, faltando %v: cookie novo inválido ou de outra pessoa (%+v)", rota.nome, caso.falta, de)
			}
			if falta := time.Until(time.Unix(expires, 0)); falta < sessionTTL-time.Minute {
				t.Errorf("%s: o cookie novo vale só %v, esperava o prazo inteiro", rota.nome, falta)
			}
			if !c.Secure || !c.HttpOnly || c.Path != "/" {
				t.Errorf("%s: cookie novo perdeu atributos: %+v", rota.nome, c)
			}
		}
	}

	// Sessão vencida não renova: o login continua sendo o único caminho.
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie,
		Value: s.signToken(p, time.Now().Add(-time.Hour).Unix())})
	rec := httptest.NewRecorder()
	protegida(rec, req)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("sessão vencida: status %d, Set-Cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}

// O Secure do cookie de sessão só vale atrás de um proxy TLS. Em HTTP puro, o
// navegador descartaria o cookie e o login nunca pegaria.
func TestCookieDeSessaoSoESecurePorHTTPS(t *testing.T) {
	s, _ := testServer(t)
	comAutenticacao(t, s)

	secure := func(h http.HandlerFunc, req *http.Request) bool {
		t.Helper()
		rec := httptest.NewRecorder()
		h(rec, req)
		c, err := http.ParseSetCookie(rec.Header().Get("Set-Cookie"))
		if err != nil {
			t.Fatal(err)
		}
		return c.Secure
	}

	for proto, quer := range map[string]bool{"": false, "http": false, "https": true, "HTTPS": true} {
		login := httptest.NewRequest(http.MethodPost, "/api/login",
			strings.NewReader(`{"username":"admin","password":"senha"}`))
		logout := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
		if proto != "" {
			login.Header.Set("X-Forwarded-Proto", proto)
			logout.Header.Set("X-Forwarded-Proto", proto)
		}
		if got := secure(s.handleLogin, login); got != quer {
			t.Errorf("login com X-Forwarded-Proto=%q: Secure=%v, esperava %v", proto, got, quer)
		}
		if got := secure(s.handleLogout, logout); got != quer {
			t.Errorf("logout com X-Forwarded-Proto=%q: Secure=%v, esperava %v", proto, got, quer)
		}
	}
}
