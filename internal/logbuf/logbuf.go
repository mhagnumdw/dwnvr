// Package logbuf keeps the latest log warnings and errors in memory, for the
// Diagnostics screen to show.
//
// It exists because `docker logs` is the first step of any investigation and
// also what the person who installed dwnvr is least likely to know how to do.
// With the latest lines on screen, they copy and paste them into a
// conversation instead of opening a terminal.
//
// Only WARN and ERROR go in: dwnvr's INFO is the day-to-day (index loaded,
// dwnvr up), and letting it in would make a restart push out exactly the error
// that explained the restart.
package logbuf

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Line is a warning or error already formatted: the message followed by the
// attributes as key=value, as the TextHandler would write them.
type Line struct {
	At    time.Time `json:"em"`
	Level string    `json:"nivel"`
	Text  string    `json:"texto"`
}

// ring is shared by every Handler derived from the same New: each recorder's
// log.With("cam", ...) writes to the same place as the root.
type ring struct {
	mu    sync.Mutex
	lines []Line
	next  int
	full  bool
	total int64
}

func (a *ring) add(l Line) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lines[a.next] = l
	a.next = (a.next + 1) % len(a.lines)
	if a.next == 0 {
		a.full = true
	}
	a.total++
}

// Handler wraps another slog.Handler: everything goes on to it as before, and
// whatever is WARN or above is also kept in the ring.
type Handler struct {
	next  slog.Handler
	ring  *ring
	attrs string // attributes from With, already formatted
	group string // prefix from WithGroup, with the dot
}

// New keeps the last n warning or error lines.
func New(next slog.Handler, n int) *Handler {
	return &Handler{next: next, ring: &ring{lines: make([]Line, n)}}
}

// Enabled lets every warning and error through, even if the handler below is
// at a higher level: the ring keeps what it would drop.
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelWarn || h.next.Enabled(ctx, l)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		var b strings.Builder
		b.WriteString(r.Message)
		b.WriteString(h.attrs)
		r.Attrs(func(a slog.Attr) bool {
			writeAttr(&b, h.group, a)
			return true
		})
		h.ring.add(Line{At: r.Time, Level: r.Level.String(), Text: b.String()})
	}
	if !h.next.Enabled(ctx, r.Level) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(as []slog.Attr) slog.Handler {
	var b strings.Builder
	b.WriteString(h.attrs)
	for _, a := range as {
		writeAttr(&b, h.group, a)
	}
	return &Handler{next: h.next.WithAttrs(as), ring: h.ring, attrs: b.String(), group: h.group}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{next: h.next.WithGroup(name), ring: h.ring, attrs: h.attrs, group: h.group + name + "."}
}

// Recent returns the stored lines, newest first, and how many warnings and
// errors there have been since dwnvr started - the total says whether what
// shows is everything or just the tail.
func (h *Handler) Recent() ([]Line, int64) {
	a := h.ring
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.next
	if a.full {
		n = len(a.lines)
	}
	out := make([]Line, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, a.lines[(a.next-i+len(a.lines))%len(a.lines)])
	}
	return out, a.total
}

// writeAttr appends " key=value", quoting the value when it has a space - the
// same rule as the TextHandler, so the line reads the same as in docker logs.
func writeAttr(b *strings.Builder, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, g := range v.Group() {
			writeAttr(b, p, g)
		}
		return
	}
	if a.Key == "" {
		return
	}
	s := v.String()
	if s == "" || strings.ContainsAny(s, " \"=\n\t") {
		s = strconv.Quote(s)
	}
	b.WriteByte(' ')
	b.WriteString(prefix)
	b.WriteString(a.Key)
	b.WriteByte('=')
	b.WriteString(s)
}
