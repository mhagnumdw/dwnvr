package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// AccessLog keeps when each person last used dwnvr: the last request with a
// valid session, not the last login. The session renews itself (see
// api.sessionRenew), so the login would say little.
//
// A request only touches memory. Disk gets acessos.json, written in the
// background, at most every AccessFlushDelay, and on shutdown. No request waits
// for the disk.
//
// It's in a separate file, not in usuarios.json, for two reasons. The Registry
// holds its lock while writing, and every authenticated request reads it:
// writing the access there every few minutes would stall every request on the
// fsync. And the file with the stored passwords doesn't need to be rewritten
// just because someone opened the live view.
type AccessLog struct {
	path string
	log  *slog.Logger

	mu   sync.Mutex
	last map[string]time.Time
	// pending says memory has what the file doesn't have yet.
	pending bool
	// timer is the scheduled write, or nil.
	timer *time.Timer

	// writing allows one write at a time: the scheduled one and the shutdown
	// one may overlap.
	writing sync.Mutex

	// now and flushDelay are the clock and AccessFlushDelay. Tests swap them so
	// they don't wait.
	now        func() time.Time
	flushDelay time.Duration
}

// accessFile is acessos.json.
type accessFile struct {
	// LastAccess maps the login to the instant.
	LastAccess map[string]time.Time `json:"lastAccess"`
}

// OpenAccessLog reads acessos.json. A file that doesn't exist is all empty.
//
// An unreadable file doesn't block boot: the error comes back, for the log, and
// the accesses start over empty. The next write replaces it, because what's
// lost is only each person's last access, which comes back the next time they
// use it.
func OpenAccessLog(path string, log *slog.Logger) (*AccessLog, error) {
	a := &AccessLog{path: path, log: log, last: map[string]time.Time{},
		now: time.Now, flushDelay: AccessFlushDelay}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	var data accessFile
	if err == nil {
		err = json.Unmarshal(b, &data)
	}
	if err != nil {
		return a, fmt.Errorf("unreadable %s: %w", path, err)
	}
	if data.LastAccess != nil {
		a.last = data.LastAccess
	}
	return a, nil
}

// Record notes that the person just used dwnvr. It's called on every
// authenticated request, so it only touches memory, and only when their last
// access is more than a LastAccessStep old.
func (a *AccessLog) Record(username string) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	// With the clock going backwards (NTP fixing a clock that ran ahead), the
	// difference comes out negative and the instant is rewritten: otherwise the
	// access would be stuck in the future until the clock caught up.
	if d := now.Sub(a.last[username]); d >= 0 && d < LastAccessStep {
		return
	}
	a.last[username] = now.Truncate(time.Second)
	a.schedule()
}

// Last returns the person's last access, or zero if there's no record.
func (a *AccessLog) Last(username string) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last[username]
}

// Forget deletes the person's record. It applies to whoever is removed and
// whoever is created: without it, a reused login would inherit another
// person's access.
func (a *AccessLog) Forget(username string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.last[username]; !ok {
		return
	}
	delete(a.last, username)
	a.schedule()
}

// schedule marks pending and schedules the write, if there isn't one yet.
// Called with mu held.
func (a *AccessLog) schedule() {
	a.pending = true
	if a.timer == nil {
		a.timer = time.AfterFunc(a.flushDelay, func() {
			if err := a.FlushPending(); err != nil {
				a.log.Error("gravando os últimos acessos", "arquivo", a.path, "erro", err)
			}
		})
	}
}

// FlushPending writes to acessos.json what memory has that's new, if
// anything. It runs on the timer and on shutdown. If the write fails, pending
// stays marked and another attempt is scheduled, without waiting for the next
// access: without an access, what failed would only reach the disk on shutdown,
// and a power cut would lose more than the delay.
func (a *AccessLog) FlushPending() error {
	a.writing.Lock()
	defer a.writing.Unlock()

	a.mu.Lock()
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	if !a.pending {
		a.mu.Unlock()
		return nil
	}
	snapshot := maps.Clone(a.last)
	a.pending = false
	a.mu.Unlock()

	b, err := json.MarshalIndent(accessFile{LastAccess: snapshot}, "", "  ")
	if err == nil {
		// 0600, like usuarios.json: it says when each person used dwnvr.
		err = config.WriteAtomic(a.path, append(b, '\n'), 0o600)
	}
	if err != nil {
		a.mu.Lock()
		a.schedule()
		a.mu.Unlock()
	}
	return err
}
