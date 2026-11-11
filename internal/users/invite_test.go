package users

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func emptyRegistry(t *testing.T) *Registry {
	t.Helper()
	c, _, err := Open(filepath.Join(t.TempDir(), "usuarios.json"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The whole path: create, open the link, set the password and log in with it.
func TestInviteFromCreateToLogin(t *testing.T) {
	c := emptyRegistry(t)
	inv, err := c.Create("maria", "  Maria  ", "")
	if err != nil {
		t.Fatal(err)
	}
	if inv.User.Name != "Maria" || inv.User.Role != RoleViewer || inv.User.PasswordHash != "" {
		t.Errorf("created as %+v", inv.User)
	}
	if got := inv.User.Status(time.Now()); got != "invitePending" {
		t.Errorf("status %q, want invitePending", got)
	}

	// The file keeps the token's hash, never the token.
	b, _ := os.ReadFile(c.path)
	if strings.Contains(string(b), inv.Token) {
		t.Error("the token ended up in usuarios.json")
	}

	if u, err := c.CheckInvite(inv.Token); err != nil || u.Username != "maria" {
		t.Fatalf("check: %+v, %v", u, err)
	}
	if _, err := c.CheckInvite(inv.Token + "x"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("wrong token: %v", err)
	}

	u, err := c.SetPassword(context.Background(), inv.Token, "uma senha boa")
	if err != nil {
		t.Fatal(err)
	}
	if u.Link != nil || u.Status(time.Now()) != "active" || u.ActiveSince.IsZero() {
		t.Errorf("after setting: %+v", u)
	}
	if ok, err := VerifyPassword(context.Background(), "uma senha boa", u.PasswordHash); !ok || err != nil {
		t.Errorf("the set password doesn't verify: ok=%v err=%v", ok, err)
	}

	// Single use.
	if _, err := c.SetPassword(context.Background(), inv.Token, "outra senha boa"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("the link worked twice: %v", err)
	}
}

func TestCreateRejects(t *testing.T) {
	c := emptyRegistry(t)
	if _, err := c.Create("maria", "Maria", ""); err != nil {
		t.Fatal(err)
	}
	cases := map[string][3]string{ // username, name and the rejected field
		"repetido":          {"maria", "Outra Maria", FieldUsername},
		"o dono":            {"admin", "Admin", FieldUsername},
		"fora do formato":   {"Maria", "Maria", FieldUsername},
		"sem nome":          {"jose", "   ", FieldName},
		"nome comprido":     {"jose", strings.Repeat("a", MaxNameLen+1), FieldName},
		"nome com controle": {"jose", "Jo\x00sé", FieldName},
	}
	for name, tc := range cases {
		_, err := c.Create(tc[0], tc[1], "")
		checkRejection(t, name, err, tc[2])
	}
	if n := len(c.List()); n != 1 {
		t.Errorf("%d people in the registry, want only maria", n)
	}
}

// A new link clears the password and invalidates the previous link: only the latest holds.
func TestNewLinkClearsPasswordAndPreviousLink(t *testing.T) {
	c := emptyRegistry(t)
	first, _ := c.Create("maria", "Maria", "")
	if _, err := c.SetPassword(context.Background(), first.Token, "uma senha boa"); err != nil {
		t.Fatal(err)
	}

	second, err := c.NewLink("maria")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := c.Find("maria")
	if u.PasswordHash != "" {
		t.Error("the password still worked after the new link")
	}
	if !u.ActiveSince.IsZero() {
		t.Error("active since stayed after the new link")
	}

	third, _ := c.NewLink("maria")
	if _, err := c.CheckInvite(second.Token); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("the replaced link still works: %v", err)
	}
	if _, err := c.CheckInvite(third.Token); err != nil {
		t.Errorf("the newest link doesn't work: %v", err)
	}

	if _, err := c.NewLink("ninguem"); !errors.Is(err, ErrNotFound) {
		t.Errorf("link for a nonexistent person: %v", err)
	}
}

func TestLinkExpires(t *testing.T) {
	c := emptyRegistry(t)
	inv, _ := c.Create("maria", "Maria", "")

	c.now = func() time.Time { return time.Now().Add(InviteValidity + time.Second) }
	if _, err := c.CheckInvite(inv.Token); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("expired link accepted: %v", err)
	}
	if _, err := c.SetPassword(context.Background(), inv.Token, "uma senha boa"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("password set with an expired link: %v", err)
	}
	u, _ := c.Find("maria")
	if got := u.Status(c.now()); got != "inviteExpired" {
		t.Errorf("status %q, want inviteExpired", got)
	}
}

func TestPasswordOutOfLength(t *testing.T) {
	c := emptyRegistry(t)
	inv, _ := c.Create("maria", "Maria", "")
	// Counts characters, not bytes: "ação" has 4 characters and 6 bytes.
	for _, s := range []string{"1234567", "açãoaçã", strings.Repeat("a", MaxPasswordLen+1)} {
		var rejected ValidationError
		if _, err := c.SetPassword(context.Background(), inv.Token, s); !errors.As(err, &rejected) {
			t.Errorf("password of %d characters: %v", len([]rune(s)), err)
		}
	}
	// The rejection doesn't spend the link.
	if _, err := c.CheckInvite(inv.Token); err != nil {
		t.Errorf("the rejected password spent the link: %v", err)
	}
}

func TestRemove(t *testing.T) {
	c := emptyRegistry(t)
	for _, u := range [][2]string{{"maria", "Maria"}, {"jose", "José"}} {
		if _, err := c.Create(u[0], u[1], ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Remove("maria"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Find("maria"); ok {
		t.Error("maria is still in the registry")
	}
	if _, ok := c.Find("jose"); !ok {
		t.Error("josé vanished along")
	}
	if err := c.Remove("maria"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing again: %v", err)
	}
}

// checkRejection checks that err is a form's rejection with the rejected
// field, and only it.
func checkRejection(t *testing.T, tc string, err error, field string) {
	t.Helper()
	var fieldErrors FieldErrors
	if !errors.As(err, &fieldErrors) {
		t.Errorf("%s: err=%v, want FieldErrors", tc, err)
		return
	}
	if _, ok := fieldErrors[field]; !ok || len(fieldErrors) != 1 {
		t.Errorf("%s: field errors %q, want only field %q", tc, fieldErrors, field)
	}
}

// TestCreateRejectsEachField: with more than one field wrong, each one's
// error comes back, so the screen puts each message under its field.
func TestCreateRejectsEachField(t *testing.T) {
	c := emptyRegistry(t)
	_, err := c.Create("Maria", "", "maria")
	var fieldErrors FieldErrors
	if !errors.As(err, &fieldErrors) || len(fieldErrors) != 3 {
		t.Fatalf("err=%v, want the errors of the 3 fields", err)
	}
	// The first one, in screen order (name, username, e-mail), is the message
	// for whoever shows just one.
	if fieldErrors.Error() != fieldErrors[FieldName] {
		t.Errorf("Error() = %q, want the name's", fieldErrors.Error())
	}
}

// TestCheckNewIsTheSameRuleAsCreate: what the check accepts, Create
// accepts; what it rejects, Create rejects with the same message. And it
// writes nothing.
func TestCheckNewIsTheSameRuleAsCreate(t *testing.T) {
	c := emptyRegistry(t)
	if _, err := c.Create("maria", "Maria", "maria@exemplo.com"); err != nil {
		t.Fatal(err)
	}
	cases := [][3]string{
		{"joao", "João", "joao@exemplo.com"},
		{"joao", "João", ""},
		{"maria", "Outra Maria", "outra@exemplo.com"},
		{"admin", "Admin", ""},
		{"Joao", " ", "MARIA@exemplo.com"},
		{"ana", "Ana", "Ana <ana@exemplo.com>"},
	}
	for _, tc := range cases {
		verified, err := c.CheckNew(tc[0], tc[1], tc[2])
		if err != nil {
			t.Fatal(err)
		}
		if n := c.Count(); n != 1 {
			t.Fatalf("%q: the check wrote (%d entries)", tc, n)
		}
		_, err = c.Create(tc[0], tc[1], tc[2])
		var created FieldErrors
		errors.As(err, &created)
		if (len(verified) == 0 && err != nil) || !maps.Equal(verified, created) {
			t.Errorf("%q: checked %q, Create %v", tc, verified, err)
		}
		if err == nil {
			if err := c.Remove(tc[0]); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestCheckNewCountsTheIgnoredEntry: the login of an invalid entry in the
// file isn't in the screen's list, but Create rejects it, and so does the
// check.
func TestCheckNewCountsTheIgnoredEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.json")
	file := `{"owner":{},"users":[
		{"username":"maria","name":"Maria","role":"chefe","email":"maria@exemplo.com","createdAt":"2026-10-24T10:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Open(path, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.List()) != 0 {
		t.Fatal("the entry with an unknown role should be left out")
	}
	// The login and the e-mail of the same entry: the e-mail is rejected too,
	// and doesn't turn green just because the proposed login is its own.
	if r, _ := c.CheckNew("maria", "Maria", "MARIA@exemplo.com"); r[FieldUsername] == "" || r[FieldEmail] == "" {
		t.Errorf("field errors %q, want the username and the e-mail", r)
	}
}

// TestCheckEmail: SetEmail's rule, without writing.
func TestCheckEmail(t *testing.T) {
	c := emptyRegistry(t)
	for _, u := range [][3]string{{"maria", "Maria", "maria@exemplo.com"}, {"joao", "João", ""}} {
		if _, err := c.Create(u[0], u[1], u[2]); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]struct {
		username, email string
		rejection       bool
	}{
		"o da Maria":             {"joao", "Maria@Exemplo.com", true},
		"fora do formato":        {"joao", "joao", true},
		"vazio apaga":            {"joao", "", false},
		"novo":                   {"joao", "joao@exemplo.com", false},
		"o próprio, outra caixa": {"maria", "MARIA@exemplo.com", false},
	}
	for name, tc := range cases {
		r, err := c.CheckEmail(tc.username, tc.email)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if (r[FieldEmail] != "") != tc.rejection || len(r) > 1 {
			t.Errorf("%s: field errors %q", name, r)
		}
	}
	if u, _ := c.Find("joao"); u.Email != "" {
		t.Errorf("the check wrote the e-mail %q", u.Email)
	}
	if _, err := c.CheckEmail("ninguem", "x@exemplo.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("nonexistent person: %v", err)
	}
}

// TestCheckWithUnreadableFile: with usuarios.json unreadable the in-memory
// registry is empty, and the check returns the error instead of "all
// good".
func TestCheckWithUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Open(path, "admin")
	if err == nil {
		t.Fatal("the broken file should be unreadable")
	}
	if _, err := c.CheckNew("maria", "Maria", ""); err == nil {
		t.Error("CheckNew with the unreadable file gave no error")
	}
	if _, err := c.CheckEmail("maria", ""); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("CheckEmail with the unreadable file: %v, want the file's error", err)
	}
}
