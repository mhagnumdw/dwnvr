package users

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingFileIsAnEmptyRegistry(t *testing.T) {
	c, warnings, err := Open(filepath.Join(t.TempDir(), "usuarios.json"), "admin")
	if err != nil || len(warnings) != 0 || c.Count() != 0 {
		t.Fatalf("err=%v warnings=%v count=%d", err, warnings, c.Count())
	}
	if _, ok := c.Find("maria"); ok {
		t.Error("found someone in an empty registry")
	}
}

func TestUpdateWritesAndRereads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.json")
	c, _, _ := Open(path, "admin")
	err := c.Update(func(a *FileData) error {
		a.Owner.Name = "Dono"
		a.Users = append(a.Users, User{Username: "maria", Name: "Maria", Role: RoleViewer, PasswordHash: "$x$y"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600: the file has the stored passwords", info.Mode().Perm())
	}

	from, _, err := Open(path, "admin")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := from.Find("maria")
	if !ok || u.Name != "Maria" || u.Role != RoleViewer || u.PasswordHash != "$x$y" || from.Owner().Name != "Dono" {
		t.Errorf("reread differs from what was written: %+v (found=%v), owner %+v", u, ok, from.Owner())
	}
}

// A failed change changes nothing, neither in the file nor in memory.
func TestFailedUpdateChangesNothing(t *testing.T) {
	c, _, _ := Open(filepath.Join(t.TempDir(), "usuarios.json"), "admin")
	err := c.Update(func(a *FileData) error {
		a.Users = append(a.Users, User{Username: "maria", Role: RoleViewer})
		return errors.New("recusado")
	})
	if err == nil || c.Count() != 0 {
		t.Errorf("err=%v count=%d", err, c.Count())
	}
	if _, err := os.Stat(c.path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("wrote the file even with the change rejected: %v", err)
	}
}

// An invalid entry is left out of lookups and becomes a warning, but stays
// in the file: the next write doesn't delete it.
func TestInvalidEntryIsLeftOutWithoutVanishing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.json")
	content := `{"users": [
		{"username": "maria", "name": "Maria", "role": "viewer"},
		{"username": "admin", "name": "Outro dono", "role": "admin"},
		{"username": "Joao", "name": "João", "role": "viewer"},
		{"username": "maria", "name": "Outra Maria", "role": "admin"},
		{"username": "ana", "name": "Ana", "role": "chefe"},
		{"username": "bia", "name": "Bia", "role": "admin"}
	]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	c, warnings, err := Open(path, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 4 {
		t.Errorf("want 4 warnings, got %d: %v", len(warnings), warnings)
	}
	for _, want := range []string{"#1", "#2", "#3", "#4"} {
		if !strings.Contains(strings.Join(warnings, "\n"), want) {
			t.Errorf("no warning for entry %s: %v", want, warnings)
		}
	}

	if u, ok := c.Find("maria"); !ok || u.Name != "Maria" {
		t.Errorf("on a repeat the first one wins: %+v %v", u, ok)
	}
	if u, ok := c.Find("bia"); !ok || u.Role != RoleAdmin {
		t.Errorf("bia: %+v %v", u, ok)
	}
	for _, outside := range []string{"admin", "Joao", "ana"} {
		if _, ok := c.Find(outside); ok {
			t.Errorf("%q should have been left out", outside)
		}
	}

	if err := c.Update(func(*FileData) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if from, _, _ := Open(path, "admin"); from.Count() != 6 {
		t.Errorf("the write deleted entries: %d of 6 left", from.Count())
	}
}

// An unreadable file doesn't block boot, but the registry comes empty and
// doesn't write over it.
func TestUnreadableFileIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.json")
	if err := os.WriteFile(path, []byte(`{"users": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Open(path, "admin")
	if err == nil || c == nil || c.Count() != 0 {
		t.Fatalf("err=%v c=%v", err, c)
	}
	if err := c.Update(func(*FileData) error { return nil }); err == nil {
		t.Error("wrote over the unreadable file")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"users": [` {
		t.Errorf("the file changed: %q", b)
	}
}

func TestValidateUsername(t *testing.T) {
	for _, good := range []string{"maria", "joao.silva", "ze_2", "a-b", strings.Repeat("a", 32)} {
		if err := ValidateUsername(good); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"", "Maria", "joão", "a b", "a/b", strings.Repeat("a", 33)} {
		if ValidateUsername(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
