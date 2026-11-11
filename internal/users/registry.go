// Package users keeps who can log into dwnvr besides the owner:
// usuarios.json, each person's password and role.
//
// The owner stays in dwnvr.yaml (server.username and server.password), always
// an admin, and is the way back if everything goes wrong. Here is only what the
// screen writes.
//
// Validation messages, the file warnings and the errors the API shows are in
// pt-BR, because they reach the screen; they move to the catalogs with i18n.
package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Role says what the person can do. The admin does everything; the viewer
// sees the cameras, the recordings and the detections, and doesn't touch user
// management nor see the server diagnostics.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

func (p Role) valid() bool { return p == RoleAdmin || p == RoleViewer }

// FileData is usuarios.json.
type FileData struct {
	// Owner keeps what dwnvr.yaml doesn't have about the owner. Their password
	// stays in the yaml.
	Owner Owner  `json:"owner"`
	Users []User `json:"users"`
}

type Owner struct {
	// Display name. Empty means "use the username from dwnvr.yaml".
	Name string `json:"name,omitempty"`
	// Avatar is the id of the photo in the avatares/ folder, or empty (see SetAvatar).
	Avatar string `json:"avatar,omitempty"`
}

type User struct {
	// Username is the login: short, fixed, chosen by the admin (see
	// ValidateUsername).
	Username string `json:"username"`
	// Name is the display name.
	Name string `json:"name"`
	// Email is where the invite link goes, or empty. Unique among people,
	// case-insensitive (see ValidateEmail).
	Email string `json:"email,omitempty"`
	Role  Role   `json:"role"`
	// PasswordHash is the stored value (see HashPassword), never the password.
	// Empty means "hasn't set it yet": nobody logs in with this user.
	PasswordHash string `json:"passwordHash,omitempty"`
	// Link is the open invite link, or nil. One per person: generating another
	// replaces this one.
	Link *Link `json:"invite,omitempty"`
	// Avatar is the id of the photo in the avatares/ folder, or empty (see SetAvatar).
	Avatar    string    `json:"avatar,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// ActiveSince is when the person set the password through the link, or
	// zero while they haven't. Changing one's own password doesn't touch it; a new
	// link zeroes it.
	ActiveSince time.Time `json:"activeSince,omitzero"`
}

// Link is what usuarios.json keeps of the invite link: the token's SHA-256,
// never the token. Whoever reads the file can't build the link.
type Link struct {
	TokenSha256 string    `json:"tokenSha256"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// Status says where the person stands: "active" (has a password),
// "invitePending" (can still set the password through the link) or
// "inviteExpired" (needs another link to log in).
func (u User) Status(now time.Time) string {
	switch {
	case u.Link != nil && now.Before(u.Link.ExpiresAt):
		return "invitePending"
	case u.Link == nil && u.PasswordHash != "":
		return "active"
	}
	return "inviteExpired"
}

// ValidationError is the error for what someone typed or asked for, with the
// message to be read on screen. The API returns it as 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// The fields of the person forms, with their name in the API JSON: they're
// the keys of FieldErrors.
const (
	FieldUsername = "username"
	FieldName     = "name"
	FieldEmail    = "email"
)

// fieldOrder is the order the fields show up on screen.
var fieldOrder = []string{FieldName, FieldUsername, FieldEmail}

// FieldErrors are a form's validation errors, field by field: the key is the
// field (FieldUsername, FieldName, FieldEmail) and the value, the message for
// the screen, under it. Empty means "all good". As an error, the API returns it
// as 400, with the messages per field.
type FieldErrors map[string]string

// Error is the first one, in screen order, for whoever shows just one.
func (r FieldErrors) Error() string {
	for _, field := range fieldOrder {
		if m, ok := r[field]; ok {
			return m
		}
	}
	for _, m := range r {
		return m // field not in fieldOrder
	}
	return ""
}

var (
	// ErrNotFound is a person who isn't registered.
	ErrNotFound = errors.New("usuário não encontrado")
	// ErrInvalidLink is a link that expired, was already used, was replaced by
	// another or was made up: for whoever opens it, the way out is the same, ask
	// for another.
	ErrInvalidLink = errors.New("link vencido ou já usado: peça outro ao administrador")
)

var usernameFormat = regexp.MustCompile(`^[a-z0-9._-]+$`)

// ValidateUsername checks the login: lowercase letters, digits, ".", "_" and
// "-", up to MaxUsernameLen characters.
func ValidateUsername(u string) error {
	if u == "" {
		return ValidationError("usuário vazio")
	}
	if len(u) > MaxUsernameLen {
		return ValidationError(fmt.Sprintf("usuário com mais de %d caracteres", MaxUsernameLen))
	}
	if !usernameFormat.MatchString(u) {
		return ValidationError("usuário só aceita letras minúsculas, números, ponto, _ e -")
	}
	return nil
}

// ValidateName checks the display name and returns it without surrounding spaces.
func ValidateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", ValidationError("nome vazio")
	case utf8.RuneCountInString(name) > MaxNameLen:
		return "", ValidationError(fmt.Sprintf("nome com mais de %d caracteres", MaxNameLen))
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", ValidationError("nome com caractere de controle")
	}
	return name, nil
}

// ValidateEmail checks the person's e-mail: a single address, without a name
// in front, like "fulano@exemplo.com". Empty is valid: a person without
// e-mail.
func ValidateEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	switch {
	case email == "":
		return "", nil
	case len(email) > MaxEmailLen:
		return "", ValidationError(fmt.Sprintf("e-mail com mais de %d caracteres", MaxEmailLen))
	}
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return "", ValidationError("e-mail inválido: escreva só o endereço, como fulano@exemplo.com")
	}
	return email, nil
}

// ValidEmail is the person's e-mail, if it passes ValidateEmail, or empty: one
// written wrong by hand in the file counts as no e-mail, and isn't removed from
// it.
func (u User) ValidEmail() string {
	if e, err := ValidateEmail(u.Email); err == nil && e == u.Email {
		return e
	}
	return ""
}

// emailInUse says whether another person, other than except, already has the
// e-mail. It counts every entry in the file, even an ignored one: it may become
// valid again when someone fixes it.
func emailInUse(a *FileData, email, except string) bool {
	return slices.ContainsFunc(a.Users, func(u User) bool {
		return u.Username != except && strings.EqualFold(u.Email, email)
	})
}

// ValidatePassword only checks the length, in characters: no composition
// rule, as NIST SP 800-63B asks.
func ValidatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLen:
		return ValidationError(fmt.Sprintf("a senha precisa de pelo menos %d caracteres", MinPasswordLen))
	case n > MaxPasswordLen:
		return ValidationError(fmt.Sprintf("a senha passa de %d caracteres", MaxPasswordLen))
	}
	return nil
}

// Registry is usuarios.json loaded. It's read once, at boot, and each change
// writes the whole file and swaps the in-memory copy.
type Registry struct {
	path string
	// avatarDir is the photos folder, next to usuarios.json.
	avatarDir string
	// owner is server.username from dwnvr.yaml: nobody in the file may have the
	// same login.
	owner string

	mu   sync.RWMutex
	data FileData
	// unreadable keeps why the file couldn't be read. With it, the registry
	// stays empty and Update refuses: writing over it would erase whoever is in the
	// file.
	unreadable error
	// warnings are the problems of the file entries (see warningsOf), redone on
	// every change: fixing it on screen removes the warning without a restart.
	warnings []string

	// now is the links' clock. Tests swap it to expire a link without
	// waiting.
	now func() time.Time
}

// Open reads usuarios.json. A file that doesn't exist is an empty registry.
//
// An invalid entry doesn't block boot nor vanish from the file: it's left out
// of Find and becomes a warning, for the log and for the Diagnostics warnings
// (see Warnings). An unreadable file doesn't block boot either, because
// recording must not stop because of it: the error comes back, and the
// registry comes empty and refuses to write. Only the owner logs in until the
// file is fixed.
func Open(path, owner string) (c *Registry, warnings []string, err error) {
	c = &Registry{path: path, avatarDir: filepath.Join(filepath.Dir(path), "avatares"),
		owner: owner, now: time.Now}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &c.data)
	}
	if err != nil {
		c.data, c.unreadable = FileData{}, fmt.Errorf("%s ilegível: %w", path, err)
		return c, nil, c.unreadable
	}

	c.warnings = c.warningsOf(&c.data)
	return c, slices.Clone(c.warnings), nil
}

// warningsOf lists what isn't valid in the file entries, which only editing
// by hand can write: an ignored entry, an unreadable e-mail and a repeated
// one.
func (c *Registry) warningsOf(data *FileData) (warnings []string) {
	seen := map[string]bool{}
	var withEmail []User // the ones with a valid e-mail so far, without repeats
	for i, u := range data.Users {
		if reason := c.ignoreReason(u, seen); reason != "" {
			warnings = append(warnings, fmt.Sprintf("usuário #%d (%q) ignorado: %s", i, u.Username, reason))
		}
		seen[u.Username] = true
		if u.Email == "" {
			continue
		}
		if u.ValidEmail() == "" {
			warnings = append(warnings, fmt.Sprintf("usuário %q: e-mail %q ignorado, a pessoa fica sem e-mail", u.Username, u.Email))
			continue
		}
		// EqualFold, as in FindByEmail and emailInUse: with ToLower, a pair only
		// EqualFold matches ("s" and "ſ") wouldn't log in by e-mail and wouldn't get a
		// warning.
		i := slices.IndexFunc(withEmail, func(o User) bool { return strings.EqualFold(o.Email, u.Email) })
		if i >= 0 {
			warnings = append(warnings, fmt.Sprintf("usuário %q: o e-mail %q é o mesmo de %q, e nenhum dos dois entra pelo e-mail",
				u.Username, u.Email, withEmail[i].Username))
			continue
		}
		withEmail = append(withEmail, u)
	}
	return warnings
}

// Warnings lists what isn't valid in the file entries now (see warningsOf).
func (c *Registry) Warnings() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.warnings)
}

// Unreadable says why the file couldn't be read at boot, or nil.
func (c *Registry) Unreadable() error {
	return c.unreadable // only Open writes it
}

// ignoreReason says why the entry isn't valid, or "" if it is. seen are the
// logins of the previous entries: on a repeat, the first one wins.
func (c *Registry) ignoreReason(u User, seen map[string]bool) string {
	switch {
	case ValidateUsername(u.Username) != nil:
		return ValidateUsername(u.Username).Error()
	case u.Username == c.owner:
		return "é o mesmo usuário do dono, no dwnvr.yaml"
	case seen[u.Username]:
		return "repetido; vale a primeira entrada"
	case !u.Role.valid():
		return fmt.Sprintf("papel %q desconhecido", u.Role)
	}
	return ""
}

// Find looks the person up by login. The owner isn't here: they come from
// dwnvr.yaml.
func (c *Registry) Find(username string) (User, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if i := c.indexOf(&c.data, username); i >= 0 {
		return c.data.Users[i], true
	}
	return User{}, false
}

// FindByEmail looks the person up by e-mail, case-insensitively. A repeated
// e-mail in the file finds nobody, because it doesn't say who is logging in; it
// counts every entry, even an ignored one, like emailInUse. An ignored entry
// isn't found, as in Find.
func (c *Registry) FindByEmail(email string) (User, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	found := -1
	for i, u := range c.data.Users {
		if e := u.ValidEmail(); e == "" || !strings.EqualFold(e, email) {
			continue
		}
		if found >= 0 {
			return User{}, false
		}
		found = i
	}
	if found < 0 || c.indexOf(&c.data, c.data.Users[found].Username) != found {
		return User{}, false
	}
	return c.data.Users[found], true
}

// indexOf finds the person's valid entry in the file, or -1.
func (c *Registry) indexOf(data *FileData, username string) int {
	seen := map[string]bool{}
	for i, u := range data.Users {
		if c.ignoreReason(u, seen) == "" && u.Username == username {
			return i
		}
		seen[u.Username] = true
	}
	return -1
}

// List returns the valid people, in file order. An invalid entry is left
// out, as in Find.
func (c *Registry) List() []User {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := []User{}
	seen := map[string]bool{}
	for _, u := range c.data.Users {
		if c.ignoreReason(u, seen) == "" {
			list = append(list, u)
		}
		seen[u.Username] = true
	}
	return list
}

// Owner returns what the file keeps about the owner.
func (c *Registry) Owner() Owner {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data.Owner
}

// Count says how many entries the file has, valid or not.
func (c *Registry) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data.Users)
}

// Update applies f to a copy of the file, writes it and only then swaps the
// in-memory copy: if f or the write fail, nothing changes. One change at a
// time, so two tabs don't lose each other's change.
func (c *Registry) Update(f func(*FileData) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unreadable != nil {
		return fmt.Errorf("o cadastro não é gravado enquanto o arquivo estiver ilegível: %w", c.unreadable)
	}

	updated := FileData{Owner: c.data.Owner, Users: append([]User(nil), c.data.Users...)}
	// Link is a pointer: without the copy, f would touch the in-memory copy's
	// before the write succeeded.
	for i, u := range updated.Users {
		if u.Link != nil {
			l := *u.Link
			updated.Users[i].Link = &l
		}
	}
	if err := f(&updated); err != nil {
		return err
	}
	if updated.Users == nil {
		updated.Users = []User{} // "users": [], not null
	}
	b, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return err
	}
	// 0600, like .session-secret: the file has the stored passwords.
	if err := config.WriteAtomic(c.path, append(b, '\n'), 0o600); err != nil {
		return err
	}
	c.data = updated
	c.warnings = c.warningsOf(&c.data)
	return nil
}
