package ratelimit

import (
	"testing"
	"time"
)

// clock is a stopped clock that the test moves forward by hand.
type clock struct{ t time.Time }

// now is the instant the clock is stopped at.
func (r *clock) now() time.Time { return r.t }

// advance moves the clock forward by d.
func (r *clock) advance(d time.Duration) { r.t = r.t.Add(d) }

// newClock is a clock stopped at a fixed instant.
func newClock() *clock {
	return &clock{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
}

// mustAllow records a request for key k and fails the test if it is denied.
func mustAllow(t *testing.T, g *GCRA, k string) {
	t.Helper()
	if r := g.Allow(k); !r.Allowed {
		t.Fatalf("%s: denied, expected it to go through (retry after %s)", k, r.RetryAfter)
	}
}

// The whole burst goes through; the next one is denied with a wait of one
// interval, and after that exactly one goes through.
func TestBurstThenRate(t *testing.T) {
	clk := newClock()
	g := newGCRA(30*time.Second, 5, clk.now)

	for range 5 {
		mustAllow(t, g, "a")
	}
	r := g.Allow("a")
	if r.Allowed || r.RetryAfter != 30*time.Second || !r.FirstDenial {
		t.Fatalf("sixth: %+v, expected a 30s denial, the first one", r)
	}

	clk.advance(10 * time.Second)
	r = g.Allow("a")
	if r.Allowed || r.RetryAfter != 20*time.Second || r.FirstDenial {
		t.Fatalf("during the wait: %+v, expected a 20s denial, not the first", r)
	}

	clk.advance(20 * time.Second)
	mustAllow(t, g, "a")
	if r := g.Allow("a"); r.Allowed || r.RetryAfter != 30*time.Second || !r.FirstDenial {
		t.Fatalf("right after the allowed one: %+v, expected a new 30s denial", r)
	}
}

// Retrying during the wait doesn't make it longer: denied requests don't count.
func TestDenialDoesNotExtendWait(t *testing.T) {
	clk := newClock()
	g := newGCRA(30*time.Second, 1, clk.now)
	mustAllow(t, g, "a")
	for range 100 {
		g.Allow("a")
	}
	clk.advance(30 * time.Second)
	mustAllow(t, g, "a")
}

// An idle client gets one slot back per interval, up to the whole burst.
func TestSlotsComeBack(t *testing.T) {
	clk := newClock()
	g := newGCRA(30*time.Second, 5, clk.now)
	for range 5 {
		mustAllow(t, g, "a")
	}
	clk.advance(time.Minute) // two slots
	mustAllow(t, g, "a")
	mustAllow(t, g, "a")
	if r := g.Allow("a"); r.Allowed {
		t.Fatal("third one after 1 min went through; expected only two")
	}

	clk.advance(time.Hour)
	for range 5 {
		mustAllow(t, g, "a")
	}
	if r := g.Allow("a"); r.Allowed {
		t.Fatal("idle for a long time, the burst went over 5")
	}
}

// Each key has its own rate: one client over the limit doesn't block others.
func TestKeysAreIndependent(t *testing.T) {
	clk := newClock()
	g := newGCRA(30*time.Second, 2, clk.now)
	mustAllow(t, g, "a")
	mustAllow(t, g, "a")
	if g.Allow("a").Allowed {
		t.Fatal("a went over the burst")
	}
	mustAllow(t, g, "b")
	mustAllow(t, g, "b")
}

// The sweep drops clients that recovered their whole burst and keeps the ones
// still in debt.
func TestSweep(t *testing.T) {
	clk := newClock()
	g := newGCRA(time.Second, 5, clk.now)
	mustAllow(t, g, "idle")
	for range 5 {
		mustAllow(t, g, "owing")
	}

	clk.advance(SweepEvery - time.Second)
	for range 70 {
		g.Allow("owing") // pushes its TAT past the minute
	}
	if n := len(g.clients); n != 2 {
		t.Fatalf("before sweep time: %d clients, expected 2", n)
	}

	clk.advance(time.Second)
	g.Allow("owing")
	if _, ok := g.clients["idle"]; ok {
		t.Error("the sweep kept a client whose burst was already full")
	}
	if _, ok := g.clients["owing"]; !ok {
		t.Error("the sweep dropped a client still in debt")
	}
}
