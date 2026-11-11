package users

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"testing"
)

// withPassword creates the person and sets their password through the link.
func withPassword(t *testing.T, c *Registry, username, password string) User {
	t.Helper()
	inv, err := c.Create(username, username, "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.SetPassword(context.Background(), inv.Token, password)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestChangePassword(t *testing.T) {
	c := emptyRegistry(t)
	before := withPassword(t, c, "maria", "uma senha boa")
	ctx := context.Background()

	var rejected ValidationError
	if _, err := c.ChangePassword(ctx, "maria", "senha errada", "outra senha boa"); !errors.As(err, &rejected) {
		t.Errorf("wrong current password: %v", err)
	}
	if _, err := c.ChangePassword(ctx, "maria", "uma senha boa", "curta"); !errors.As(err, &rejected) {
		t.Errorf("short new password: %v", err)
	}
	if _, err := c.ChangePassword(ctx, "jose", "uma senha boa", "outra senha boa"); !errors.Is(err, ErrNotFound) {
		t.Errorf("nonexistent person: %v", err)
	}

	u, err := c.ChangePassword(ctx, "maria", "uma senha boa", "outra senha boa")
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash == before.PasswordHash {
		t.Error("the stored password didn't change")
	}
	if ok, _ := VerifyPassword(ctx, "outra senha boa", u.PasswordHash); !ok {
		t.Error("the new password doesn't verify")
	}

	// Without a password (a new open link) there's nothing to change.
	if _, err := c.NewLink("maria"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ChangePassword(ctx, "maria", "outra senha boa", "mais uma senha"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after the new link: %v", err)
	}
}

func TestRename(t *testing.T) {
	c := emptyRegistry(t)
	if _, err := c.Create("maria", "Maria", ""); err != nil {
		t.Fatal(err)
	}
	if name, err := c.Rename("maria", "  Maria Clara "); err != nil || name != "Maria Clara" {
		t.Errorf("rename: %q, %v", name, err)
	}
	if u, _ := c.Find("maria"); u.Name != "Maria Clara" {
		t.Errorf("in the registry: %q", u.Name)
	}
	// The owner isn't in Users: their name goes to its own section.
	if _, err := c.Rename("admin", "Dono da Casa"); err != nil || c.Owner().Name != "Dono da Casa" {
		t.Errorf("owner: %q, %v", c.Owner().Name, err)
	}
	var rejected ValidationError
	if _, err := c.Rename("maria", "   "); !errors.As(err, &rejected) {
		t.Errorf("empty name: %v", err)
	}
	if _, err := c.Rename("jose", "José"); !errors.Is(err, ErrNotFound) {
		t.Errorf("nonexistent person: %v", err)
	}
}

// testPhoto is a gray JPEG of side x height.
func testPhoto(t *testing.T, side, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, side, height)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCheckAvatar(t *testing.T) {
	good := testPhoto(t, AvatarSide, AvatarSide)
	cases := map[string][]byte{
		"retangular":    testPhoto(t, AvatarSide, AvatarSide-1),
		"grande demais": testPhoto(t, AvatarSide+1, AvatarSide+1),
		"truncada":      good[:len(good)/2],
		"não é JPEG":    []byte("\x89PNG\r\n\x1a\n"),
		"passa do teto": append(append([]byte{}, good...), make([]byte, MaxAvatarBytes)...),
	}
	for name, b := range cases {
		var rejected ValidationError
		if err := CheckAvatar(b); !errors.As(err, &rejected) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := CheckAvatar(good); err != nil {
		t.Errorf("the good one: %v", err)
	}
	if err := CheckAvatar(testPhoto(t, 64, 64)); err != nil {
		t.Errorf("smaller than the side: %v", err)
	}
}

func TestAvatar(t *testing.T) {
	c := emptyRegistry(t)
	if _, err := c.Create("maria", "Maria", ""); err != nil {
		t.Fatal(err)
	}
	photo := testPhoto(t, AvatarSide, AvatarSide)

	id1, err := c.SetAvatar("maria", photo)
	if err != nil {
		t.Fatal(err)
	}
	path, owner, err := c.Avatar(id1)
	if err != nil || owner != "maria" {
		t.Fatalf("finding the photo: %q, %v", owner, err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, photo) {
		t.Error("the stored photo isn't the one sent")
	}

	// Swapping deletes the previous one, and the old URL stops working.
	id2, err := c.SetAvatar("maria", photo)
	if err != nil || id2 == id1 {
		t.Fatalf("swap: %q, %v", id2, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the previous photo stayed on disk: %v", err)
	}
	if _, _, err := c.Avatar(id1); !errors.Is(err, ErrNoAvatar) {
		t.Errorf("the old id still finds a photo: %v", err)
	}

	// The owner has one too.
	ownerID, err := c.SetAvatar("admin", photo)
	if err != nil {
		t.Fatal(err)
	}
	if _, owner, _ := c.Avatar(ownerID); owner != "admin" || c.Owner().Avatar != ownerID {
		t.Errorf("owner's photo: %q", owner)
	}

	if _, err := c.SetAvatar("jose", photo); !errors.Is(err, ErrNotFound) {
		t.Errorf("nonexistent person: %v", err)
	}
	if _, _, err := c.Avatar("../usuarios"); !errors.Is(err, ErrNoAvatar) {
		t.Errorf("id outside the format: %v", err)
	}

	path2, _, _ := c.Avatar(id2)
	if err := c.RemoveAvatar("maria"); err != nil {
		t.Fatal(err)
	}
	if u, _ := c.Find("maria"); u.Avatar != "" {
		t.Errorf("the id stayed in the registry: %q", u.Avatar)
	}
	if _, err := os.Stat(path2); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the removed photo stayed on disk: %v", err)
	}
}

func TestRemoveDeletesTheAvatar(t *testing.T) {
	c := emptyRegistry(t)
	if _, err := c.Create("maria", "Maria", ""); err != nil {
		t.Fatal(err)
	}
	id, err := c.SetAvatar("maria", testPhoto(t, 32, 32))
	if err != nil {
		t.Fatal(err)
	}
	path, _, _ := c.Avatar(id)
	if err := c.Remove("maria"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the photo of whoever left stayed on disk: %v", err)
	}
}
