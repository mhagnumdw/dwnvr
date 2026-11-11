// Package ratelimit counts each client's requests and says when it has gone
// over the allowed rate.
//
// Login uses it (see internal/api/auth.go), and so does the alert cap, with a
// single key for the whole installation (see internal/alert). A limit on
// other routes would use the same Limiter, with a different rate and burst.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter decides whether a client's next request goes through.
type Limiter interface {
	// Allow records a request from the client identified by key and says
	// whether it goes through. A denied request doesn't count: retrying during
	// the wait doesn't make it longer.
	Allow(key string) Result
}

// Result is the decision about one request.
type Result struct {
	Allowed bool

	// RetryAfter is how long until the next request goes through. Only
	// meaningful when Allowed is false.
	RetryAfter time.Duration

	// FirstDenial marks the first denial since the last allowed request: it is
	// the start of an episode, and only that gets logged. Without it, a client
	// that keeps retrying would produce one log line per attempt.
	FirstDenial bool
}

// GCRA is the Limiter implemented with the Generic Cell Rate Algorithm
// (https://en.wikipedia.org/wiki/Generic_cell_rate_algorithm).
//
// Each client keeps a single instant, the TAT (theoretical arrival time): when
// the next request would arrive if the client moved exactly at the rate. Every
// allowed request pushes the TAT one interval forward. A request goes through
// as long as the TAT is not ahead by more than the tolerance, which is what
// lets the burst through.
//
// Same result as a token bucket, but storing one instant per client instead
// of a counter and an instant. And a TAT in the past is the same as a new
// client, which makes the sweep trivial.
type GCRA struct {
	interval  time.Duration // between two requests at the rate
	tolerance time.Duration // how far ahead the TAT may get: (burst - 1) intervals
	now       func() time.Time

	mu        sync.Mutex
	clients   map[string]*client
	lastSweep time.Time
}

type client struct {
	tat     time.Time
	denying bool
}

// NewGCRA allows perMinute requests per minute from each client, with up to
// burst of them in a row. perMinute and burst must be positive.
func NewGCRA(perMinute, burst int) *GCRA {
	return NewGCRAEvery(time.Minute/time.Duration(perMinute), burst)
}

// NewGCRAEvery allows one request from each client per interval, with up to
// burst of them in a row: the rates that "N per minute" can't express, such as
// 15 per hour. interval and burst must be positive.
func NewGCRAEvery(interval time.Duration, burst int) *GCRA {
	return newGCRA(interval, burst, time.Now)
}

// newGCRA is NewGCRAEvery with a swappable clock, for tests.
func newGCRA(interval time.Duration, burst int, now func() time.Time) *GCRA {
	return &GCRA{
		interval:  interval,
		tolerance: time.Duration(burst-1) * interval,
		now:       now,
		clients:   map[string]*client{},
		lastSweep: now(),
	}
}

// Allow is Limiter.Allow for the GCRA: the request goes through if the key's
// TAT is not ahead by more than the tolerance, and then pushes it one interval.
func (g *GCRA) Allow(key string) Result {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	g.sweep(now)

	c, ok := g.clients[key]
	if !ok {
		c = &client{tat: now}
		g.clients[key] = c
	}
	tat := c.tat
	if tat.Before(now) {
		tat = now
	}

	if ahead := tat.Sub(now); ahead > g.tolerance {
		first := !c.denying
		c.denying = true
		return Result{RetryAfter: ahead - g.tolerance, FirstDenial: first}
	}
	c.tat = tat.Add(g.interval)
	c.denying = false
	return Result{Allowed: true}
}

// sweep drops, at most once every SweepEvery, the clients whose TAT is in the
// past: their burst is full again, as for someone who never tried. It runs
// inside Allow, with no goroutine of its own.
func (g *GCRA) sweep(now time.Time) {
	if now.Sub(g.lastSweep) < SweepEvery {
		return
	}
	g.lastSweep = now
	for k, c := range g.clients {
		if !c.tat.After(now) {
			delete(g.clients, k)
		}
	}
}
