package recorder

import (
	"math"
	"math/rand/v2"
	"testing"
)

// The estimate can't follow the burst: a burst only delays arrival, and the
// frame that arrives with no extra delay is the right measure. Here 1 in every
// 20 frames arrives at the minimum delay and the rest up to 1.1s later - the
// swing measured on the real network.
func TestClockIgnoresBursts(t *testing.T) {
	c := mediaClock{bucket: clockBucket.Milliseconds()}
	rng := rand.New(rand.NewPCG(1, 2))
	const wallStart, minDelay = int64(1_789_151_000_000), int64(300)

	for i := range 15 * 120 { // 2 minutes at 15fps
		media := int64(i) * 1000 / 15
		delay := minDelay
		if i%20 != 0 {
			delay += rng.Int64N(1100)
		}
		c.observe(media, wallStart+media+delay)
	}

	media := int64(15*120) * 1000 / 15
	got, ok := c.wall(media)
	if want := wallStart + media + minDelay; !ok || got != want {
		t.Fatalf("wall = %d (ok=%v), want %d: off by %dms", got, ok, want, got-want)
	}
}

func TestClockWithoutObservation(t *testing.T) {
	var c mediaClock
	if _, ok := c.wall(0); ok {
		t.Fatal("no observation, no estimate")
	}
}

func TestSplicedStart(t *testing.T) {
	const splice, half, tol, gap = int64(100_000), int64(33), int64(250), int64(1000)
	tests := []struct {
		name   string
		target int64
		want   int64
	}{
		{"within tolerance stays at the splice", splice + 200, splice},
		{"within tolerance, ahead", splice - 200, splice},
		{"behind: half a frame forward", splice + 600, splice + half},
		{"ahead: half a frame back", splice - 600, splice - half},
		{"far ahead: still only half a frame", splice - 60_000, splice - half},
		{"behind beyond the gap: jumps to the clock", splice + 10_000, splice + 10_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splicedStart(splice, tt.target, half, tol, gap); got != tt.want {
				t.Errorf("splicedStart = %d, want %d", got, tt.want)
			}
		})
	}
}

// simulation reproduces the segmenter over a synthetic connection: frames at
// 15fps, a keyframe every 2s, a cut after 30s of media. The camera clock runs
// `drift` faster than real time (negative: slower), the network delays each
// frame between 300ms and 1.4s, and frames after index `freeze` arrive
// `freezeMs` later without the media counting that time.
type simulation struct {
	drift    float64
	hours    float64
	freeze   int
	freezeMs float64
	old      bool // uses the old rule: always at the splice when the clock falls behind
}

type simSeg struct {
	start, dur int64
	truth      float64 // when the keyframe was delivered with no extra delay
}

func (p simulation) run() []simSeg {
	const mediaFrame = 1000.0 / 15
	const minDelay = 300.0
	rng := rand.New(rand.NewPCG(3, 4))
	c := mediaClock{bucket: clockBucket.Milliseconds()}

	var segs []simSeg
	var lastEnd, mediaStart int64
	opened := false
	n := int(p.hours * 3600 * 15)
	for i := range n {
		media := float64(i) * mediaFrame
		real := 1_789_151_000_000 + media/(1+p.drift)
		if p.freeze > 0 && i >= p.freeze {
			real += p.freezeMs
		}
		arrival := real + minDelay + rng.Float64()*1100
		c.observe(int64(media), int64(arrival))

		keyframe := i%30 == 0
		if !keyframe || (opened && int64(media)-mediaStart < 30_000) {
			continue
		}
		if opened {
			segs[len(segs)-1].dur = int64(media) - mediaStart
			lastEnd = segs[len(segs)-1].start + segs[len(segs)-1].dur
		}
		target, _ := c.wall(int64(media))
		var start int64
		switch {
		case !opened:
			start = max(target, lastEnd)
		case p.old:
			start = max(int64(arrival), lastEnd)
		default:
			start = splicedStart(lastEnd, target, 33,
				clockTolerance.Milliseconds(), clockGap.Milliseconds())
		}
		segs = append(segs, simSeg{start: start, truth: real + minDelay})
		mediaStart, opened = int64(media), true
	}
	return segs[:len(segs)-1] // the last one didn't close
}

// The 11/09/2026 defect: cam_zeta runs 0.04% ahead of the real clock, and the
// old rule, which only pushed the start to the splice, let the timeline creep
// ahead segment after segment. In 8h it goes past 10s.
func TestOldRuleAccumulatesDrift(t *testing.T) {
	segs := simulation{drift: 0.0004, hours: 8, old: true}.run()
	last := segs[len(segs)-1]
	if errMsg := float64(last.start) - last.truth; errMsg < 10_000 {
		t.Fatalf("the simulation should reproduce the old rule's drift; final error %.0fms", errMsg)
	}
}

// The new rule keeps the timeline close to the clock in both drift directions,
// and each splice overlaps or opens at most half a frame - which MSE plays
// without losing a frame and without stalling.
//
// The connection's first segment is a known exception: its only measure is the
// arrival of its own keyframe, and it inherits that arrival's burst. The
// simulation uses the worst case of Phase 2 (up to 1.1s); measured on the real
// go2rtc on 11/09/2026, the first fragment arrived at most 74ms above the
// minimum delay. From then on the half-frame correction per segment removes the
// excess, and the test checks that it only shrinks and is gone within 15
// minutes.
func TestTimelineFollowsTheClockWithoutLosingFrames(t *testing.T) {
	for _, tt := range []struct {
		name  string
		drift float64
	}{
		{"camera on pace", 0},
		{"camera 0.04% ahead, cam_zeta", 0.0004},
		{"camera 0.04% behind", -0.0004},
		{"camera 0.08% ahead, twice cam_zeta", 0.0008},
	} {
		t.Run(tt.name, func(t *testing.T) {
			segs := simulation{drift: tt.drift, hours: 8}.run()
			// The tolerated error is the tolerance plus the slack that frames
			// delivered with minimum delay leave within a bucket.
			limit := float64(clockTolerance.Milliseconds()) + 60
			worst := math.Abs(float64(segs[0].start) - segs[0].truth)
			for k, s := range segs {
				if k > 0 {
					prevEnd := segs[k-1].start + segs[k-1].dur
					if d := s.start - prevEnd; d < -33 || d > 33 {
						t.Fatalf("segment %d: splice of %dms, more than half a frame", k, d)
					}
				}
				errMsg := math.Abs(float64(s.start) - s.truth)
				if k < 30 { // the first 15 minutes
					if errMsg > max(worst, limit) {
						t.Fatalf("segment %d: error rose to %.0fms during convergence", k, errMsg)
					}
					worst = max(errMsg, limit)
					continue
				}
				if errMsg > limit {
					t.Fatalf("segment %d (%.1fh): timeline %.0fms from the clock",
						k, float64(k)*30/3600, errMsg)
				}
			}
		})
	}
}

// A camera that freezes for 10s and resumes without jumping its clock: the
// media didn't count that time, and the timeline must show a gap instead of
// staying 10s behind until the next reconnection.
func TestFreezeBecomesAGap(t *testing.T) {
	const freezeAt = 15 * 600 // at 10 minutes
	segs := simulation{hours: 0.5, freeze: freezeAt, freezeMs: 10_000}.run()

	gaps := 0
	for k := 1; k < len(segs); k++ {
		if segs[k].start-(segs[k-1].start+segs[k-1].dur) > 5000 {
			gaps++
		}
	}
	if gaps != 1 {
		t.Fatalf("want a gap after the freeze, got %d", gaps)
	}
	// Both estimator buckets have to empty of the past before the jump: up to 3
	// segments after the freeze the error can still be the freeze's.
	for k, s := range segs {
		if float64(k)*30 < 600+3*30 {
			continue
		}
		if errMsg := float64(s.start) - s.truth; errMsg < -400 || errMsg > 400 {
			t.Fatalf("segment %d: timeline %.0fms from the clock after the freeze", k, errMsg)
		}
	}
}
