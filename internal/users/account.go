package users

import "context"

// My account: what each person changes about themselves. The name and the
// photo apply to the owner too; their password stays in dwnvr.yaml.

// ChangePassword changes the password of someone who already has one,
// asking for the current one. The person's sessions drop, because the cookie
// key comes from the stored password (see api.chaveDaSessao): whoever changed
// it gets a new cookie, and the other devices go back to the login screen.
//
// It's two password hashes, one to verify the current and another to generate
// the new one, each waiting its turn in the queue (see HashPassword).
func (c *Registry) ChangePassword(ctx context.Context, username, current, newPassword string) (User, error) {
	u, ok := c.Find(username)
	if !ok || u.PasswordHash == "" {
		return User{}, ErrNotFound
	}
	if err := ValidatePassword(newPassword); err != nil {
		return User{}, err
	}
	matches, err := VerifyPassword(ctx, current, u.PasswordHash)
	if err != nil {
		return User{}, err
	}
	if !matches {
		return User{}, ValidationError("a senha atual não confere")
	}
	stored, err := HashPassword(ctx, newPassword)
	if err != nil {
		return User{}, err
	}
	err = c.Update(func(a *FileData) error {
		i := c.indexOf(a, username)
		if i < 0 {
			return ErrNotFound
		}
		// The admin generated a new link, or another device changed the password,
		// during the hash: the verified password is no longer the one that holds.
		if a.Users[i].PasswordHash != u.PasswordHash {
			return ValidationError("a senha mudou enquanto isso: entre de novo")
		}
		a.Users[i].PasswordHash = stored
		u = a.Users[i]
		return nil
	})
	return u, err
}

// Rename changes the display name of the person, or the owner's, and
// returns the name as it ended up.
func (c *Registry) Rename(username, name string) (string, error) {
	name, err := ValidateName(name)
	if err != nil {
		return "", err
	}
	return name, c.Update(func(a *FileData) error {
		if username == c.owner {
			a.Owner.Name = name
			return nil
		}
		i := c.indexOf(a, username)
		if i < 0 {
			return ErrNotFound
		}
		a.Users[i].Name = name
		return nil
	})
}
