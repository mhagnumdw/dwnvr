package users

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// accessLogAt opens the directory's acessos.json, with the clock at *now.
func accessLogAt(t *testing.T, dir string, now *time.Time) *AccessLog {
	t.Helper()
	a, err := OpenAccessLog(filepath.Join(dir, "acessos.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return *now }
	// Tests write through FlushPending; the timer must not get ahead.
	a.flushDelay = time.Hour
	return a
}

func TestLastAccessMovesInSteps(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 8, 12, 0, 0, time.Local)
	now := t0
	a := accessLogAt(t, t.TempDir(), &now)

	if got := a.Last("maria"); !got.IsZero() {
		t.Fatalf("no record, got %v", got)
	}
	a.Record("maria")

	for _, tc := range []struct {
		name  string
		after time.Duration
		want  time.Time
	}{
		{"less than a step doesn't move", 30 * time.Second, t0},
		{"a step later, it moves", LastAccessStep, t0.Add(LastAccessStep)},
		// The clock fixed backwards rewrites: otherwise the access would be in the
		// future until the clock caught up.
		{"clock going back, rewrites", -time.Hour, t0.Add(-time.Hour)},
	} {
		now = t0.Add(tc.after)
		a.Record("maria")
		if got := a.Last("maria"); !got.Equal(tc.want) {
			t.Errorf("%s: last access %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLastAccessSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 14, 8, 12, 31, 500e6, time.Local)
	a := accessLogAt(t, dir, &now)
	a.Record("maria")
	a.Record("admin")
	if err := a.FlushPending(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "acessos.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("acessos.json with mode %v, want 0600", info.Mode().Perm())
	}

	from := accessLogAt(t, dir, &now)
	want := now.Truncate(time.Second)
	for _, u := range []string{"maria", "admin"} {
		if got := from.Last(u); !got.Equal(want) {
			t.Errorf("%s after reopening: %v, want %v", u, got, want)
		}
	}
}

// Without a new access there's nothing to write: whoever doesn't use dwnvr,
// or uses it with authentication off, gets no file at all.
func TestNoAccessWritesNothing(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := accessLogAt(t, dir, &now)
	if err := a.FlushPending(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "acessos.json")); !os.IsNotExist(err) {
		t.Errorf("acessos.json created without any access (stat: %v)", err)
	}
}

// Record doesn't write: the timer does, after the delay.
func TestWriteIsLeftToTheTimer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acessos.json")
	now := time.Now()
	a := accessLogAt(t, dir, &now)
	a.flushDelay = 20 * time.Millisecond

	a.Record("maria")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Record wrote right away (stat: %v)", err)
	}
	timeout := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(timeout) {
			t.Fatal("the timer didn't write acessos.json")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A failed write schedules another attempt on its own: without that, it
// would wait for the next access, and without one it would be left for
// shutdown.
func TestFailedWriteRetries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config") // doesn't exist yet
	path := filepath.Join(dir, "acessos.json")
	now := time.Now()
	a := accessLogAt(t, dir, &now)

	a.Record("maria")
	a.flushDelay = 20 * time.Millisecond
	if err := a.FlushPending(); err == nil {
		t.Fatal("wrote to a directory that doesn't exist")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	timeout := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(timeout) {
			t.Fatal("after the failure, no attempt wrote acessos.json")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestForgetDeletesTheAccess(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := accessLogAt(t, dir, &now)
	a.Record("maria")
	if err := a.FlushPending(); err != nil {
		t.Fatal(err)
	}
	a.Forget("maria")
	if err := a.FlushPending(); err != nil {
		t.Fatal(err)
	}
	if got := accessLogAt(t, dir, &now).Last("maria"); !got.IsZero() {
		t.Errorf("after forgetting and reopening, got %v", got)
	}
}

// An unreadable file blocks nothing: the error comes back, the accesses
// start over empty, and the next write replaces it.
func TestUnreadableAccessLogStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acessos.json")
	if err := os.WriteFile(path, []byte("{não é json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := OpenAccessLog(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("unreadable file without an error")
	}
	a.Record("maria")
	if err := a.FlushPending(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if got := accessLogAt(t, dir, &now).Last("maria"); got.IsZero() {
		t.Error("the write didn't replace the unreadable file")
	}
}
