package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/ratelimit"
	"github.com/mhagnumdw/dwnvr/internal/users"
)

// withAuth turns authentication on, with the owner "admin", and registers the
// given people in usuarios.json.
func withAuth(t *testing.T, s *Server, people ...users.User) {
	t.Helper()
	s.cfg.Server.Username, s.cfg.Server.Password = "admin", "senha"
	// Reopened with the owner, as at boot: it is the owner that the registry
	// refuses to create.
	cad, _, err := users.Open(s.cfg.UsersPath(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	s.users = cad
	err = s.users.Update(func(a *users.FileData) error {
		a.Users = append(a.Users, people...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// For the session tests, the stored password does not need to be a real hash:
// the session only uses it to bind the cookie.
var maria = users.User{Username: "maria", Name: "Maria", Role: users.RoleViewer, PasswordHash: "$guardada$da-maria"}

// cookieDe is the cookie the person receives, valid for one more hour.
func cookieOf(t *testing.T, s *Server, username string) *http.Cookie {
	t.Helper()
	p, ok := s.who(username)
	if !ok {
		t.Fatalf("%q does not exist", username)
	}
	return &http.Cookie{Name: sessionCookie, Value: s.signToken(p, time.Now().Add(time.Hour).Unix())}
}

func accepts(s *Server, tok string) bool {
	_, _, ok := s.tokenPerson(tok)
	return ok
}

func TestSessionToken(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)
	owner, _ := s.who("admin")

	valid := s.signToken(owner, time.Now().Add(time.Hour).Unix())
	if p, _, ok := s.tokenPerson(valid); !ok || p.Username != "admin" || !p.Owner {
		t.Errorf("freshly signed token: %+v, ok=%v", p, ok)
	}

	if accepts(s, s.signToken(owner, time.Now().Add(-time.Hour).Unix())) {
		t.Error("expired token was accepted")
	}

	// Extending the deadline or switching the person must invalidate it: the
	// HMAC covers the login and the deadline.
	login, rest, _ := strings.Cut(valid, ".")
	_, sig, _ := strings.Cut(rest, ".")
	if accepts(s, login+".99999999999."+sig) {
		t.Error("extended deadline was accepted")
	}
	if accepts(s, strings.Replace(valid, login, "bWFyaWE", 1)) { // "maria"
		t.Error("the owner's token with maria's login was accepted")
	}

	// A signature from another secret must not be valid.
	other := &Server{cfg: s.cfg, users: s.users, sessionSecret: []byte("outro-segredo-de-32-bytes!!!!!!!")}
	if accepts(s, other.signToken(owner, time.Now().Add(time.Hour).Unix())) {
		t.Error("token signed with another secret was accepted")
	}

	for _, bad := range []string{"", "semponto", "abc.def", ".", "123.", "YWRtaW4.abc.def", "!!.123.abc", ".."} {
		if accepts(s, bad) {
			t.Errorf("malformed token %q was accepted", bad)
		}
	}
}

// Each person's cookie is bound to their credential: changing one person's
// password, or removing them, drops only that person.
func TestSessionDropsOnlyThePerson(t *testing.T) {
	joao := users.User{Username: "joao", Name: "João", Role: users.RoleViewer, PasswordHash: "$guardada$do-joao"}
	change := func(s *Server, f func(*users.FileData)) {
		t.Helper()
		if err := s.users.Update(func(a *users.FileData) error { f(a); return nil }); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		name      string
		change    func(s *Server)
		dropped   string
		remaining []string
	}{
		{"maria's new password", func(s *Server) {
			change(s, func(a *users.FileData) { a.Users[0].PasswordHash = "$guardada$nova" })
		}, "maria", []string{"admin", "joao"}},
		{"maria without a password, waiting for a new link", func(s *Server) {
			change(s, func(a *users.FileData) { a.Users[0].PasswordHash = "" })
		}, "maria", []string{"admin", "joao"}},
		{"maria removed", func(s *Server) {
			change(s, func(a *users.FileData) { a.Users = a.Users[1:] })
		}, "maria", []string{"admin", "joao"}},
		{"owner's new password", func(s *Server) { s.cfg.Server.Password += "-nova" }, "admin", []string{"maria", "joao"}},
	} {
		s, _ := testServer(t)
		withAuth(t, s, maria, joao)
		cookies := map[string]string{}
		for _, u := range []string{"admin", "maria", "joao"} {
			cookies[u] = cookieOf(t, s, u).Value
		}

		tc.change(s)
		if accepts(s, cookies[tc.dropped]) {
			t.Errorf("%s: %s's cookie stayed valid", tc.name, tc.dropped)
		}
		for _, u := range tc.remaining {
			if !accepts(s, cookies[u]) {
				t.Errorf("%s: %s's cookie dropped too", tc.name, u)
			}
		}
	}
}

// Changing the owner's username or password and restarting drops their
// sessions, because the key that signs the cookie comes out of .session-secret
// together with the credential. Restarting with the same credential drops no
// one. Goes through New, which is the boot path.
func TestChangingOwnerCredentialDropsTheirSessions(t *testing.T) {
	secret := []byte("segredo-de-teste-com-32-bytes!!!")
	filePath := filepath.Join(t.TempDir(), "usuarios.json")
	start := func(username, password string) *Server {
		cfg := &config.Config{}
		cfg.Server.Username, cfg.Server.Password = username, password
		cad, _, err := users.Open(filePath, username)
		if err != nil {
			t.Fatal(err)
		}
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		accesses, err := users.OpenAccessLog(filepath.Join(filepath.Dir(filePath), "acessos.json"), log)
		if err != nil {
			t.Fatal(err)
		}
		return New(cfg, nil, nil, nil, nil, cad, accesses, nil, secret, log)
	}
	before := start("admin", "senha")
	owner, _ := before.who("admin")
	cookie := before.signToken(owner, time.Now().Add(time.Hour).Unix())

	for _, tc := range []struct {
		name               string
		username, password string
		accepts            bool
	}{
		{"same credential", "admin", "senha", true},
		{"new password", "admin", "senha-nova", false},
		{"new username", "dono", "senha", false},
	} {
		if ok := accepts(start(tc.username, tc.password), cookie); ok != tc.accepts {
			t.Errorf("%s: the old cookie valid=%v, expected %v", tc.name, ok, tc.accepts)
		}
	}
}

func TestRequireAuth(t *testing.T) {
	s, _ := testServer(t)
	var who person
	called := false
	h := s.requireAuth(func(w http.ResponseWriter, r *http.Request) { called, who = true, personOf(r) })

	// With no credential configured, authentication is off, and whoever arrives
	// can do everything.
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !called || !who.admin() {
		t.Errorf("no credentials: called=%v, person %+v", called, who)
	}

	withAuth(t, s, maria)
	called = false
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if called || rec.Code != http.StatusUnauthorized {
		t.Errorf("with credentials, no cookie: called=%v status=%d", called, rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(cookieOf(t, s, "maria"))
	h(httptest.NewRecorder(), req)
	if !called || who.Username != "maria" || who.Role != users.RoleViewer {
		t.Errorf("maria's cookie: called=%v, person %+v", called, who)
	}
}

// Each route declares the role it requires. The viewer is rejected with 403 on
// everything that belongs to the Cameras screen and to the server diagnostics,
// even without the tab on the screen. Goes through Handler, which is where each
// route's role is declared.
func TestRolePerRoute(t *testing.T) {
	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	withAuth(t, s, maria)
	h := s.Handler()

	ask := func(username, method, route string) int {
		req := httptest.NewRequest(method, route, nil)
		req.AddCookie(cookieOf(t, s, username))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	soAdmin := []string{
		"POST /api/cameras", "DELETE /api/cameras?id=cam_teste", "GET /api/streams/probe?src=x",
		"POST /api/go2rtc/restart", "GET /api/health", "POST /api/reconnects/reset",
		"GET /api/health/servidor", "DELETE /api/rec?cam=cam_teste",
		"GET /api/usuarios", "POST /api/usuarios", "POST /api/usuarios/link",
		"DELETE /api/usuarios?username=maria", "DELETE /api/usuarios/avatar?username=maria",
	}
	for _, r := range soAdmin {
		method, route, _ := strings.Cut(r, " ")
		if code := ask("maria", method, route); code != http.StatusForbidden {
			t.Errorf("viewer on %s: status %d, expected 403", r, code)
		}
	}
	if !s.knownCamera("cam_teste") {
		t.Error("the camera disappeared with the request rejected")
	}

	// What belongs to everyone passes: here, it just must not be 401 or 403.
	ofAll := []string{
		"GET /api/cameras", "GET /api/rec/days?cam=cam_teste", "GET /api/deteccoes/dias",
		"GET /api/rec/timeline?cam=cam_teste&day=2026-10-09",
	}
	for _, r := range ofAll {
		method, route, _ := strings.Cut(r, " ")
		if code := ask("maria", method, route); code == http.StatusForbidden || code == http.StatusUnauthorized {
			t.Errorf("viewer on %s: status %d", r, code)
		}
	}
	if code := ask("admin", http.MethodPost, "/api/reconnects/reset"); code != http.StatusOK {
		t.Errorf("admin on /api/reconnects/reset: status %d", code)
	}
}

// The viewer gets only the cameras and the detector: nothing from go2rtc, the
// orphans, the padrao, the faixas or the path on disk, and from the alert only
// whether it is on, without the families or the schedules. A camera with the
// alert off goes without it.
func TestCamerasLeanForTheViewer(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person},
			Schedules: []config.AlertSchedule{{Days: []time.Weekday{time.Monday}, From: "22:00", To: "23:59"}}}})
	s.mgr.Set(config.Camera{ID: "cam_b", Name: "B", Enabled: true,
		Alert: &config.CameraAlert{Families: []detect.Family{detect.Person}}})
	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	req.AddCookie(cookieOf(t, s, "maria"))
	rec := httptest.NewRecorder()
	// No go2rtc in testServer: if the viewer queried it, the test would fail.
	s.requireAuth(s.handleCameras)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 2 || resp["cameras"] == nil || resp["detector"] == nil {
		t.Errorf("unexpected keys: %s", rec.Body.String())
	}
	var cams []map[string]any
	if err := json.Unmarshal(resp["cameras"], &cams); err != nil || len(cams) != 2 {
		t.Fatalf("cameras: %s (%v)", resp["cameras"], err)
	}
	for _, c := range cams {
		alert, _ := json.Marshal(c["alert"])
		want := map[any]string{"cam_teste": `{"enabled":true}`, "cam_b": "null"}[c["id"]]
		if c["dir"] != nil || string(alert) != want {
			t.Errorf("viewer's camera: %v", c)
		}
	}
}

// A corrupted value in usuarios.json (edited by hand, from another version)
// lets no one in, and the log states the problem without copying the stored
// value.
func TestLoginWithCorruptedStoredPassword(t *testing.T) {
	s, _ := testServer(t)
	var log strings.Builder
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	corrupted := maria
	corrupted.PasswordHash = "$pbkdf2-sha256$i=99999999999$c2FsdA$aGFzaA"
	withAuth(t, s, corrupted)

	attempt := "qualquer uma"
	body, _ := json.Marshal(map[string]string{"username": "maria", "password": attempt})
	rec := httptest.NewRecorder()
	s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body))))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, expected 401", rec.Code)
	}
	if !strings.Contains(log.String(), "unreadable stored password") {
		t.Errorf("the log does not point to the corrupted value: %s", log.String())
	}
	if strings.Contains(log.String(), "99999999999") {
		t.Errorf("the log copied the stored value: %s", log.String())
	}
}

func TestLogin(t *testing.T) {
	s, _ := testServer(t)
	stored, err := users.HashPassword(context.Background(), "senha da maria")
	if err != nil {
		t.Fatal(err)
	}
	m := maria
	m.PasswordHash = stored
	withAuth(t, s, m, users.User{Username: "ze", Name: "Zé", Role: users.RoleViewer})

	doLogin := func(username, password string) (*httptest.ResponseRecorder, person) {
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		rec := httptest.NewRecorder()
		s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body))))
		var resp struct {
			Person person `json:"user"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec, resp.Person
	}

	for _, tc := range []struct {
		username, password string
		role               users.Role
	}{{"admin", "senha", users.RoleAdmin}, {"maria", "senha da maria", users.RoleViewer}} {
		rec, p := doLogin(tc.username, tc.password)
		if rec.Code != http.StatusOK || p.Username != tc.username || p.Role != tc.role {
			t.Errorf("%s: status %d, person %+v", tc.username, rec.Code, p)
			continue
		}
		c, err := http.ParseSetCookie(rec.Header().Get("Set-Cookie"))
		if err != nil {
			t.Fatal(err)
		}
		if p, _, ok := s.tokenPerson(c.Value); !ok || p.Username != tc.username {
			t.Errorf("%s: the login cookie is not valid: %+v", tc.username, p)
		}
		if strings.Contains(rec.Body.String(), stored) || strings.Contains(rec.Body.String(), `"passwordHash"`) {
			t.Errorf("%s: the login response carries the password: %s", tc.username, rec.Body.String())
		}
	}

	for _, tc := range [][2]string{
		{"maria", "senha"},          // the owner's password does not work for maria
		{"admin", "senha da maria"}, // nor maria's for the owner
		{"ninguem", "senha"},
		{"ze", ""}, // has not set the password yet
		{"", ""},
	} {
		if rec, _ := doLogin(tc[0], tc[1]); rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("%q with %q: status %d, Set-Cookie %q", tc[0], tc[1], rec.Code, rec.Header().Get("Set-Cookie"))
		}
	}
}

// Signing in by e-mail is signing in as the person: the cookie and the log keep
// the username. An e-mail that matches no one, including one repeated in the
// file, gets the same answer as a wrong password.
func TestLoginByEmail(t *testing.T) {
	s, _ := testServer(t)
	var log strings.Builder
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	stored, err := users.HashPassword(context.Background(), "senha da maria")
	if err != nil {
		t.Fatal(err)
	}
	m := maria
	m.PasswordHash, m.Email = stored, "Maria@Exemplo.com"
	// The repeated one only gets into a hand-edited file; the Update here does
	// not check it.
	ana := users.User{Username: "ana", Name: "Ana", Role: users.RoleViewer, PasswordHash: stored, Email: "casa@exemplo.com"}
	bia := ana
	bia.Username, bia.Name = "bia", "Bia"
	withAuth(t, s, m, ana, bia,
		users.User{Username: "ze", Name: "Zé", Role: users.RoleViewer, Email: "ze@exemplo.com"})

	doLogin := func(login, password string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": login, "password": password})
		rec := httptest.NewRecorder()
		s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body))))
		return rec
	}

	for _, login := range []string{"maria@exemplo.com", " MARIA@exemplo.com "} {
		rec := doLogin(login, "senha da maria")
		c, err := http.ParseSetCookie(rec.Header().Get("Set-Cookie"))
		if rec.Code != http.StatusOK || err != nil {
			t.Errorf("%q: status %d, %v", login, rec.Code, err)
			continue
		}
		if p, _, ok := s.tokenPerson(c.Value); !ok || p.Username != "maria" {
			t.Errorf("%q: cookie of %+v", login, p)
		}
	}
	if !strings.Contains(log.String(), `msg="login accepted" user=maria via_email=true`) {
		t.Errorf("the log of the login by e-mail: %s", log.String())
	}

	// The owner's username may have "@" in dwnvr.yaml, and they still get in,
	// even if someone in the registry has the same text as e-mail.
	s.cfg.Server.Username = "admin@casa"
	if err := s.users.Update(func(a *users.FileData) error {
		a.Users[0].Email = "Admin@Casa"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec := doLogin("admin@casa", "senha"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"owner":true`) {
		t.Errorf("owner with @ in the username: status %d, %s", rec.Code, rec.Body.String())
	}

	wrongPassword := doLogin("maria", "outra").Body.String()
	for _, tc := range [][2]string{
		{"maria@exemplo.com", "outra"},
		{"ninguem@exemplo.com", "senha da maria"},
		{"casa@exemplo.com", "senha da maria"}, // whose? ana or bia
		{"ze@exemplo.com", ""},                 // has not set the password yet
		{"@", ""},
	} {
		rec := doLogin(tc[0], tc[1])
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" || rec.Body.String() != wrongPassword {
			t.Errorf("%q with %q: status %d, Set-Cookie %q, body %s", tc[0], tc[1], rec.Code,
				rec.Header().Get("Set-Cookie"), rec.Body.String())
		}
	}
}

// A usuarios.json with a problem becomes a warning in /api/health, for the
// Warnings card and the bell; with no problem, the field does not even come.
func TestRegistryWithProblemBecomesWarning(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)
	_, resp := requestAs(t, s, "admin", http.MethodGet, "/api/health", nil)
	if _, has := resp["users"]; has {
		t.Errorf("users in health with no problem at all: %v", resp["users"])
	}

	if err := os.WriteFile(s.cfg.UsersPath(), []byte(`{"owner":{},"users":[
		{"username":"maria","name":"Maria","role":"viewer","email":"casa@exemplo.com"},
		{"username":"ana","name":"Ana","role":"viewer","email":"casa@exemplo.com"}
	]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cad, _, err := users.Open(s.cfg.UsersPath(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	s.users = cad
	_, resp = requestAs(t, s, "admin", http.MethodGet, "/api/health", nil)
	usersInfo, _ := resp["users"].(map[string]any)
	warnings, _ := usersInfo["warnings"].([]any)
	if len(warnings) != 1 || !strings.Contains(warnings[0].(string), `o mesmo de "maria"`) || usersInfo["error"] != nil {
		t.Errorf("users %v", resp["users"])
	}

	if err := os.WriteFile(s.cfg.UsersPath(), []byte(`{"users":`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.users, _, _ = users.Open(s.cfg.UsersPath(), "admin")
	_, resp = requestAs(t, s, "admin", http.MethodGet, "/api/health", nil)
	usersInfo, _ = resp["users"].(map[string]any)
	if errText, _ := usersInfo["error"].(string); !strings.Contains(errText, "unreadable") {
		t.Errorf("unreadable file: users %v", resp["users"])
	}
}

// The accepted login and the logout go to the log with who and from where,
// including the proxy's X-Forwarded-For. A logout without a valid session does
// not go to the log.
func TestLoginAndLogoutGoToTheLog(t *testing.T) {
	s, _ := testServer(t)
	var log strings.Builder
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	withAuth(t, s)

	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "senha"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body)))
	req.Header.Set("X-Forwarded-For", "100.64.0.7")
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status %d", rec.Code)
	}
	if l := log.String(); !strings.Contains(l, `msg="login accepted" user=admin from=192.0.2.1:1234 forwarded=100.64.0.7`) {
		t.Errorf("login log: %s", l)
	}
	c, err := http.ParseSetCookie(rec.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatal(err)
	}

	log.Reset()
	logout := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	logout.AddCookie(c)
	s.handleLogout(httptest.NewRecorder(), logout)
	if l := log.String(); !strings.Contains(l, `msg=logout user=admin from=192.0.2.1:1234`) || strings.Contains(l, "forwarded") {
		t.Errorf("logout log: %s", l)
	}

	log.Reset()
	s.handleLogout(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/logout", nil))
	if log.Len() != 0 {
		t.Errorf("logout without a session went to the log: %s", log.String())
	}
}

// X-Forwarded-For goes into the log cut at the cap, so that a made-up header
// does not fill the Diagnostics screen memory.
func TestForwardedCutsAtTheCap(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if a := forwarded(req); !a.Equal(slog.Attr{}) {
		t.Errorf("without the header: %v, expected empty", a)
	}
	req.Header.Add("X-Forwarded-For", "203.0.113.5")
	req.Header.Add("X-Forwarded-For", "100.64.0.7")
	if v := forwarded(req).Value.String(); v != "203.0.113.5, 100.64.0.7" {
		t.Errorf("two headers: %q", v)
	}
	req.Header.Set("X-Forwarded-For", strings.Repeat("1", forwardedMax+50))
	if v := forwarded(req).Value.String(); v != strings.Repeat("1", forwardedMax)+"…" {
		t.Errorf("header longo: %d bytes", len(v))
	}
}

// Past the burst, login answers 429 with Retry-After, without checking the
// password, not even the right one. The episode becomes a single log line, and
// the 429 does not go into the rejected request log. Another address, behind
// the same proxy, still gets in.
func TestLoginOverTheLimit(t *testing.T) {
	s, _ := testServer(t)
	var log strings.Builder
	s.log = slog.New(slog.NewTextHandler(&log, nil))
	withAuth(t, s)
	s.loginLimit, s.proxies = loginLimiter(s.cfg.Server)
	h := logRequests(s.log, http.HandlerFunc(s.handleLogin))

	doLogin := func(password, from string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body)))
		req.RemoteAddr = "172.18.0.1:5000" // the proxy, the same for everyone
		req.Header.Set("X-Forwarded-For", from)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	for i := range ratelimit.LoginBurst {
		if rec := doLogin("errada", "100.64.0.7"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, expected 401", i+1, rec.Code)
		}
	}
	log.Reset()
	for range 3 {
		rec := doLogin("senha", "100.64.0.7")
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("after the burst, with the right password: status %d, Set-Cookie %q",
				rec.Code, rec.Header().Get("Set-Cookie"))
		}
		// The clock here is the real one, and the burst's password computations
		// move it forward (about 6 s with -race): the exact wait belongs to the
		// internal/ratelimit test, with a fake clock. Here it is enough to be
		// within the interval.
		if got, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || got < 1 || got > 30 {
			t.Errorf("Retry-After %q, expected 1 to 30", rec.Header().Get("Retry-After"))
		}
	}
	l := log.String()
	if n := strings.Count(l, `msg="login attempt limit reached" client=100.64.0.7 wait_s=`); n != 1 {
		t.Errorf("%d lines for the episode, expected 1: %s", n, l)
	}
	if strings.Contains(l, "request rejected") {
		t.Errorf("the 429 went into the rejected request log: %s", l)
	}

	if rec := doLogin("senha", "100.64.0.8"); rec.Code != http.StatusOK {
		t.Errorf("another address: status %d, expected 200", rec.Code)
	}
}

// With server.loginAttemptsPerMinute at zero, there is no limit at all.
func TestLoginLimitOff(t *testing.T) {
	c := config.Server{LoginAttemptsPerMinute: 0, TrustedProxies: config.DefaultTrustedProxies}
	if l, proxies := loginLimiter(c); l != nil || len(proxies) != 3 {
		t.Errorf("limit %v, proxies %v", l, proxies)
	}
}

// /api/session tells who is logged in, without the credential.
func TestSessionTellsWhoIsIn(t *testing.T) {
	s, _ := testServer(t)
	session := func(c *http.Cookie) (resp struct {
		AuthRequired  bool    `json:"authRequired"`
		Authenticated bool    `json:"authenticated"`
		Person        *person `json:"user"`
	}, body string) {
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

	if r, _ := session(nil); r.AuthRequired || !r.Authenticated || r.Person == nil || !r.Person.admin() {
		t.Errorf("no authentication: %+v", r)
	}

	withAuth(t, s, maria)
	if r, _ := session(nil); !r.AuthRequired || r.Authenticated || r.Person != nil {
		t.Errorf("no cookie: %+v", r)
	}
	r, body := session(cookieOf(t, s, "maria"))
	if !r.Authenticated || r.Person == nil || *r.Person != (person{Username: "maria", Name: "Maria", Role: users.RoleViewer}) {
		t.Errorf("maria: %s", body)
	}
	if strings.Contains(body, maria.PasswordHash) {
		t.Errorf("the response carries the stored password: %s", body)
	}
	if r, _ := session(cookieOf(t, s, "admin")); r.Person == nil || !r.Person.Owner || r.Person.Name != "admin" {
		t.Errorf("owner: %+v", r.Person)
	}
}

// A session in use renews itself: past half of its lifetime, the response
// brings a new cookie with the full lifetime, for the same person. Before that,
// no Set-Cookie, so the cookie is not rewritten on every request. It applies to
// the protected routes and to /api/session, which is what the screen calls when
// it opens.
func TestSessionRenewsWithUse(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)
	p, _ := s.who("maria")
	protected := s.requireAuth(func(w http.ResponseWriter, r *http.Request) {})

	for _, route := range []struct {
		name string
		h    http.HandlerFunc
	}{{"requireAuth", protected}, {"/api/session", s.handleSession}} {
		for _, tc := range []struct {
			missing time.Duration
			renews  bool
		}{
			{sessionTTL - time.Hour, false},
			{sessionRenew + time.Hour, false},
			{sessionRenew - time.Hour, true},
			{time.Hour, true},
		} {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set("X-Forwarded-Proto", "https")
			req.AddCookie(&http.Cookie{Name: sessionCookie,
				Value: s.signToken(p, time.Now().Add(tc.missing).Unix())})
			rec := httptest.NewRecorder()
			route.h(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s, %v left: status %d", route.name, tc.missing, rec.Code)
			}
			setCookie := rec.Header().Get("Set-Cookie")
			if !tc.renews {
				if setCookie != "" {
					t.Errorf("%s, %v left: renewed too early: %s", route.name, tc.missing, setCookie)
				}
				continue
			}
			c, err := http.ParseSetCookie(setCookie)
			if err != nil {
				t.Fatalf("%s, %v left: no new cookie (%v)", route.name, tc.missing, err)
			}
			from, expires, ok := s.tokenPerson(c.Value)
			if !ok || from.Username != "maria" {
				t.Fatalf("%s, %v left: new cookie invalid or from another person (%+v)", route.name, tc.missing, from)
			}
			if missing := time.Until(time.Unix(expires, 0)); missing < sessionTTL-time.Minute {
				t.Errorf("%s: the new cookie is valid for only %v, expected the full lifetime", route.name, missing)
			}
			if !c.Secure || !c.HttpOnly || c.Path != "/" {
				t.Errorf("%s: new cookie lost attributes: %+v", route.name, c)
			}
		}
	}

	// An expired session does not renew: login is still the only way.
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie,
		Value: s.signToken(p, time.Now().Add(-time.Hour).Unix())})
	rec := httptest.NewRecorder()
	protected(rec, req)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("expired session: status %d, Set-Cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}

// The session cookie's Secure only applies behind a TLS proxy. Over plain HTTP,
// the browser would discard the cookie and login would never stick.
func TestSessionCookieIsSecureOnlyOverHTTPS(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s)

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

	for proto, wants := range map[string]bool{"": false, "http": false, "https": true, "HTTPS": true} {
		login := httptest.NewRequest(http.MethodPost, "/api/login",
			strings.NewReader(`{"username":"admin","password":"senha"}`))
		logout := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
		if proto != "" {
			login.Header.Set("X-Forwarded-Proto", proto)
			logout.Header.Set("X-Forwarded-Proto", proto)
		}
		if got := secure(s.handleLogin, login); got != wants {
			t.Errorf("login with X-Forwarded-Proto=%q: Secure=%v, expected %v", proto, got, wants)
		}
		if got := secure(s.handleLogout, logout); got != wants {
			t.Errorf("logout with X-Forwarded-Proto=%q: Secure=%v, expected %v", proto, got, wants)
		}
	}
}
