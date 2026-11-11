package users

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHashedPasswordVerifies(t *testing.T) {
	ctx := context.Background()
	stored, err := HashPassword(ctx, "uma senha boa")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "$pbkdf2-sha256$i=600000$") {
		t.Errorf("unexpected format: %s", stored)
	}
	if strings.Contains(stored, "uma senha boa") {
		t.Error("the password was stored as it came")
	}

	for password, want := range map[string]bool{"uma senha boa": true, "uma senha boA": false, "": false} {
		ok, err := VerifyPassword(ctx, password, stored)
		if err != nil || ok != want {
			t.Errorf("password %q: ok=%v err=%v, want %v", password, ok, err, want)
		}
	}
}

// Without a stored password, the hash is computed all the same and the
// answer is no. It's what keeps the login's response time from telling whether
// the user exists.
func TestNoStoredPasswordPaysTheHashAndFails(t *testing.T) {
	ctx := context.Background()
	stored, err := HashPassword(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	measure := func(stored string) time.Duration {
		start := time.Now()
		if ok, _ := VerifyPassword(ctx, "x", stored); ok && stored == "" {
			t.Error("without a stored password, it logged in")
		}
		return time.Since(start)
	}
	withPassword, noPassword := measure(stored), measure("")
	if noPassword < withPassword/2 {
		t.Errorf("without a stored password it took %v, against %v with one: the time tells who exists", noPassword, withPassword)
	}
}

func TestMalformedStoredPassword(t *testing.T) {
	bads := []string{
		"senha-em-texto",
		"$argon2id$v=19$m=7168,t=5,p=1$c2FsdA$aGFzaA",
		"$pbkdf2-sha256$600000$c2FsdA$aGFzaA",
		"$pbkdf2-sha256$i=1000$" + b64.EncodeToString([]byte("salt-de-16-bytes")) + "$" + b64.EncodeToString(make([]byte, 32)),
		"$pbkdf2-sha256$i=600000$!!!$" + b64.EncodeToString(make([]byte, 32)),
		"$pbkdf2-sha256$i=600000$c2FsdA$curto",
	}
	for _, g := range bads {
		var err error
		if alg, errName := algorithmOf(g); errName != nil {
			err = errName
		} else {
			_, err = alg.Verify("x", g)
		}
		if err == nil {
			t.Errorf("%q was accepted", g)
		}
	}
}

// One hash at a time: the second waits for the first, and whoever gives up
// in the queue leaves without hashing.
func TestQueueOneHashAtATime(t *testing.T) {
	turn <- struct{}{} // someone hashing
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := VerifyPassword(ctx, "x", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("with the turn taken, want giving up on the timeout, got %v", err)
	}
	<-turn

	var mu sync.Mutex
	inside, maxLen := 0, 0
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			_ = inTurn(context.Background(), func() {
				mu.Lock()
				inside++
				maxLen = max(maxLen, inside)
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				inside--
				mu.Unlock()
			})
		})
	}
	wg.Wait()
	if maxLen != 1 {
		t.Errorf("%d hashes at the same time, want 1", maxLen)
	}
}
