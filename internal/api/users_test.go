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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/users"
)

// requestAs makes the request through Handler, with the requester's cookie (or
// no cookie, with an empty username), and returns the status and the body read
// as JSON.
func requestAs(t *testing.T, s *Server, username, method, route string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, route, reader)
	if username != "" {
		req.AddCookie(cookieOf(t, s, username))
	}
	if s.client == nil {
		s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"}) // the Handler builds the live proxy with it
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

// From the admin creating the person to the person signing in with the password
// they set themselves.
func TestInviteFromScreenToLogin(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s)

	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios",
		map[string]string{"username": "maria", "name": "Maria"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d, %s", rec.Code, rec.Body)
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatalf("create did not return the token: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "tokenSha256") {
		t.Errorf("the response carries the token hash: %s", rec.Body)
	}

	rec, resp = requestAs(t, s, "", http.MethodPost, "/api/convite/conferir", map[string]string{"token": token})
	if rec.Code != http.StatusOK || resp["name"] != "Maria" || resp["minPasswordLength"] != float64(users.MinPasswordLen) {
		t.Errorf("check: status %d, %s", rec.Code, rec.Body)
	}

	rec, _ = requestAs(t, s, "", http.MethodPost, "/api/convite", map[string]string{"token": token, "password": "curta"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("short password: status %d, expected 400", rec.Code)
	}

	rec, _ = requestAs(t, s, "", http.MethodPost, "/api/convite", map[string]string{"token": token, "password": "uma senha boa"})
	if rec.Code != http.StatusOK {
		t.Fatalf("set the password: status %d, %s", rec.Code, rec.Body)
	}
	c, err := http.ParseSetCookie(rec.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatalf("setting the password did not open the session: %v", err)
	}
	if p, _, ok := s.tokenPerson(c.Value); !ok || p.Username != "maria" || p.Role != users.RoleViewer {
		t.Errorf("session opened for %+v (ok=%v)", p, ok)
	}

	// Single use.
	rec, _ = requestAs(t, s, "", http.MethodPost, "/api/convite", map[string]string{"token": token, "password": "outra senha boa"})
	if rec.Code != http.StatusGone {
		t.Errorf("link used again: status %d, expected 410", rec.Code)
	}

	rec, resp = requestAs(t, s, "admin", http.MethodGet, "/api/usuarios", nil)
	list, _ := resp["users"].([]any)
	if rec.Code != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["status"] != "active" {
		t.Errorf("list: status %d, %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "pbkdf2") {
		t.Errorf("the list carries the stored password: %s", rec.Body)
	}
}

func TestCreateUserRejects(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)
	for _, body := range []map[string]string{
		{"username": "maria", "name": "Outra"}, // duplicate
		{"username": "admin", "name": "Dono"},  // the owner
		{"username": "Zé", "name": "Zé"},       // wrong format
		{"username": "ze", "name": ""},
	} {
		if rec, _ := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d, expected 400", body, rec.Code)
		}
	}
	if rec, _ := requestAs(t, s, "admin", http.MethodDelete, "/api/usuarios?username=ninguem", nil); rec.Code != http.StatusNotFound {
		t.Errorf("remove someone who does not exist: status %d", rec.Code)
	}
}

// fieldsOf is the "fields" of the response: the per-field rejections.
func fieldsOf(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	fields, ok := resp["fields"].(map[string]any)
	if !ok {
		t.Fatalf("response without fields: %v", resp)
	}
	return fields
}

// The create rejection comes field by field, so that the screen puts each
// message under its field, and the first one also in the usual "error".
func TestCreateUserRejectsByField(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)
	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios",
		map[string]string{"username": "maria", "name": "Outra", "email": "outra"})
	fields := fieldsOf(t, resp)
	if rec.Code != http.StatusBadRequest || len(fields) != 2 || fields["username"] == nil || fields["email"] == nil {
		t.Errorf("status %d, %s", rec.Code, rec.Body)
	}
	if resp["error"] != fields["username"] {
		t.Errorf("error %q, expected the username rejection", resp["error"])
	}
}

// The check answers what create and change e-mail would reject, without saving,
// and only for the admin.
func TestCheckUsername(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)
	if _, err := s.users.SetEmail("maria", "maria@exemplo.com"); err != nil {
		t.Fatal(err)
	}

	rec, resp := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/conferir",
		map[string]string{"username": "Maria", "name": " ", "email": "MARIA@exemplo.com"})
	if fields := fieldsOf(t, resp); rec.Code != http.StatusOK || len(fields) != 3 {
		t.Errorf("rejections: status %d, %s", rec.Code, rec.Body)
	}
	rec, resp = requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/conferir",
		map[string]string{"username": "joao", "name": "João", "email": ""})
	if fields := fieldsOf(t, resp); rec.Code != http.StatusOK || len(fields) != 0 {
		t.Errorf("all good: status %d, %s", rec.Code, rec.Body)
	}
	if _, ok := s.users.Find("joao"); ok {
		t.Error("the check created the person")
	}

	rec, resp = requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/email/conferir",
		map[string]string{"username": "maria", "email": "Maria@Exemplo.com"})
	if fields := fieldsOf(t, resp); rec.Code != http.StatusOK || len(fields) != 0 {
		t.Errorf("their own e-mail: status %d, %s", rec.Code, rec.Body)
	}
	// A free and different e-mail: accepted, and the saved one does not change.
	rec, resp = requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/email/conferir",
		map[string]string{"username": "maria", "email": "outra@exemplo.com"})
	if fields := fieldsOf(t, resp); rec.Code != http.StatusOK || len(fields) != 0 {
		t.Errorf("free e-mail: status %d, %s", rec.Code, rec.Body)
	}
	if u, _ := s.users.Find("maria"); u.Email != "maria@exemplo.com" {
		t.Errorf("the check saved the e-mail: %q", u.Email)
	}
	if rec, _ = requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/email/conferir",
		map[string]string{"username": "ninguem", "email": "x@exemplo.com"}); rec.Code != http.StatusNotFound {
		t.Errorf("someone who does not exist: status %d, expected 404", rec.Code)
	}

	for _, route := range []string{"/api/usuarios/conferir", "/api/usuarios/email/conferir"} {
		if rec, _ = requestAs(t, s, "maria", http.MethodPost, route,
			map[string]string{"username": "maria"}); rec.Code != http.StatusForbidden {
			t.Errorf("%s by the viewer: status %d, expected 403", route, rec.Code)
		}
	}
}

// Without authentication there is no registry: the screen does not save, and no
// link is valid.
func TestNoAuthNoRegistry(t *testing.T) {
	s, _ := testServer(t)
	if rec, _ := requestAs(t, s, "", http.MethodPost, "/api/usuarios",
		map[string]string{"username": "maria", "name": "Maria"}); rec.Code != http.StatusConflict {
		t.Errorf("create without authentication: status %d, expected 409", rec.Code)
	}
	if rec, _ := requestAs(t, s, "", http.MethodPost, "/api/usuarios/conferir",
		map[string]string{"username": "maria", "name": "Maria"}); rec.Code != http.StatusConflict {
		t.Errorf("check the new one without authentication: status %d, expected 409", rec.Code)
	}
	if rec, _ := requestAs(t, s, "", http.MethodPost, "/api/convite/conferir",
		map[string]string{"token": "x"}); rec.Code != http.StatusGone {
		t.Errorf("check without authentication: status %d, expected 410", rec.Code)
	}
	if rec, resp := requestAs(t, s, "", http.MethodGet, "/api/usuarios", nil); rec.Code != http.StatusOK || resp["authRequired"] != false {
		t.Errorf("list without authentication: status %d, %s", rec.Code, rec.Body)
	}
}

// accessOnScreen is the person's lastAccessMs in GET /api/usuarios, requested
// by the admin, or nil if it did not come.
func accessOnScreen(t *testing.T, s *Server, username string) any {
	t.Helper()
	_, resp := requestAs(t, s, "admin", http.MethodGet, "/api/usuarios", nil)
	list, _ := resp["users"].([]any)
	for _, u := range list {
		if m, _ := u.(map[string]any); m["username"] == username {
			return m["lastAccessMs"]
		}
	}
	t.Fatalf("%s not in the list: %v", username, resp)
	return nil
}

// The last access: any request with a session records it, the Users screen
// shows it, and removing the person deletes it.
func TestLastAccessOnUsersScreen(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s, maria)

	if got := accessOnScreen(t, s, "maria"); got != nil {
		t.Errorf("before she used dwnvr, lastAccessMs=%v", got)
	}

	before := time.Now().Truncate(time.Second).UnixMilli()
	if rec, _ := requestAs(t, s, "maria", http.MethodGet, "/api/session", nil); rec.Code != http.StatusOK {
		t.Fatalf("maria's session: status %d", rec.Code)
	}
	got, _ := accessOnScreen(t, s, "maria").(float64)
	if int64(got) < before || int64(got) > time.Now().UnixMilli() {
		t.Errorf("after she used it, lastAccessMs=%v, expected between %d and now", got, before)
	}

	// The admin who lists has also just used it: the owner comes with theirs.
	_, resp := requestAs(t, s, "admin", http.MethodGet, "/api/usuarios", nil)
	if owner, _ := resp["owner"].(map[string]any); owner["lastAccessMs"] == nil {
		t.Errorf("the owner came without lastAccessMs: %v", resp["owner"])
	}

	if rec, _ := requestAs(t, s, "admin", http.MethodDelete, "/api/usuarios?username=maria", nil); rec.Code != http.StatusOK {
		t.Fatalf("remove: status %d, %s", rec.Code, rec.Body)
	}
	if got := s.accesses.Last("maria"); !got.IsZero() {
		t.Errorf("after removal, maria's access remained: %v", got)
	}
}

// The acessos.json the boot reads may have the access of whoever had the same
// username before, if dwnvr went down before saving the Forget. An access from
// before the account was created does not show up; the new person's does.
func TestAccessFromBeforeTheAccountIsHidden(t *testing.T) {
	s, _ := testServer(t)
	created := time.Now().Truncate(time.Second)
	fresh := maria
	fresh.CreatedAt = created
	withAuth(t, s, fresh)

	old := created.Add(-time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(s.cfg.AccessPath(), []byte(`{"lastAccess":{"maria":"`+old+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	accesses, err := users.OpenAccessLog(s.cfg.AccessPath(), s.log)
	if err != nil {
		t.Fatal(err)
	}
	s.accesses = accesses

	if got := accessOnScreen(t, s, "maria"); got != nil {
		t.Errorf("access from before the account showed up: lastAccessMs=%v", got)
	}

	if rec, _ := requestAs(t, s, "maria", http.MethodGet, "/api/session", nil); rec.Code != http.StatusOK {
		t.Fatalf("maria's session: status %d", rec.Code)
	}
	if got := accessOnScreen(t, s, "maria"); got == nil {
		t.Error("the new maria's access did not show up")
	}
}

// With authentication off there is no one to record the access for, and no file
// is created.
func TestNoAuthNoLastAccess(t *testing.T) {
	s, _ := testServer(t)
	if rec, _ := requestAs(t, s, "", http.MethodGet, "/api/cameras", nil); rec.Code != http.StatusOK {
		t.Fatalf("cameras without authentication: status %d", rec.Code)
	}
	if err := s.accesses.FlushPending(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.cfg.AccessPath()); !os.IsNotExist(err) {
		t.Errorf("acessos.json created without authentication (stat: %v)", err)
	}
}

// handshake is the websocket request, with the person's cookie. The key is the
// one from the RFC 6455 example, built from its nonce so that the commit secret
// detector does not take it for a credential.
func handshake(route string, c *http.Cookie) string {
	key := base64.StdEncoding.EncodeToString([]byte("the sample nonce"))
	return "GET " + route + " HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\nCookie: " + c.String() + "\r\n\r\n"
}

// openWarning opens the session warning with the person's cookie, on a real
// server: httptest.ResponseRecorder does not hijack.
func openWarning(t *testing.T, srv *httptest.Server, c *http.Cookie) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_, _ = io.WriteString(conn, handshake("/api/session/ws", c))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols ||
		resp.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" { // the RFC 6455 example
		t.Fatalf("handshake: %d, accept %q", resp.StatusCode, resp.Header.Get("Sec-WebSocket-Accept"))
	}
	return conn
}

// closes tells whether the server closed the connection within the deadline.
func closes(conn net.Conn, deadline time.Duration) bool {
	_ = conn.SetReadDeadline(time.Now().Add(deadline))
	_, err := conn.Read(make([]byte, 1))
	return errors.Is(err, io.EOF)
}

// Generating a new link or removing drops right away what the person has open,
// and only theirs.
func TestDropsThePersonsConnections(t *testing.T) {
	s, _ := testServer(t)
	ze := maria
	ze.Username, ze.Name = "ze", "Zé"
	withAuth(t, s, maria, ze)
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ofMaria := openWarning(t, srv, cookieOf(t, s, "maria"))
	zesLink := openWarning(t, srv, cookieOf(t, s, "ze"))
	if closes(ofMaria, 100*time.Millisecond) {
		t.Fatal("the warning closed on its own")
	}

	if rec, _ := requestAs(t, s, "admin", http.MethodPost, "/api/usuarios/link",
		map[string]string{"username": "maria"}); rec.Code != http.StatusOK {
		t.Fatalf("link novo: status %d, %s", rec.Code, rec.Body)
	}
	if !closes(ofMaria, 2*time.Second) {
		t.Error("maria's warning stayed open after the new link")
	}
	if closes(zesLink, 100*time.Millisecond) {
		t.Error("zé's warning dropped too")
	}

	if rec, _ := requestAs(t, s, "admin", http.MethodDelete, "/api/usuarios?username=ze", nil); rec.Code != http.StatusOK {
		t.Fatalf("remove: status %d", rec.Code)
	}
	if !closes(zesLink, 2*time.Second) {
		t.Error("zé's warning stayed open after removal")
	}
}

// The live view websocket also counts: the proxy records the connection that
// ReverseProxy takes over.
func TestDropsTheLiveView(t *testing.T) {
	// A fake go2rtc, which accepts the websocket and stays idle.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		_, _ = io.Copy(io.Discard, rw)
	}))
	defer fake.Close()

	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: fake.URL})
	withAuth(t, s, maria)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, handshake("/api/live/ws?src=cam_teste", cookieOf(t, s, "maria")))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("live view: status %d", resp.StatusCode)
	}

	if rec, _ := requestAs(t, s, "admin", http.MethodDelete, "/api/usuarios?username=maria", nil); rec.Code != http.StatusOK {
		t.Fatalf("remove: status %d", rec.Code)
	}
	if !closes(conn, 2*time.Second) {
		t.Error("the live view stayed open after the person was removed")
	}
}
