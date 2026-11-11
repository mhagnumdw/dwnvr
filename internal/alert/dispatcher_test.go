package alert

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/ratelimit"
)

func silent() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// alerts is an alert from the camera with a person at the Portão, at the given instant.
func alertOf(atMs int64) Alert {
	return Alert{Camera: "cam_a", CameraName: "Frente", AtMs: atMs, Frame: []byte("jpeg"),
		Objects: []Object{{Family: "pessoa", Class: "person", Score: 0.87, Areas: []string{"Portão"}}}}
}

// TestRecordWithoutNotifier: without a notifier, the alert is counted and
// goes to the log, and nothing else is created.
func TestRecordWithoutNotifier(t *testing.T) {
	var log strings.Builder
	c := NewDispatcher(nil, slog.New(slog.NewTextHandler(&log, nil)))
	if c.queue != nil {
		t.Fatal("the queue was created without a notifier")
	}

	c.Receive(alertOf(1))
	c.Receive(alertOf(2))
	if !strings.Contains(log.String(), "msg=alert cam=cam_a families=pessoa objects=1 areas=Portão") {
		t.Errorf("log: %s", log.String())
	}
	if c.Total() != 2 {
		t.Errorf("total %d, want 2", c.Total())
	}
	// Run without a notifier returns right away, and no goroutine is left behind.
	c.Run(context.Background())
}

// fakeNotifier is a Notifier that obeys the test: it returns the current
// error, notes each send's deadline and signals that the alert arrived.
type fakeNotifier struct {
	mu       sync.Mutex
	err      error
	timeouts []time.Duration
	arrived  chan Alert
}

func (m *fakeNotifier) Name() string { return "teste" }

func (m *fakeNotifier) Send(ctx context.Context, a Alert) error {
	if end, ok := ctx.Deadline(); ok {
		m.mu.Lock()
		m.timeouts = append(m.timeouts, time.Until(end))
		m.mu.Unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.arrived != nil {
		m.arrived <- a
	}
	return m.err
}

// TestSlowNotifierDoesNotHoldTheReceiver: with the notifier stopped, the queue
// fills up and what doesn't fit is dropped and counted, without Receive
// waiting. The record keeps all of them.
func TestSlowNotifierDoesNotHoldTheReceiver(t *testing.T) {
	c := NewDispatcher([]Notifier{&fakeNotifier{}}, silent())
	c.rateCap = fakeCap{pass: true} // only the queue can drop
	// Without Run, nobody takes from the queue: it's the slowest notifier possible.
	start := time.Now()
	for i := range QueuedAlerts + 5 {
		c.Receive(alertOf(int64(i)))
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("Receive took %v", d)
	}
	d, ok := c.Drops()
	if !ok || d.QueueFull != 5 || d.OverCap != 0 || c.Total() != QueuedAlerts+5 {
		t.Errorf("drop %+v and %d in total, want 5 on queue full and %d", d, c.Total(), QueuedAlerts+5)
	}
}

// TestShutdownWithQueuedAlerts: ctx ending with queued alerts leaves a log
// line with how many were left, and none of them goes out through the
// notifiers, not even one select picks after the end.
func TestShutdownWithQueuedAlerts(t *testing.T) {
	var log strings.Builder
	notifier := &fakeNotifier{arrived: make(chan Alert, 3)}
	c := NewDispatcher([]Notifier{notifier}, slog.New(slog.NewTextHandler(&log, nil)))
	for i := range 3 {
		c.Receive(alertOf(int64(i)))
	}
	ctx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	c.Run(ctx)
	if n := len(notifier.arrived); n != 0 {
		t.Errorf("%d alerts went out after the end", n)
	}
	if !strings.Contains(log.String(), "queued alerts") || !strings.Contains(log.String(), "alerts=3") {
		t.Errorf("log: %s", log.String())
	}
}

// fakeCap is a ratelimit.Limiter that lets everything through, or nothing.
type fakeCap struct{ pass bool }

func (t fakeCap) Allow(string) ratelimit.Result { return ratelimit.Result{Allowed: t.pass} }

// TestCap: the real cap lets the whole burst through, and the next one is
// dropped and counted, without taking a place in the queue.
func TestCap(t *testing.T) {
	c := NewDispatcher([]Notifier{&fakeNotifier{}}, silent())
	for i := range AlertBurst + 1 {
		c.Receive(alertOf(int64(i)))
	}
	d, ok := c.Drops()
	if !ok || d.OverCap != 1 || d.QueueFull != 0 || len(c.queue) != AlertBurst {
		t.Errorf("drop %+v and %d in the queue, want 1 over the cap and %d", d, len(c.queue), AlertBurst)
	}
	if c.Total() != AlertBurst+1 {
		t.Errorf("total %d: the dropped one also counts in the record", c.Total())
	}
}

// TestDropEpisode: drops add up in the same episode while the last one is
// younger than DropWarningLasts; after that the episode goes away, and the next
// drop starts another, from zero. Only the first of each one goes to the
// log.
func TestDropEpisode(t *testing.T) {
	var log strings.Builder
	c := NewDispatcher([]Notifier{&fakeNotifier{}}, slog.New(slog.NewTextHandler(&log, nil)))
	c.rateCap = fakeCap{pass: false}
	t0 := time.Date(2026, 10, 27, 14, 2, 0, 0, time.Local)
	now := t0
	c.now = func() time.Time { return now }

	c.Receive(alertOf(1))
	now = t0.Add(30 * time.Minute)
	c.Receive(alertOf(2))
	now = now.Add(DropWarningLasts)
	d, ok := c.Drops()
	if !ok || d.OverCap != 2 || !d.Since.Equal(t0) || !d.Last.Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("drop %+v (%v), want 2 since %v", d, ok, t0)
	}
	if n := strings.Count(log.String(), "alerts stopped going out"); n != 1 {
		t.Errorf("%d log lines, want 1: %s", n, log.String())
	}

	now = now.Add(time.Second)
	if d, ok := c.Drops(); ok {
		t.Fatalf("the episode went on after DropWarningLasts: %+v", d)
	}
	c.Receive(alertOf(3))
	if d, _ := c.Drops(); d.OverCap != 1 || !d.Since.Equal(now) {
		t.Errorf("the new episode didn't start from zero: %+v", d)
	}
	if n := strings.Count(log.String(), "alerts stopped going out"); n != 2 {
		t.Errorf("%d log lines, want 2", n)
	}
}

// TestFailingNotifier: the failure is counted, with the "since when" and the
// error, and the first successful send clears them. Each send goes with
// SendTimeout.
func TestFailingNotifier(t *testing.T) {
	notifier := &fakeNotifier{err: errors.New("senha recusada"), arrived: make(chan Alert, 4)}
	c := NewDispatcher([]Notifier{notifier}, silent())
	ctx := t.Context()
	go c.Run(ctx)

	queue := func() {
		t.Helper()
		select {
		case <-notifier.arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("the notifier didn't get the alert")
		}
	}
	// The state is written right after Send returns: wait a bit for it.
	state := func(cond func(NotifierState) bool) NotifierState {
		t.Helper()
		for range 500 {
			if e := c.NotifierStates()[0]; cond(e) {
				return e
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("state %+v", c.NotifierStates()[0])
		return NotifierState{}
	}

	c.Receive(alertOf(1))
	queue()
	e := state(func(e NotifierState) bool { return e.Failures == 1 })
	since := e.FailingSince
	if since.IsZero() || !e.Last.Equal(since) || e.Err != "senha recusada" || e.Name != "teste" {
		t.Errorf("state %+v", e)
	}

	c.Receive(alertOf(2))
	queue()
	e = state(func(e NotifierState) bool { return e.Failures == 2 })
	if !e.FailingSince.Equal(since) {
		t.Errorf("failing since %v, want the first failure's, %v", e.FailingSince, since)
	}
	if !e.Last.After(since) {
		t.Errorf("last send %v, want after the first failure, %v", e.Last, since)
	}

	notifier.mu.Lock()
	notifier.err = nil
	notifier.mu.Unlock()
	c.Receive(alertOf(3))
	queue()
	e = state(func(e NotifierState) bool { return e.Sent == 1 })
	if !e.FailingSince.IsZero() || e.Err != "" || e.Failures != 2 || e.Last.IsZero() {
		t.Errorf("after the successful send: %+v", e)
	}

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	for _, p := range notifier.timeouts {
		if p <= 0 || p > SendTimeout {
			t.Errorf("send deadline %v, want up to %v", p, SendTimeout)
		}
	}
	if len(notifier.timeouts) != 3 {
		t.Errorf("%d sends with a deadline, want 3", len(notifier.timeouts))
	}
}
