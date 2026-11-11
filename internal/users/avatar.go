package users

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
)

// The avatar is the person's photo, a square JPEG of up to AvatarSide
// pixels. The scaling is done by the browser: the original photo doesn't leave
// the device, and the server only checks and stores the bytes as they came,
// without compressing them again.
//
// Each photo is a file avatares/<id>.jpg, and usuarios.json keeps the id. The
// id changes on every swap, which lets the browser cache the image forever: a
// new photo is a new URL.

var avatarIDFormat = regexp.MustCompile(fmt.Sprintf(`^[0-9a-f]{%d}$`, 2*AvatarIDBytes))

// CheckAvatar says whether the bytes are an accepted avatar: up to
// MaxAvatarBytes bytes, and a whole JPEG, square, of up to AvatarSide pixels.
// It decodes the whole image, not just the header, so a truncated file or one
// that only starts like a JPEG doesn't get through.
func CheckAvatar(b []byte) error {
	if len(b) > MaxAvatarBytes {
		return ValidationError(fmt.Sprintf("a foto passa de %d KB", MaxAvatarBytes>>10))
	}
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return ValidationError("a foto não é um JPEG legível")
	}
	l := img.Bounds()
	if l.Dx() != l.Dy() || l.Dx() > AvatarSide {
		return ValidationError(fmt.Sprintf("a foto precisa ser quadrada, de até %d pixels", AvatarSide))
	}
	return nil
}

// avatarField says where the file keeps the person's avatar, or nil if they
// don't exist. The owner has an avatar too.
func (c *Registry) avatarField(a *FileData, username string) *string {
	if username == c.owner {
		return &a.Owner.Avatar
	}
	if i := c.indexOf(a, username); i >= 0 {
		return &a.Users[i].Avatar
	}
	return nil
}

func (c *Registry) avatarPath(id string) string {
	return filepath.Join(c.avatarDir, id+".jpg")
}

// deleteAvatar deletes the photo file. Failing only leaves a stray file that
// nobody reaches: the id left usuarios.json.
func (c *Registry) deleteAvatar(id string) {
	if avatarIDFormat.MatchString(id) {
		_ = os.Remove(c.avatarPath(id))
	}
}

// SetAvatar stores the person's photo and returns its id. The previous photo
// is deleted.
func (c *Registry) SetAvatar(username string, b []byte) (string, error) {
	if err := CheckAvatar(b); err != nil {
		return "", err
	}
	r := make([]byte, AvatarIDBytes)
	if _, err := rand.Read(r); err != nil {
		return "", err
	}
	id := hex.EncodeToString(r)

	// The file comes before usuarios.json: an id written without the file would
	// be a broken photo on screen; the file without the id is just one more file.
	// 0700 and 0600 like the rest of the config folder: they're photos of
	// people.
	if err := os.MkdirAll(c.avatarDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(c.avatarPath(id), b, 0o600); err != nil {
		return "", err
	}
	var old string
	err := c.Update(func(a *FileData) error {
		field := c.avatarField(a, username)
		if field == nil {
			return ErrNotFound
		}
		old, *field = *field, id
		return nil
	})
	if err != nil {
		c.deleteAvatar(id)
		return "", err
	}
	c.deleteAvatar(old)
	return id, nil
}

// RemoveAvatar deletes the person's photo. Whoever has no photo stays as they are.
func (c *Registry) RemoveAvatar(username string) error {
	var old string
	err := c.Update(func(a *FileData) error {
		field := c.avatarField(a, username)
		if field == nil {
			return ErrNotFound
		}
		old, *field = *field, ""
		return nil
	})
	if err == nil {
		c.deleteAvatar(old)
	}
	return err
}

// ErrNoAvatar is an id that isn't anyone's photo.
var ErrNoAvatar = errors.New("foto não encontrada")

// Avatar finds the photo by id: the file path and whose it is, for whoever
// serves it to decide whether the requester may see it.
func (c *Registry) Avatar(id string) (path, username string, err error) {
	if !avatarIDFormat.MatchString(id) {
		return "", "", ErrNoAvatar
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data.Owner.Avatar == id {
		return c.avatarPath(id), c.owner, nil
	}
	seen := map[string]bool{}
	for _, u := range c.data.Users {
		valid := c.ignoreReason(u, seen) == ""
		seen[u.Username] = true
		if valid && u.Avatar == id {
			return c.avatarPath(id), u.Username, nil
		}
	}
	return "", "", ErrNoAvatar
}
