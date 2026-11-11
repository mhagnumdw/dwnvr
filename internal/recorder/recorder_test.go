package recorder

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

// discardLogger saves the test from needing a real logger just to exercise the
// alarm, which logs by definition.
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A stopped camera has to report a ZERO rate, not an almost-zero rate.
//
// The exponential average alone only approaches zero: on 09/08/2026, after 3h38
// without recording, /api/health returned bitrateKbps=3.7e-126. Since that is
// still "greater than zero", the retention estimate divided the quota by it and
// published 5.4e+128 days - diagnostics showed a dead camera as if it were
// healthy.
func TestBitrateDropsToZeroWhenNoByteArrives(t *testing.T) {
	r := &Recorder{}
	t0 := time.Now()

	// Two samples with traffic: the rate goes up.
	r.sampleBitrate(t0)
	r.bytes.Store(1 << 20) // 1 MiB
	r.sampleBitrate(t0.Add(15 * time.Second))

	if r.bitrateKbps <= 0 {
		t.Fatalf("rate should be positive with traffic, got %v", r.bitrateKbps)
	}

	// From here on no new byte comes in.
	for i := 1; i <= 3; i++ {
		r.sampleBitrate(t0.Add(time.Duration(15+15*i) * time.Second))
		if r.bitrateKbps != 0 {
			t.Fatalf("sample %d without traffic: rate %v, want 0", i, r.bitrateKbps)
		}
	}
}

// The retention estimate can't blow up when the rate is negligible: that's
// exactly where "5.4e+128 days" on the screen came from.
//
// With no history on disk there's no density, so the rate answers - and that's
// exactly where it needs the floor.
func TestRetentionDoesNotEstimateWithNegligibleRate(t *testing.T) {
	tests := []struct {
		name     string
		rate     float64
		estimate bool
	}{
		{"stopped", 0, false},
		{"exponential average residue", 3.7e-126, false},
		{"below the floor", 0.5, false},
		{"real camera", 900, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days := retainDays(20480, 0, 0, tt.rate)

			if tt.estimate && days <= 0 {
				t.Errorf("rate %v should produce an estimate, got %v", tt.rate, days)
			}
			if !tt.estimate && days != 0 {
				t.Errorf("rate %v shouldn't produce an estimate, got %v", tt.rate, days)
			}
		})
	}
}

// The estimate comes from the density of what's already recorded, not from the
// instant rate. The anchored case is cam_alfa on 18/08/2026, which prompted the
// change: 20478 MB of recording covering 8.23 days, with a 20480 MB quota.
//
// With the quota full, "fits" has to give the same as the "retained" shown
// beside it - the old number, taken from the rate at that instant, said 9.6 days
// at dawn and ~5 in the afternoon, and that swing is what made the screen look
// wrong.
func TestRetentionUsesTheDensityOfWhatIsRecorded(t *testing.T) {
	const (
		day   = int64(86400000)
		bytes = int64(20478) << 20
		span  = int64(8.23 * float64(day))
	)

	// An absurd rate alongside, on purpose: if density didn't take precedence,
	// it would show in the result.
	days := retainDays(20480, bytes, span, 5000)
	if days < 8.1 || days > 8.4 {
		t.Errorf("days = %v, want ~8.23 (the real density, not the rate)", days)
	}

	// The math is linear in the quota: doubling the quota doubles what fits.
	// The edit screen depends on that to answer "what if I set 40 GB?".
	if double := retainDays(40960, bytes, span, 5000); double < 2*days-0.01 || double > 2*days+0.01 {
		t.Errorf("doubled quota = %v, want twice %v", double, days)
	}
}

// A freshly added camera is when the estimate is most useful - it's what the
// quota is chosen with - and it's when there's no history to take density from.
// Below the minimum span, the rate answers.
func TestRetentionFallsBackToRateWithShortHistory(t *testing.T) {
	const bytes = int64(200) << 20

	short := retainDays(20480, bytes, minSpanForEstimate-1, 900)
	byRate := retainDays(20480, 0, 0, 900)
	if short != byRate {
		t.Errorf("short span gave %v, want the rate estimate (%v)", short, byRate)
	}

	// One more minute of history and density takes over.
	long := retainDays(20480, bytes, minSpanForEstimate+60000, 900)
	if long == byRate {
		t.Errorf("with enough span density should take over, got the rate (%v)", long)
	}

	// With neither source the field stays zero and the screen shows "-".
	if empty := retainDays(20480, 0, 0, 0); empty != 0 {
		t.Errorf("no rate and no history = %v, want 0", empty)
	}
}

// A recorder that just started can't be taken as stopped: the first segment
// only closes after segmentSeconds, and alarming in that interval would make
// the screen shout on every restart.
func TestSilenceToleratesStartupAndAlarmsLater(t *testing.T) {
	r := &Recorder{startedAt: time.Now(), log: discardLogger()}
	r.cam.SegmentSeconds = 30

	r.mu.RLock()
	limit := r.silenceLimitLocked()
	r.mu.RUnlock()
	if limit != 90*time.Second {
		t.Fatalf("limit = %v, want 90s (3 segments)", limit)
	}

	// Just started: no alarm, and nothing logged.
	r.checkSilence(r.startedAt.Add(60 * time.Second))
	if r.silentLogged {
		t.Error("alarmed before the first segment could exist")
	}

	// Past the limit without closing any segment: alarms.
	r.checkSilence(r.startedAt.Add(2 * time.Minute))
	if !r.silentLogged {
		t.Fatal("should have alarmed after 2min without recording")
	}

	// Closed a segment: back to normal.
	r.setLastEndMs(r.startedAt.Add(2 * time.Minute).UnixMilli())
	r.checkSilence(r.startedAt.Add(2*time.Minute + time.Second))
	if r.silentLogged {
		t.Error("should have gone back to normal after recording")
	}
}

// The one-minute floor protects whoever uses short segments: 3×5s would alarm
// at every network hiccup.
func TestSilenceLimitHasAFloor(t *testing.T) {
	r := &Recorder{}
	r.cam.SegmentSeconds = 5
	r.mu.RLock()
	defer r.mu.RUnlock()
	if got := r.silenceLimitLocked(); got != time.Minute {
		t.Errorf("limit = %v, want the 1min floor", got)
	}
}
