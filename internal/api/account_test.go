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

	"github.com/mhagnumdw/dwnvr/internal/users"
)

// Changing one's own password: it asks for the current one, drops the other
// sessions and keeps the session of whoever changed it alive, with a new
// cookie.
func TestChangePasswordThroughTheAccount(t *testing.T) {
	s, _ := testServer(t)
	withAuth(t, s)
	conv, err := s.users.Create("maria", "Maria", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.users.SetPassword(context.Background(), conv.Token, "uma senha boa"); err != nil {
		t.Fatal(err)
	}
	otherDevice := cookieOf(t, s, "maria")

	rec, _ := requestAs(t, s, "maria", http.MethodPost, "/api/conta/senha",
		map[string]string{"currentPassword": "senha errada", "newPassword": "outra senha boa"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("wrong current password: status %d, expected 400", rec.Code)
	}
	if !accepts(s, otherDevice.Value) {
		t.Error("the wrong attempt dropped the session")
	}

	rec, resp := requestAs(t, s, "maria", http.MethodPost, "/api/conta/senha",
		map[string]string{"currentPassword": "uma senha boa", "newPassword": "outra senha boa"})
	if rec.Code != http.StatusOK {
		t.Fatalf("change: status %d, %s", rec.Code, rec.Body)
	}
	// The last cookie is the one that counts: the test cookie expires in one
	// hour, and requireAuth renews it before the change.
	cs := rec.Result().Cookies()
	if len(cs) == 0 || !accepts(s, cs[len(cs)-1].Value) {
		t.Errorf("whoever changed it was left without a session: %v", cs)
	}
	if accepts(s, otherDevice.Value) {
		t.Error("the other device still gets in with the old password")
	}
	if p, _ := resp["user"].(map[string]any); p["username"] != "maria" {
		t.Errorf("person in the response: %s", rec.Body)
	}

	// The owner changes the password in dwnvr.yaml.
	if rec, _ := requestAs(t, s, "admin", http.MethodPost, "/api/conta/senha",
		map[string]string{"currentPassword": "senha", "newPassword": "outra senha boa"}); rec.Code != http.StatusConflict {
		t.Errorf("owner: status %d, expected 409", rec.Code)
	}
}

// testPhoto is a square gray JPEG.
func testPhoto(t *testing.T, side int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, side, side)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// sendPhoto sends the raw JPEG, as the screen does.
func sendPhoto(t *testing.T, s *Server, username string, photo []byte) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/conta/avatar", bytes.NewReader(photo))
	req.Header.Set("Content-Type", "image/jpeg")
	req.AddCookie(cookieOf(t, s, username))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

func TestNameAndAvatarThroughTheAccount(t *testing.T) {
	jose := users.User{Username: "jose", Name: "José", Role: users.RoleViewer, PasswordHash: "$guardada$do-jose"}
	s, _ := testServer(t)
	withAuth(t, s, maria, jose)

	rec, resp := requestAs(t, s, "maria", http.MethodGet, "/api/conta", nil)
	if rec.Code != http.StatusOK || resp["minPasswordLength"] != float64(users.MinPasswordLen) {
		t.Errorf("account: status %d, %s", rec.Code, rec.Body)
	}

	rec, resp = requestAs(t, s, "maria", http.MethodPost, "/api/conta/nome", map[string]string{"name": "Maria Clara"})
	if p, _ := resp["user"].(map[string]any); rec.Code != http.StatusOK || p["name"] != "Maria Clara" {
		t.Errorf("nome: status %d, %s", rec.Code, rec.Body)
	}
	if rec, _ := requestAs(t, s, "admin", http.MethodPost, "/api/conta/nome", map[string]string{"name": "Dono"}); rec.Code != http.StatusOK {
		t.Errorf("owner's name: status %d, %s", rec.Code, rec.Body)
	}
	if p, _ := s.who("admin"); p.Name != "Dono" {
		t.Errorf("the owner is still %q", p.Name)
	}

	rec, resp = sendPhoto(t, s, "maria", testPhoto(t, users.AvatarSide))
	p, _ := resp["user"].(map[string]any)
	id, _ := p["avatar"].(string)
	if rec.Code != http.StatusOK || id == "" {
		t.Fatalf("photo: status %d, %s", rec.Code, rec.Body)
	}
	if rec, _ := sendPhoto(t, s, "maria", make([]byte, users.MaxAvatarBytes+1)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("photo above the cap: status %d, expected 413", rec.Code)
	}
	if rec, _ := sendPhoto(t, s, "maria", []byte("não é foto")); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid photo: status %d, expected 400", rec.Code)
	}

	// The person themselves and the admin see the photo; the others do not.
	for username, wants := range map[string]int{"maria": http.StatusOK, "admin": http.StatusOK, "jose": http.StatusNotFound} {
		rec, _ := requestAs(t, s, username, http.MethodGet, "/api/avatar?id="+id, nil)
		if rec.Code != wants {
			t.Errorf("%s viewing maria's photo: status %d, expected %d", username, rec.Code, wants)
		}
		if wants == http.StatusOK && (rec.Header().Get("Content-Type") != "image/jpeg" ||
			rec.Header().Get("X-Content-Type-Options") != "nosniff") {
			t.Errorf("%s: headers %v", username, rec.Header())
		}
	}

	// The admin removes other people's photos; the viewer does not.
	if rec, _ := requestAs(t, s, "jose", http.MethodDelete, "/api/usuarios/avatar?username=maria", nil); rec.Code != http.StatusForbidden {
		t.Errorf("viewer removing someone else's photo: status %d", rec.Code)
	}
	if rec, _ := requestAs(t, s, "admin", http.MethodDelete, "/api/usuarios/avatar?username=maria", nil); rec.Code != http.StatusOK {
		t.Errorf("admin removing the photo: status %d, %s", rec.Code, rec.Body)
	}
	if rec, _ := requestAs(t, s, "maria", http.MethodGet, "/api/avatar?id="+id, nil); rec.Code != http.StatusNotFound {
		t.Errorf("removed photo is still served: status %d", rec.Code)
	}

	if rec, _ := sendPhoto(t, s, "jose", testPhoto(t, 64)); rec.Code != http.StatusOK {
		t.Fatalf("josé's photo: status %d", rec.Code)
	}
	rec, resp = requestAs(t, s, "jose", http.MethodDelete, "/api/conta/avatar", nil)
	if p, _ := resp["user"].(map[string]any); rec.Code != http.StatusOK || p["avatar"] != nil {
		t.Errorf("removing one's own photo: status %d, %s", rec.Code, rec.Body)
	}
}

// Without authentication there is no one to own the account and no registry:
// whoever reaches the screen is admin, and nothing that writes to usuarios.json
// or avatares/ goes through.
func TestNoAuthNothingIsSaved(t *testing.T) {
	s, _ := testServer(t)
	routes := []string{
		"GET /api/conta", "POST /api/conta/senha", "POST /api/conta/nome",
		"PUT /api/conta/avatar", "DELETE /api/conta/avatar",
		"POST /api/usuarios/link", "DELETE /api/usuarios?username=maria",
		"DELETE /api/usuarios/avatar?username=maria",
	}
	for _, r := range routes {
		method, route, _ := strings.Cut(r, " ")
		if rec, _ := requestAs(t, s, "", method, route, map[string]string{}); rec.Code != http.StatusConflict {
			t.Errorf("%s without authentication: status %d, expected 409", r, rec.Code)
		}
	}
	if _, err := os.Stat(s.cfg.UsersPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("usuarios.json was saved without authentication: %v", err)
	}
}
