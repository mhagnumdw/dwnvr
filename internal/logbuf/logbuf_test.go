package logbuf

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestOnlyWarnAndErrorAreKept(t *testing.T) {
	var out bytes.Buffer
	h := New(slog.NewTextHandler(&out, nil), 10)
	log := slog.New(h)

	log.Info("index loaded", "cam", "a")
	log.Warn("connection dropped", "err", errors.New("EOF"))
	log.Error("camera stopped recording")

	lines, total := h.Recent()
	if total != 2 || len(lines) != 2 {
		t.Fatalf("kept %d of total %d, want 2 and 2", len(lines), total)
	}
	// Newest first.
	if lines[0].Level != "ERROR" || lines[1].Text != "connection dropped err=EOF" {
		t.Errorf("lines %+v", lines)
	}
	// The regular log still comes out whole.
	if !strings.Contains(out.String(), "index loaded") {
		t.Error("the INFO line is missing from the output log")
	}
}

func TestRingDropsOldest(t *testing.T) {
	h := New(slog.DiscardHandler, 3)
	log := slog.New(h)
	for _, m := range []string{"1", "2", "3", "4", "5"} {
		log.Warn(m)
	}
	lines, total := h.Recent()
	if total != 5 {
		t.Errorf("total %d, want 5", total)
	}
	var got []string
	for _, l := range lines {
		got = append(got, l.Text)
	}
	if strings.Join(got, ",") != "5,4,3" {
		t.Errorf("kept %v, want 5,4,3", got)
	}
}

// Each recorder's log.With("cam", ...) must write to the same ring, with the
// attribute along - it's what says which camera the error is from.
func TestWithSharesTheRing(t *testing.T) {
	h := New(slog.DiscardHandler, 5)
	slog.New(h).With("cam", "garage").WithGroup("g").Warn("connection dropped", "wait", "2s later")

	lines, _ := h.Recent()
	if len(lines) != 1 {
		t.Fatalf("kept %d lines", len(lines))
	}
	if want := `connection dropped cam=garage g.wait="2s later"`; lines[0].Text != want {
		t.Errorf("text %q, want %q", lines[0].Text, want)
	}
}
