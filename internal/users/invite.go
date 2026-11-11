package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strings"
	"time"
)

// Without SMTP, someone logging in for the first time comes in through a
// link: the admin creates the person, dwnvr generates the link, and the person
// sets their own password. The same link serves for "forgot my password" and
// "lost my phone": generating another clears the current password.

// Invite is the freshly generated link. The Token only exists here, in the
// answer to whoever generated it: the file keeps its hash.
type Invite struct {
	User  User
	Token string
}

// newLink draws the token and returns what goes into the file.
func (c *Registry) newLink() (token string, l *Link, err error) {
	b := make([]byte, TokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	expires := c.now().Add(InviteValidity).Truncate(time.Second)
	return token, &Link{TokenSha256: tokenHash(token), ExpiresAt: expires}, nil
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// newUserErrors is the rule for whoever is about to be created, field by
// field: Create's and CheckNew's, which is the same so the screen doesn't say
// "fine" to something Create rejects. It checks every field, so the screen
// shows each error under its own.
func (c *Registry) newUserErrors(a *FileData, username, name, email string) FieldErrors {
	r := FieldErrors{}
	switch {
	case ValidateUsername(username) != nil:
		r[FieldUsername] = ValidateUsername(username).Error()
	case username == c.owner:
		r[FieldUsername] = "este é o usuário do dono, no dwnvr.yaml"
	// Any entry with the same login, even one ignored as invalid: the new one
	// would come after it and be the ignored one.
	case slices.ContainsFunc(a.Users, func(o User) bool { return o.Username == username }):
		r[FieldUsername] = "já existe alguém com este usuário"
	}
	if _, err := ValidateName(name); err != nil {
		r[FieldName] = err.Error()
	}
	// Nobody to except: the new person has no record yet. Excepting the
	// proposed login would let through the e-mail of whoever already has that
	// login.
	if m := emailRejection(a, email, ""); m != "" {
		r[FieldEmail] = m
	}
	return r
}

// emailRejection is the rule for the e-mail of the person except, which is
// or will be theirs: the rejection message, or "" if the e-mail is valid.
// Empty is valid.
func emailRejection(a *FileData, email, except string) string {
	email, err := ValidateEmail(email)
	switch {
	case err != nil:
		return err.Error()
	case email != "" && emailInUse(a, email, except):
		return "já existe alguém com este e-mail"
	}
	return ""
}

// CheckNew says what Create would reject with this data, without writing
// anything: it's the screen's check while the admin fills in the form. Empty
// means "all good". With the file unreadable the error comes back, as in
// Update: the in-memory registry is empty, and "all good" would be a lie.
func (c *Registry) CheckNew(username, name, email string) (FieldErrors, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.unreadable != nil {
		return nil, c.unreadable
	}
	return c.newUserErrors(&c.data, username, name, email), nil
}

// CheckEmail says what SetEmail would reject with this e-mail, without
// writing anything. ErrNotFound if the person isn't registered; with the file
// unreadable, its error, as in CheckNew.
func (c *Registry) CheckEmail(username, email string) (FieldErrors, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.unreadable != nil {
		return nil, c.unreadable
	}
	if c.indexOf(&c.data, username) < 0 {
		return nil, ErrNotFound
	}
	r := FieldErrors{}
	if m := emailRejection(&c.data, email, username); m != "" {
		r[FieldEmail] = m
	}
	return r, nil
}

// Create registers a viewer, still without a password, and returns the link
// for them to set theirs. The e-mail is optional: empty, the link only goes
// through the screen. A rejection comes as FieldErrors, field by field.
func (c *Registry) Create(username, name, email string) (Invite, error) {
	// What ValidateName and ValidateEmail return when they accept.
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	token, link, err := c.newLink()
	if err != nil {
		return Invite{}, err
	}
	u := User{Username: username, Name: name, Email: email, Role: RoleViewer, Link: link,
		CreatedAt: c.now().Truncate(time.Second)}

	err = c.Update(func(a *FileData) error {
		if r := c.newUserErrors(a, username, name, email); len(r) > 0 {
			return r
		}
		a.Users = append(a.Users, u)
		return nil
	})
	if err != nil {
		return Invite{}, err
	}
	return Invite{User: u, Token: token}, nil
}

// SetEmail changes the person's e-mail, or clears it with an empty email. It
// doesn't touch the link or the password. A rejection comes as FieldErrors, on
// FieldEmail.
func (c *Registry) SetEmail(username, email string) (User, error) {
	email = strings.TrimSpace(email) // what ValidateEmail returns when it accepts
	var u User
	err := c.Update(func(a *FileData) error {
		i := c.indexOf(a, username)
		if i < 0 {
			return ErrNotFound
		}
		if m := emailRejection(a, email, username); m != "" {
			return FieldErrors{FieldEmail: m}
		}
		a.Users[i].Email = email
		u = a.Users[i]
		return nil
	})
	return u, err
}

// NewLink generates another link for the person. Their password stops
// working right away, and so does the previous link: only the new one sets the
// password. Their sessions drop because the cookie key comes from the stored
// password (see api.sessionKey).
func (c *Registry) NewLink(username string) (Invite, error) {
	token, link, err := c.newLink()
	if err != nil {
		return Invite{}, err
	}
	var u User
	err = c.Update(func(a *FileData) error {
		i := c.indexOf(a, username)
		if i < 0 {
			return ErrNotFound
		}
		a.Users[i].PasswordHash = ""
		a.Users[i].ActiveSince = time.Time{}
		a.Users[i].Link = link
		u = a.Users[i]
		return nil
	})
	if err != nil {
		return Invite{}, err
	}
	return Invite{User: u, Token: token}, nil
}

// Remove deletes the person from the registry, and their photo along with it.
func (c *Registry) Remove(username string) error {
	var avatar string
	err := c.Update(func(a *FileData) error {
		i := c.indexOf(a, username)
		if i < 0 {
			return ErrNotFound
		}
		avatar = a.Users[i].Avatar
		a.Users = slices.Delete(a.Users, i, i+1)
		return nil
	})
	if err == nil {
		c.deleteAvatar(avatar)
	}
	return err
}

// byToken finds the link's person, if it's still valid.
func (c *Registry) byToken(a *FileData, token string) int {
	if token == "" {
		return -1
	}
	want := []byte(tokenHash(token))
	now := c.now()
	seen := map[string]bool{}
	for i, u := range a.Users {
		valid := c.ignoreReason(u, seen) == ""
		seen[u.Username] = true
		if !valid || u.Link == nil || !now.Before(u.Link.ExpiresAt) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(u.Link.TokenSha256), want) == 1 {
			return i
		}
	}
	return -1
}

// CheckInvite says whose link it is, if it's still valid.
func (c *Registry) CheckInvite(token string) (User, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i := c.byToken(&c.data, token)
	if i < 0 {
		return User{}, ErrInvalidLink
	}
	return c.data.Users[i], nil
}

// SetPassword writes the password of the link's person and deletes the
// link: it's single-use. The password hash waits its turn in the queue (see
// HashPassword) and is done outside the registry lock; the link is checked
// again at write time, and if it expired or was used in the meantime, nothing
// changes.
func (c *Registry) SetPassword(ctx context.Context, token, password string) (User, error) {
	if _, err := c.CheckInvite(token); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	stored, err := HashPassword(ctx, password)
	if err != nil {
		return User{}, err
	}
	var u User
	err = c.Update(func(a *FileData) error {
		i := c.byToken(a, token)
		if i < 0 {
			return ErrInvalidLink
		}
		a.Users[i].PasswordHash = stored
		a.Users[i].ActiveSince = c.now().Truncate(time.Second)
		a.Users[i].Link = nil
		u = a.Users[i]
		return nil
	})
	return u, err
}
