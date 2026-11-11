package users

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEmailOnlyTheAddress: the e-mail is a single address, without a name
// and without a list, and the surrounding space goes away.
func TestEmailOnlyTheAddress(t *testing.T) {
	c := emptyRegistry(t)
	inv, err := c.Create("maria", "Maria", "  maria@exemplo.com ")
	if err != nil {
		t.Fatal(err)
	}
	if inv.User.Email != "maria@exemplo.com" || inv.User.ValidEmail() != "maria@exemplo.com" {
		t.Errorf("e-mail %q", inv.User.Email)
	}
	for i, email := range []string{
		"maria",
		"Maria <maria@exemplo.com>",
		"maria@exemplo.com, joao@exemplo.com",
		strings.Repeat("a", MaxEmailLen) + "@exemplo.com",
	} {
		_, err := c.Create("pessoa"+string(rune('a'+i)), "Pessoa", email)
		checkRejection(t, email, err, FieldEmail)
	}
}

// TestEmailIsUnique: two people don't have the same e-mail, not even with a
// different case, so a future login by e-mail knows who is coming in.
func TestEmailIsUnique(t *testing.T) {
	c := emptyRegistry(t)
	if _, err := c.Create("maria", "Maria", "Maria@Exemplo.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create("joao", "João", ""); err != nil {
		t.Fatal(err)
	}
	_, err := c.Create("ana", "Ana", "maria@exemplo.com")
	checkRejection(t, "criar com o e-mail da Maria", err, FieldEmail)
	_, err = c.SetEmail("joao", "MARIA@exemplo.com")
	checkRejection(t, "trocar para o e-mail da Maria", err, FieldEmail)
	// One's own e-mail, in another case, doesn't conflict with itself.
	if _, err := c.SetEmail("maria", "maria@exemplo.com"); err != nil {
		t.Errorf("Maria changing the case of her own e-mail: %v", err)
	}
}

// TestSetEmail: changes and clears it without touching the link or the password.
func TestSetEmail(t *testing.T) {
	c := emptyRegistry(t)
	inv, err := c.Create("maria", "Maria", "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.SetEmail("maria", "maria@exemplo.com")
	if err != nil || u.Email != "maria@exemplo.com" {
		t.Fatalf("change: %+v, %v", u, err)
	}
	if u.Link == nil || u.Link.TokenSha256 != inv.User.Link.TokenSha256 {
		t.Error("changing the e-mail touched the link")
	}
	if u, err = c.SetEmail("maria", ""); err != nil || u.Email != "" {
		t.Errorf("clear: %+v, %v", u, err)
	}
	if _, err := c.SetEmail("ninguem", "x@exemplo.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("nonexistent person: %v", err)
	}
}

// TestBadEmailInFileDoesNotVanish: an e-mail written wrong by hand becomes a
// warning and counts as no e-mail, but stays in the file, like the invalid
// entry.
func TestBadEmailInFileDoesNotVanish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.json")
	file := `{"owner":{},"users":[
		{"username":"maria","name":"Maria","role":"viewer","email":"Maria <maria@exemplo.com>","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"joao","name":"João","role":"viewer","email":"joao@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"ana","name":"Ana","role":"viewer","email":"JOAO@exemplo.com","createdAt":"2026-10-24T10:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	c, warnings, err := Open(path, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], "maria") || !strings.Contains(warnings[1], `o mesmo de "joao"`) {
		t.Errorf("warnings %q", warnings)
	}
	maria, _ := c.Find("maria")
	if maria.ValidEmail() != "" {
		t.Errorf("bad e-mail in effect: %q", maria.ValidEmail())
	}

	// A repeated one counts too: nobody else takes João's e-mail while Ana's
	// copy is in the file, not even João.
	_, err = c.SetEmail("joao", "joao@exemplo.com")
	checkRejection(t, "regravar o e-mail repetido", err, FieldEmail)

	// Writing another person doesn't delete the bad e-mail from the file.
	if _, err := c.SetEmail("ana", ""); err != nil {
		t.Fatal(err)
	}
	reread, _, err := Open(path, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if maria, _ := reread.Find("maria"); maria.Email != "Maria <maria@exemplo.com>" {
		t.Errorf("the bad e-mail became %q in the file", maria.Email)
	}
}

// TestFindByEmail: finds it case-insensitively; an e-mail repeated in the
// file and an ignored entry find nobody.
func TestFindByEmail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.json")
	file := `{"owner":{},"users":[
		{"username":"maria","name":"Maria","role":"viewer","email":"Maria@Exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"joao","name":"João","role":"viewer","email":"joao@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"ana","name":"Ana","role":"viewer","email":"JOAO@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"Zé","name":"Zé","role":"viewer","email":"ze@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"os","name":"Os","role":"viewer","email":"os@exemplo.com","createdAt":"2026-10-24T10:00:00Z"},
		{"username":"os2","name":"Os 2","role":"viewer","email":"oſ@exemplo.com","createdAt":"2026-10-24T10:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	c, warnings, err := Open(path, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := c.FindByEmail("maria@exemplo.com"); !ok || u.Username != "maria" {
		t.Errorf("maria by e-mail: %+v, %v", u, ok)
	}
	for _, email := range []string{"joao@exemplo.com", "ze@exemplo.com", "ninguem@exemplo.com", "", "os@exemplo.com"} {
		if u, ok := c.FindByEmail(email); ok {
			t.Errorf("%q found %q", email, u.Username)
		}
	}
	// A repeat only EqualFold matches ("s" and "ſ") gets a warning too.
	for _, from := range []string{`o mesmo de "joao"`, `o mesmo de "os"`} {
		if !slices.ContainsFunc(warnings, func(a string) bool { return strings.Contains(a, from+", e nenhum dos dois entra pelo e-mail") }) {
			t.Errorf("without the warning %q: %q", from, warnings)
		}
	}

	// Removing the repeat on screen removes the warning and frees the e-mail, without a restart.
	if _, err := c.SetEmail("ana", ""); err != nil {
		t.Fatal(err)
	}
	if u, ok := c.FindByEmail("JOAO@exemplo.com"); !ok || u.Username != "joao" {
		t.Errorf("joao after removing the repeat: %+v, %v", u, ok)
	}
	if av := c.Warnings(); len(av) != 2 || !strings.Contains(av[0], `"Zé"`) {
		t.Errorf("warnings after removing the repeat: %q", av)
	}
}
