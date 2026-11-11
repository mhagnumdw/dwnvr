package alert

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/ratelimit"
)

// Dispatcher receives the alerts of every camera, counts them, logs them and
// hands them to the notifiers.
//
// The alert record is the log line and the `alert` mark on the object line,
// which the Recorder writes before the Dispatcher receives it. It exists with
// or without a configured notifier.
//
// Receive is called from the object detector queue's goroutine and never waits:
// what may wait is the send, on Run's goroutine. Without a notifier, there's
// no wait, no cap and no send goroutine.
type Dispatcher struct {
	log       *slog.Logger
	notifiers []Notifier
	now       func() time.Time

	// queue takes the alert to the send goroutine, and rateCap decides whether
	// it gets in. Nil without a notifier.
	queue   chan Alert
	rateCap ratelimit.Limiter

	total atomic.Int64

	mu     sync.Mutex
	states []NotifierState
	dropEp DropEpisode
}

// capKey is the cap's only key: it applies to the whole installation, not
// per camera, because it protects the e-mail account.
const capKey = "alertas"

// DropEpisode is an episode of alerts that didn't go out through the
// notifiers: it starts at the first drop and ends DropWarningLasts after the
// last. The warning about alerts that didn't go out comes from it. The alerts
// stay recorded on the detection.
type DropEpisode struct {
	// QueueFull are the ones that didn't fit in the queue, with sending slow or
	// stuck; OverCap, the ones that went over AlertsPerHour.
	QueueFull, OverCap int64
	Since, Last        time.Time
}

// dropReason says why the alert didn't go out, in the log.
type dropReason string

const (
	queueFull dropReason = "the send queue is full"
	overCap   dropReason = "over the alerts per hour cap"
)

// NotifierState is what's known about a notifier's sends since dwnvr
// started. The warning about a failing notifier comes from it.
type NotifierState struct {
	Name     string
	Sent     int64
	Failures int64
	// Last is the instant of the most recent attempt, successful or not: with
	// an empty Err, it succeeded. Zero before the first send.
	Last time.Time
	// FailingSince is the first failure since the last successful send, and
	// Err, the most recent failure's. Zeroed when a send succeeds.
	FailingSince time.Time
	Err          string
}

// NewDispatcher is born with the first camera that turns the alert on:
// whoever never turns it on pays nothing.
func NewDispatcher(notifiers []Notifier, log *slog.Logger) *Dispatcher {
	c := &Dispatcher{log: log, notifiers: notifiers, now: time.Now}
	if len(notifiers) > 0 {
		c.queue = make(chan Alert, QueuedAlerts)
		c.rateCap = ratelimit.NewGCRAEvery(time.Hour/AlertsPerHour, AlertBurst)
		c.states = make([]NotifierState, len(notifiers))
		for i, m := range notifiers {
			c.states[i].Name = m.Name()
		}
	}
	return c
}

// Receive counts the alert, logs it and puts it in the notifiers' queue.
// Over the cap, or with the queue full, the alert is dropped and counted: it's
// already recorded on the detection, and waiting here would stall the
// inferences of every camera.
func (c *Dispatcher) Receive(a Alert) {
	c.total.Add(1)
	attrs := []any{"cam", a.Camera, "families", familiesOf(a), "objects", len(a.Objects)}
	if areas := areasOf(a); len(areas) > 0 {
		attrs = append(attrs, "areas", strings.Join(areas, ","))
	}
	c.log.Info("alert", attrs...)

	if c.queue == nil {
		return
	}
	if !c.rateCap.Allow(capKey).Allowed {
		c.drop(overCap)
		return
	}
	select {
	case c.queue <- a:
	default:
		c.drop(queueFull)
	}
}

// drop counts the alert that didn't go out in the current episode, or starts
// another when the last drop is older than DropWarningLasts. Only the first of
// each episode goes to the log: in a burst, it would be one line per alert.
func (c *Dispatcher) drop(reason dropReason) {
	now := c.now()
	c.mu.Lock()
	d := &c.dropEp
	fresh := d.Last.IsZero() || now.Sub(d.Last) > DropWarningLasts
	if fresh {
		*d = DropEpisode{Since: now}
	}
	if reason == queueFull {
		d.QueueFull++
	} else {
		d.OverCap++
	}
	d.Last = now
	c.mu.Unlock()
	if fresh {
		c.log.Warn("alerts stopped going out through the notifiers", "reason", string(reason))
	}
}

// Run hands the queued alerts to the notifiers, one at a time, until ctx is
// done. Without a notifier, it returns right away.
func (c *Dispatcher) Run(ctx context.Context) {
	if c.queue == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			c.logLeftInQueue(0)
			return
		case a := <-c.queue:
			// With ctx and the queue ready together, select picks at random: an
			// alert taken after the end doesn't go out either.
			if ctx.Err() != nil {
				c.logLeftInQueue(1)
				return
			}
			c.send(ctx, c.withDrawnFrame(a))
		}
	}
}

// logLeftInQueue logs the alerts that shutdown left without going out
// through the notifiers: they're recorded in Detections, but without this line
// they would vanish without a trace. Draining the queue before leaving would
// delay shutdown.
func (c *Dispatcher) logLeftInQueue(taken int) {
	if n := len(c.queue) + taken; n > 0 {
		c.log.Warn("dwnvr shut down with queued alerts that didn't go out through the notifiers", "alerts", n)
	}
}

// withDrawnFrame swaps the inferred frame for the frame with the boxes and
// areas drawn, once per alert, for every notifier. If drawing fails, the frame
// goes as it came: the alert doesn't stop going out because of it.
func (c *Dispatcher) withDrawnFrame(a Alert) Alert {
	if len(a.Frame) == 0 {
		return a
	}
	frame, err := drawnFrame(a)
	if err != nil {
		c.log.Warn("the alert frame goes without drawing", "cam", a.Camera, "err", err)
		return a
	}
	a.Frame = frame
	return a
}

// send passes the alert to each notifier, each with its own SendTimeout: a
// stuck notifier doesn't eat the next one's deadline.
func (c *Dispatcher) send(ctx context.Context, a Alert) {
	for i, m := range c.notifiers {
		sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)
		err := m.Send(sendCtx, a)
		cancel()

		now := c.now()
		c.mu.Lock()
		e := &c.states[i]
		e.Last = now
		if err == nil {
			e.Sent++
			e.FailingSince, e.Err = time.Time{}, ""
		} else {
			e.Failures++
			if e.FailingSince.IsZero() {
				e.FailingSince = now
			}
			e.Err = err.Error()
		}
		c.mu.Unlock()
		if err != nil {
			c.log.Warn("sending the alert failed", "notifier", m.Name(), "cam", a.Camera, "err", err)
		}
	}
}

// Total is how many alerts the Dispatcher received since it was born. It's
// what /api/health carries.
func (c *Dispatcher) Total() int64 { return c.total.Load() }

// Drops returns the drop episode still in effect, and false when there's
// none: there never was, or the last drop is older than DropWarningLasts.
func (c *Dispatcher) Drops() (DropEpisode, bool) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.dropEp
	if d.Last.IsZero() || now.Sub(d.Last) > DropWarningLasts {
		return DropEpisode{}, false
	}
	return d, true
}

// NotifierStates returns each notifier's state, in the order they were built.
func (c *Dispatcher) NotifierStates() []NotifierState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]NotifierState(nil), c.states...)
}

// familiesOf joins the objects' families, without repeats, in the order they
// come: the Recorder's, which puts the highest priority first.
func familiesOf(a Alert) string {
	var fs []string
	for _, o := range a.Objects {
		if !slices.Contains(fs, o.Family) {
			fs = append(fs, o.Family)
		}
	}
	return strings.Join(fs, ",")
}

// areasOf are the areas the objects fell in, without repeats, in the order
// they appear: the log and the e-mail show them.
func areasOf(a Alert) []string {
	var as []string
	for _, o := range a.Objects {
		for _, name := range o.Areas {
			if !slices.Contains(as, name) {
				as = append(as, name)
			}
		}
	}
	return as
}
