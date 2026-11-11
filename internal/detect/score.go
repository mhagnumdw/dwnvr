package detect

import "math"

// Score answers "how much does THIS frame stand out from what this camera
// usually does". Zero is its normal; 3 is three deviations above its normal.
//
// It doesn't know what an onset is and decides nothing: the Method decides.
// That separation is what allows tuning every function to the same cost and
// comparing what they catch - that's how the default function was picked
// among two dozen candidates.
//
// Not safe for concurrent use: each camera has its own, and only its goroutine
// touches it.
type Score interface {
	// Push takes ONE frame and returns its salience. `bytes` is the whole mdat
	// box, header included, which is the number the recorder has at hand
	// without any math. `dtMs` is how much time passed since the previous
	// frame.
	Push(bytes int, keyframe bool, dtMs float64) float64

	// Reset forgets everything. Recording gap or reconnection: what came
	// before no longer counts.
	Reset()

	// Warmup is how many frames the function needs to see after each Reset
	// before its number counts.
	Warmup() int

	Name() string
}

// ScoreFuncs is the map that makes swapping a function a matter of swapping a
// string, so the default function can be replaced without touching its users.
//
// Only the ones that won the measurement are here. A new function goes in
// after being measured against these, at the same cost.
var ScoreFuncs = map[string]func() Score{
	"kleinberg":   func() Score { return newKleinberg() },
	"kleinberg-p": func() Score { return hideIFrames(newKleinberg()) },
}

// --- exponential mean and deviation -----------------------------------------

// ewma is the exponential mean and deviation over which `kleinberg` measures
// the z.
//
// The half life is in FRAMES, not seconds: cameras range from 10 to 15 fps and
// the baseline has to be the same in number of samples.
type ewma struct {
	alpha          float64
	mean, variance float64
	n              int
	hasMean        bool
}

func newEWMA(halfLife float64) *ewma {
	return &ewma{alpha: 1 - math.Exp(math.Log(0.5)/halfLife)}
}

func (e *ewma) reset() { e.mean, e.variance, e.n, e.hasMean = 0, 0, 0, false }

func (e *ewma) push(x float64) {
	e.n++
	if !e.hasMean {
		e.mean, e.variance, e.hasMean = x, 0, true
		return
	}
	d := x - e.mean
	e.mean += e.alpha * d
	e.variance = (1 - e.alpha) * (e.variance + e.alpha*d*d)
}

func (e *ewma) stdDev() float64 { return math.Sqrt(e.variance) }

// --- `kleinberg` ------------------------------------------------------------

// kleinberg is a two-state burst detector, "calm" and "bursty".
//
// It was made precisely for the question "when did the burst start in a
// stream", and here it's in its cheap online form: the likelihood ratio
// between the two states, accumulated with a cost to switch states.
//
// It beat the other candidates by a margin of seven standard errors over the
// runner-up, and doesn't degrade at night - half the hours of an NVR are
// night.
type kleinberg struct {
	e            *ewma
	calm, bursty float64
}

func newKleinberg() *kleinberg {
	k := &kleinberg{e: newEWMA(KleinbergHalfLife)}
	k.Reset()
	return k
}

func (k *kleinberg) Name() string { return "kleinberg" }
func (k *kleinberg) Warmup() int  { return KleinbergWarmup }

func (k *kleinberg) Reset() {
	k.e.reset()
	k.calm, k.bursty = 0, -KleinbergSwitchCost
}

func (k *kleinberg) Push(bytes int, _ bool, _ float64) float64 {
	// The log is what puts 28 and 305 kbps cameras on the same scale: what
	// matters is the RATIO between the frame and its normal, not the
	// difference.
	x := math.Log(float64(max(bytes, 1)))

	if !k.e.hasMean || k.e.n < FramesBeforeFirstScore {
		k.e.push(x)
		return 0
	}

	z := (x - k.e.mean) / math.Max(k.e.stdDev(), LogStdDevFloor)
	// Learns only outside the event. See FreezeAbove.
	if z < FreezeAbove {
		k.e.push(x)
	}

	// Log-likelihood of each state. The bursty one expects the mean
	// KleinbergFactor deviations higher.
	lc := -0.5 * z * z
	la := -0.5 * (z - KleinbergFactor) * (z - KleinbergFactor)
	newCalm := math.Max(k.calm, k.bursty-KleinbergSwitchCost) + lc
	newBursty := math.Max(k.bursty, k.calm-KleinbergSwitchCost) + la

	// Normalizes, otherwise both sink together and the float overflows.
	floor := math.Max(newCalm, newBursty)
	k.calm, k.bursty = newCalm-floor, newBursty-floor
	return k.bursty - k.calm
}

// --- the variant that hides the I frame -------------------------------------

// noIFrame wraps a function and hides the I frames from it.
//
// The I frame doesn't measure motion, it measures the whole SCENE again: it
// comes back every GOP whether something happened or not, and is 27 to 258
// times larger than a still-scene P. Since it's rare - 1.6% to 5% of frames -,
// a function tuned to 50 onsets/hour would spend its ENTIRE budget deciding
// which I frame is too large, and the motion, which lives in the P frames,
// would never get inferred.
//
// The measured effect is not subtle: the candidates split in two, with the
// seven functions that don't see the I frame above the reference and the
// seven that do below it - worse than firing at random.
//
// It costs nothing: the recorder already has that bit at hand.
type noIFrame struct {
	inner Score
	last  float64
}

func hideIFrames(inner Score) Score { return &noIFrame{inner: inner} }

func (s *noIFrame) Name() string { return s.inner.Name() + "-p" }
func (s *noIFrame) Warmup() int  { return s.inner.Warmup() }

func (s *noIFrame) Reset() {
	s.inner.Reset()
	s.last = 0
}

func (s *noIFrame) Push(bytes int, keyframe bool, dtMs float64) float64 {
	if keyframe {
		return s.last
	}
	s.last = s.inner.Push(bytes, keyframe, dtMs)
	return s.last
}
