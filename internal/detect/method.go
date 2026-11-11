package detect

import "math"

// Method decides WHEN to fire and WHERE to look. It gets the frames in the
// order they arrive and answers, frame by frame, whether that instant is an
// onset - the instant that wakes the object detector - and which frame the
// object detector should infer because of it.
//
// Those are two questions because the answers are different instants. The
// MARK sits on the onset, which is what matches the arrival. The FRAME to
// infer is the one with the highest salience in the following PeakWindowMs: at
// the onset the object is still coming in, half of it in view, and inferring
// at the peak takes `pessoa` from 9.0% to 12.2% of arrivals, without one extra
// detection.
//
// It's the piece the user swaps in the camera settings, so it's a contract:
// `kleinberg-p` is the statistical one, which spends each inference where the
// image changed, and `periodico` is the honest reference to beat, which the UI
// offers to whoever prefers a predictable cost to a well-spent one. A new
// method goes into the Methods map.
//
// Not safe for concurrent use: each camera has its own.
type Method interface {
	// Frame takes ONE VIDEO frame - audio never gets here - and says what to
	// do with it. See Verdict.
	//
	// `atMs` is the frame's wall clock, `bytes` is the whole mdat box and
	// `keyframe` says whether it's an I frame. It's O(1) and doesn't allocate:
	// it runs for every camera, at 15 frames per second, on an Orange Pi Zero
	// 3.
	Frame(atMs int64, bytes int, keyframe bool) Verdict

	// Reset forgets everything, including an open peak window. The recorder
	// calls it on every reconnection.
	Reset()

	Name() string
}

// Verdict is the Method's answer to a frame. It's three independent signals,
// and whoever gets them handles them in THIS order - which is what lets a
// frame, in theory, close one onset's window and open the next one's:
//
//  1. Cut: the previous onset's window ended BEFORE this frame. The GOP clip
//     goes up to the last Candidate, and this frame is left out.
//  2. Onset: this frame opens a motion interval. It's the timeline mark.
//  3. Candidate: the frame to infer becomes THIS one. The clip, when cut, goes
//     up to it, inclusive.
//
// The onset frame is always the first candidate: if nothing after it stands
// out more, inferring at the peak is inferring at the onset - and that's what
// happens 60% of the time.
type Verdict struct {
	Cut       bool
	Onset     bool
	Candidate bool
}

// Methods is the constructor map: switching method is switching the string in
// the camera's `detectMecanismo` field. The level is the 1 to 5 one from the
// camera settings, and it carries the cost - each method knows how to
// translate it into what concerns it.
var Methods = map[string]func(level int) Method{
	"kleinberg-p": func(level int) Method {
		return newStatistical(ScoreFuncs["kleinberg-p"](), KleinbergPThreshold[level])
	},
	"periodico": func(level int) Method {
		return newPeriodic(3_600_000 / OnsetsPerHour[level])
	},
}

// --- the peak window --------------------------------------------------------

// peakWindow tracks, after each onset, which frame stood out the most.
//
// It's the same math the gain of inferring at the peak was measured on: it
// starts at the onset itself, only P frames count, and only one that stands
// out STRICTLY more takes the place. I frames stay out for the same reason
// they stay out of the score - they're always large, with or without motion.
type peakWindow struct {
	widthMs int64
	open    bool
	onsetMs int64
	best    float64
}

// start begins an onset's window, with the onset itself as the best so far.
func (j *peakWindow) start(atMs int64, salience float64) {
	j.open, j.onsetMs, j.best = true, atMs, salience
}

// close ends the window and says whether one was open. It's what the
// recording gap and Reset use: after them the video is a different one, and
// the peak can't come from there.
func (j *peakWindow) close() bool {
	was := j.open
	j.open = false
	return was
}

// passed closes the window if this frame is already outside it.
func (j *peakWindow) passed(atMs int64) bool {
	if !j.open || atMs <= j.onsetMs+j.widthMs {
		return false
	}
	j.open = false
	return true
}

// beats says whether this frame becomes the candidate.
func (j *peakWindow) beats(salience float64, keyframe bool) bool {
	if !j.open || keyframe || salience <= j.best {
		return false
	}
	j.best = salience
	return true
}

// NewMethod returns a camera's method. An unknown name or a level out of range
// falls back to the default: a mistyped config must not silently leave the
// camera without a trigger.
func NewMethod(method string, level int) Method {
	if level < MinLevel || level > MaxLevel {
		level = DefaultLevel
	}
	ctor, ok := Methods[method]
	if !ok {
		ctor = Methods[DefaultMethod]
	}
	return ctor(level)
}

// --- the frame clock --------------------------------------------------------

// frameClock measures how much time passed between one frame and the next,
// and is where a recording gap is noticed.
type frameClock struct {
	lastMs int64
	has    bool
}

func (r *frameClock) reset() { r.has = false }

// step returns the time since the previous frame and whether there was a gap
// in between. The first frame after a reset is not a gap: there's no previous
// one to compare with, and the state is already clean.
func (r *frameClock) step(atMs int64) (dtMs float64, gap bool) {
	prev, had := r.lastMs, r.has
	r.lastMs, r.has = atMs, true
	if !had {
		return 0, false
	}
	d := atMs - prev
	// A clock going backwards is as untrustworthy as a gap: boards without an
	// RTC boot with the wrong date and NTP fixes it later.
	if d < 0 || d > GapMs {
		return 0, true
	}
	return float64(d), false
}

// --- the statistical method: score + hysteresis -----------------------------

// statistical is a camera's Score plus the hysteresis trigger that turns its
// salience into onsets.
//
// Two choices change the number, so they're written down:
//
// THE ONSET COMES OUT ON OPENING, not after confirmation. It's the instant the
// GOP clip is dispatched, and waiting for confirmation would cost latency -
// which is exactly what makes the episode get lost. An onset 9 seconds late is
// the object detector looking at an empty backyard.
//
// THERE'S NO REFRACTORY PERIOD HERE. The hysteresis is already what keeps the
// same event from firing twenty times; a refractory period on top would mix
// two things into the same number, and that's not how the thresholds were
// measured. The queue's brake (ClipsPerCamera) is something else: it protects
// the BUDGET, and lives on the object detector's side.
type statistical struct {
	score     Score
	high, low float64
	closeMs   int64

	clock  frameClock
	frames int

	open         bool
	hasBelow     bool
	belowSinceMs int64

	peak peakWindow
}

func newStatistical(score Score, high float64) *statistical {
	return &statistical{
		score:   score,
		high:    high,
		low:     LowThreshold(high),
		closeMs: int64(SecondsToClose * 1000),
		peak:    peakWindow{widthMs: PeakWindowMs},
	}
}

// LowThreshold is the closing threshold, from the opening one.
//
// Written with the absolute value because not every salience is positive -
// `kleinberg` returns a difference of log-likelihoods, which spends most of
// the time below zero. With a positive high threshold this is exactly
// `LowThresholdFraction * high`; with a negative threshold, it goes DOWN
// instead of up, which is what "closing" has to mean in both cases.
func LowThreshold(high float64) float64 {
	return high - (1-LowThresholdFraction)*math.Max(math.Abs(high), 1e-9)
}

func (m *statistical) Name() string { return m.score.Name() }

func (m *statistical) Reset() {
	m.score.Reset()
	m.clock.reset()
	m.frames = 0
	m.open, m.hasBelow = false, false
	m.peak.close()
}

func (m *statistical) Frame(atMs int64, bytes int, keyframe bool) Verdict {
	var v Verdict
	dtMs, gap := m.clock.step(atMs)
	if gap {
		// The gap forces the interval closed: the score got a Reset and what
		// came before no longer counts. The peak window closes with it, and
		// the clip it was waiting for goes out with what it already had.
		m.score.Reset()
		m.frames = 0
		m.open, m.hasBelow = false, false
		v.Cut = m.peak.close()
	}

	salience := m.score.Push(bytes, keyframe, dtMs)

	if m.peak.passed(atMs) {
		v.Cut = true
	}
	v.Candidate = m.peak.beats(salience, keyframe)

	// During warmup the function has no baseline yet, and its number doesn't
	// count. Firing here would spend budget on the warmup itself.
	m.frames++
	if m.frames <= m.score.Warmup() {
		return v
	}

	switch {
	case salience >= m.high:
		m.hasBelow = false
		if m.open {
			return v
		}
		m.open = true
		v.Onset, v.Candidate = true, true
		m.peak.start(atMs, salience)

	case salience < m.low:
		if !m.hasBelow {
			m.hasBelow, m.belowSinceMs = true, atMs
		} else if atMs-m.belowSinceMs >= m.closeMs {
			m.open, m.hasBelow = false, false
		}

	default:
		// Between the two thresholds the interval stays as it is, and the
		// closing clock restarts: that's what keeps the marker from splitting
		// when the signal hovers around the line.
		m.hasBelow = false
	}
	return v
}

// --- the periodic method ----------------------------------------------------

// periodic fires every X seconds, ignoring the signal entirely.
//
// It's the reference to beat: if the statistics don't beat it comfortably,
// they weren't worth it. At level 4 both spend the same 50 detections per
// hour, and `kleinberg-p` catches 41.5% of arrivals where this one catches
// 15.1%.
//
// It's still around for two reasons: it's the most predictable cost there is -
// exactly 3600/period per hour, rain or shine - and it's the second opinion for
// whoever distrusts the statistical trigger on a specific camera.
//
// It has no salience, so it has no peak: it infers at the onset itself. Its
// window has zero width and closes on the next frame.
type periodic struct {
	periodMs float64
	sinceMs  float64
	clock    frameClock
	peak     peakWindow
}

func newPeriodic(periodMs float64) *periodic {
	return &periodic{periodMs: periodMs}
}

func (p *periodic) Name() string { return "periodico" }

func (p *periodic) Reset() {
	p.clock.reset()
	p.sinceMs = 0
	p.peak.close()
}

func (p *periodic) Frame(atMs int64, _ int, _ bool) Verdict {
	var v Verdict
	dtMs, gap := p.clock.step(atMs)
	if gap {
		// After reconnecting the camera waits a full period, instead of firing
		// right at the restart.
		p.sinceMs = 0
		v.Cut = p.peak.close()
		return v
	}
	v.Cut = p.peak.passed(atMs)

	p.sinceMs += dtMs
	if p.sinceMs < p.periodMs {
		return v
	}
	// Subtracts the period instead of zeroing: zeroing throws away the
	// leftover fraction of a frame and the firing drifts forward every round,
	// which would make the cost stop being exactly 3600/period - the only
	// reason this method exists.
	p.sinceMs -= p.periodMs
	p.peak.start(atMs, 0)
	v.Onset, v.Candidate = true, true
	return v
}
