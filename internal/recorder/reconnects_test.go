package recorder

import (
	"testing"
	"time"
)

// The window counts only recent reconnections, and the list doesn't keep old
// ones: with a bad link for days it would grow without end.
func TestReconnectsInWindow(t *testing.T) {
	t0 := time.Now()
	r := &Recorder{countingSince: t0}

	r.countReconnect(t0)
	r.countReconnect(t0.Add(time.Hour))
	now := t0.Add(ReconnectWindow + 30*time.Minute)
	r.countReconnect(now)

	if r.reconnects != 3 {
		t.Errorf("total: %d, want 3", r.reconnects)
	}
	if n := r.reconnectsInWindowLocked(now); n != 2 {
		t.Errorf("in window: %d, want 2 (the first already left)", n)
	}
	if len(r.recentReconnects) != 2 {
		t.Errorf("the list kept %d instants, want only the 2 in the window", len(r.recentReconnects))
	}
	if !r.lastReconnect.Equal(now) {
		t.Errorf("last: %v, want %v", r.lastReconnect, now)
	}
}

// Reset starts everything over from now: total, window, last and the "since".
func TestResetReconnects(t *testing.T) {
	t0 := time.Now()
	r := &Recorder{countingSince: t0}
	r.countReconnect(t0.Add(time.Minute))
	r.countReconnect(t0.Add(2 * time.Minute))

	wasReset := t0.Add(3 * time.Minute)
	r.resetReconnects(wasReset)

	if r.reconnects != 0 || r.reconnectsInWindowLocked(wasReset) != 0 || !r.lastReconnect.IsZero() {
		t.Errorf("count left over: total %d, window %d, last %v",
			r.reconnects, r.reconnectsInWindowLocked(wasReset), r.lastReconnect)
	}
	if !r.countingSince.Equal(wasReset) {
		t.Errorf("counting since %v, want %v", r.countingSince, wasReset)
	}
}

// The "disconnected since" is the instant of the drop. The attempts that fail
// after it can't push it forward, or the screen would always say "5s ago".
func TestDisconnectedSinceMarksOnlyTheTransition(t *testing.T) {
	r := &Recorder{}

	r.setConnected(false, "refused")
	first := r.disconnectedAt
	if first.IsZero() {
		t.Fatal("the first failure, before connecting, should mark")
	}
	time.Sleep(time.Millisecond)
	r.setConnected(false, "refused again")
	if !r.disconnectedAt.Equal(first) {
		t.Errorf("the second failure moved the since: %v -> %v", first, r.disconnectedAt)
	}

	r.setConnected(true, "")
	if !r.disconnectedAt.IsZero() {
		t.Errorf("connected, but disconnectedAt stayed %v", r.disconnectedAt)
	}

	time.Sleep(time.Millisecond)
	r.setConnected(false, "dropped")
	if !r.disconnectedAt.After(first) {
		t.Errorf("the new drop should mark a new since, stayed %v", r.disconnectedAt)
	}
}
