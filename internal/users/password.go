package users

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Algorithm is the contract of a way to store passwords, what in Java would
// be an interface. The stored value follows the PHC string format, the same as
// `$argon2id$v=19$...` in other tools: `$<name>$<parameters>`, and the name
// says which Algorithm verifies it.
type Algorithm interface {
	// Hash returns the value to store for the password, already with the name
	// and the parameters, and with a new salt: two calls with the same password
	// give different values.
	Hash(password string) (string, error)

	// Verify says whether the password matches the stored value. An error is a
	// malformed value, not a wrong password.
	Verify(password, stored string) (bool, error)
}

// algorithms maps the name in the stored value to the implementation. A new
// algorithm (Argon2id, if golang.org/x/crypto ever comes into the project) is
// one line here; switching defaultAlgorithm applies to passwords set from then
// on, and the old ones keep being verified by their own name.
var algorithms = map[string]Algorithm{
	pbkdf2Name: pbkdf2SHA256{},
}

const defaultAlgorithm = pbkdf2Name

// turn is the queue of password hashes: one at a time in the whole process.
// Each one ties up a core for 0.77 s on the Orange Pi, and login doesn't limit
// attempts; without the queue, a few simultaneous attempts would take the
// cores used by recording and the object detector. With it, the worst case is
// one busy core and everyone else's login waiting their turn.
var turn = make(chan struct{}, 1)

// inTurn runs f when its turn comes. Whoever gives up before that (the
// browser closed the connection) leaves the queue without spending the hash.
func inTurn(ctx context.Context, f func()) error {
	select {
	case turn <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-turn }()
	f()
	return nil
}

// HashPassword returns the value to store for the password, with the
// default algorithm, waiting its turn in the queue.
func HashPassword(ctx context.Context, password string) (stored string, err error) {
	if errQueue := inTurn(ctx, func() {
		stored, err = algorithms[defaultAlgorithm].Hash(password)
	}); errQueue != nil {
		return "", errQueue
	}
	return stored, err
}

// VerifyPassword says whether the password matches the stored value, waiting
// its turn in the queue.
//
// An empty stored value means "nothing to compare with": the user doesn't
// exist, or hasn't set a password yet. The hash is computed all the same,
// against a value no password reaches, and the answer is false. That way the
// login's response time doesn't tell who exists. A value with an unknown
// algorithm also pays for the hash before becoming an error.
func VerifyPassword(ctx context.Context, password, stored string) (ok bool, err error) {
	alg, errName := algorithmOf(stored)
	against := stored
	if errName != nil {
		alg, against = algorithms[defaultAlgorithm], noPassword
	}
	if errQueue := inTurn(ctx, func() {
		ok, err = alg.Verify(password, against)
	}); errQueue != nil {
		return false, errQueue
	}
	switch {
	case stored == "":
		return false, nil
	case errName != nil:
		return false, errName
	}
	return ok, err
}

// algorithmOf finds, by the name at the start of the stored value, what verifies it.
func algorithmOf(stored string) (Algorithm, error) {
	partes := strings.SplitN(stored, "$", 3)
	if len(partes) < 3 || partes[0] != "" {
		return nil, errors.New("stored password outside the $<algorithm>$<parameters> format")
	}
	alg, ok := algorithms[partes[1]]
	if !ok {
		// Without quoting what's stored: these errors go to the log, and a piece
		// of the stored value has no business there.
		return nil, errors.New("stored password with an unknown algorithm")
	}
	return alg, nil
}

// --- PBKDF2-HMAC-SHA256 -----------------------------------------------------

const pbkdf2Name = "pbkdf2-sha256"

// pbkdf2SHA256 stores the password as
// `$pbkdf2-sha256$i=<iterations>$<salt>$<hash>`, with the salt and the hash
// in base64 without padding. It's the fourth on OWASP's list, picked because
// it's in the standard library: no new dependency (see
// docs/TODO/TODO_hash-da-senha.md).
type pbkdf2SHA256 struct{}

var b64 = base64.RawStdEncoding

// noPassword is the value VerifyPassword computes against when there's no
// stored password. It has the same cost as a real password, and the all-zero
// hash isn't reached by any password in practice.
var noPassword = fmt.Sprintf("$%s$i=%d$%s$%s", pbkdf2Name, PBKDF2Iterations,
	b64.EncodeToString(make([]byte, SaltBytes)), b64.EncodeToString(make([]byte, HashBytes)))

func (pbkdf2SHA256) Hash(password string) (string, error) {
	salt := make([]byte, SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, PBKDF2Iterations, HashBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$%s$i=%d$%s$%s", pbkdf2Name, PBKDF2Iterations,
		b64.EncodeToString(salt), b64.EncodeToString(hash)), nil
}

func (pbkdf2SHA256) Verify(password, stored string) (bool, error) {
	// "", "pbkdf2-sha256", "i=600000", salt, hash
	partes := strings.Split(stored, "$")
	if len(partes) != 5 || partes[0] != "" || partes[1] != pbkdf2Name {
		return false, errors.New("stored password outside the $pbkdf2-sha256$i=<n>$<salt>$<hash> format")
	}
	text, ok := strings.CutPrefix(partes[2], "i=")
	if !ok {
		return false, errors.New("stored password without the iterations (i=)")
	}
	iterations, err := strconv.Atoi(text)
	if err != nil || iterations < MinIterations || iterations > MaxIterations {
		return false, fmt.Errorf("stored password with iterations outside %d to %d",
			MinIterations, MaxIterations)
	}
	salt, err := b64.DecodeString(partes[3])
	if err != nil || len(salt) == 0 {
		return false, errors.New("stored password with an unreadable salt")
	}
	want, err := b64.DecodeString(partes[4])
	if err != nil || len(want) != HashBytes {
		return false, errors.New("stored password with an unreadable hash")
	}

	computed, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(computed, want) == 1, nil
}
