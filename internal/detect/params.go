// Package detect finds the ONSETS - the instants when something changed
// enough to be worth an expensive inference - from the size of each frame,
// which the recorder already has at hand and would otherwise throw away.
//
// The design has THREE separate pieces, and the separation is what makes
// swapping a function a matter of changing a string in the config:
//
//	frame -> [Score] -> salience -> [Method] -> onset -> [Detector]
//	         the        a unitless    decides     what     what it
//	         statistic  number        when to     wakes    was
//	                                  fire and
//	                                  where to look
//
// Between the Method and the Detector sits the Clipper, which keeps the
// current GOP and cuts the video clip at the frame the Method picked.
//
// The Score decides NOTHING. It only answers "how much does this frame stand
// out", in units of the camera's OWN noise. That's why a sensitivity level
// means the same on a quiet camera and on one full of infrared noise: what
// differs between them is the baseline, not the number.
//
// Two properties are not details, they are requirements: O(1) per frame and
// ZERO allocations on the hot path. This runs inside the recorder, for every
// camera, on hardware the size of an Orange Pi Zero 3.
//
// Everything here was measured on real recordings before it was written. What
// detection does, and why, is in docs/deteccao.md.
package detect

// This file holds ALL of the package's numbers. A number loose in the middle
// of the code is a number nobody finds to tune, and each of these has its
// reason written next to it: either it came from a measurement, or it's a
// design choice.

// --- the five sensitivity levels --------------------------------------------

// DefaultLevel is 5 (very high). On the Orange Pi Zero 3, with the
// dwnvr-detect model, it costs ~17% of a core PER CAMERA (100 inferences per
// hour, ~6.5 s each): measured with 9 cameras on it, dwnvr-detect sat at 157%
// of a core, 39% of the four-core board, alongside recording. It's twice
// level 4, and catches 57.4% of arrivals against 41.5%. Someone with many
// cameras on a smaller board goes down to 4.
const DefaultLevel = 5

// MinLevel and MaxLevel are the range the UI offers.
const (
	MinLevel = 1
	MaxLevel = 5
)

// OnsetsPerHour is the COST of each level, per camera. It's what the user
// pays: each onset is a detection, and a detection is the feature's entire
// CPU cost.
//
// The number is per camera: with ten cameras at level 4 it's not 50
// detections per hour, it's 500.
//
// It's the yardstick SHARED by both methods, and that's what makes them
// comparable: at the same level 4 both spend 50 detections per hour, and
// `kleinberg-p` catches 41.5% of arrivals where `periodico` catches 15.1%.
var OnsetsPerHour = [MaxLevel + 1]float64{0, 6, 12, 25, 50, 100}

// KleinbergPThreshold is the opening threshold that makes `kleinberg-p` cost
// exactly what the level says. It was measured over hundreds of hours of
// recordings from several cameras, with ONE threshold for all of them: it's
// the salience each camera has to reach, in units of its own noise, and not a
// per-camera number.
//
// These numbers belong to the score parameters just below. Changing the half
// life, the factor or the switch cost without recalibrating here breaks
// nothing visibly: it just makes the five levels quietly mean something else.
// Recalibrating means measuring again, over real recordings, which threshold
// gives each cost in OnsetsPerHour.
var KleinbergPThreshold = [MaxLevel + 1]float64{0, 15.8246, 11.2852, 7.6377, 4.9695, 2.1312}

// --- the hysteresis trigger -------------------------------------------------

// LowThresholdFraction is how much of the opening threshold becomes the
// closing threshold. Without two thresholds, a person crossing the backyard
// makes the signal cross the line twenty times and the marker turns into
// twenty slivers instead of one bar.
const LowThresholdFraction = 0.4

// SecondsToClose is how long the salience must stay below the low threshold
// for the interval to close. A person who stops for a moment doesn't cut the
// marker in two.
const SecondsToClose = 3.0

// GapMs is the slack beyond which two consecutive frames stop being
// consecutive: above it there was a recording gap, and what came before no
// longer counts for the statistics.
//
// It's the same 2 s the API uses to stitch contiguous timeline ranges
// (gapTolerance, in internal/api/recordings.go), and the same that marked the
// gaps in the recordings the thresholds were measured on. Having to be the
// same number isn't elegance: it's what makes the measurement hold here.
const GapMs = 2000

// --- when to infer ----------------------------------------------------------

// PeakWindowMs is how long after the onset the method looks for the frame to
// infer: the one with the highest salience in this window, not the onset's
// own.
//
// Measured: at level 4, inferring at the peak instead of the onset takes
// `pessoa` from 9.0% to 12.2% of arrivals and `veiculo` from 30.8% to 38.6%,
// without one extra detection. The 2 s and 3 s windows tie within the noise;
// waiting a FIXED time after the onset, instead of the peak, is worse - the
// object has already left.
//
// The price is latency, not CPU: the clip can only be cut when the window
// closes.
const PeakWindowMs = 3000

// MaxGOPBytes is the largest GOP the Clipper keeps. Beyond that the rest of
// the GOP isn't kept, and an onset that falls in it marks motion without being
// inferred.
//
// It's not a tuning number, it's a memory brake: each camera keeps up to two
// GOPs, and on a 1 GB board a camera without an I frame for minutes must not
// grow a buffer forever. Common cameras at 28 to 305 kbps, with 2 to 6 s GOPs,
// stay at ~230 KB in the worst case. The 4 MB cover 1080p at 8 Mbps with a
// 4 s GOP, and a camera above that can't even be decoded in useful time on a
// small board.
const MaxGOPBytes = 4 << 20

// --- the `kleinberg` score --------------------------------------------------

const (
	// KleinbergHalfLife is the memory of the mean and the deviation, in
	// FRAMES and not seconds, on purpose: cameras range from 10 to 15 fps and
	// the baseline has to be the same in number of samples.
	KleinbergHalfLife = 200.0

	// KleinbergFactor is how many deviations above the mean the "bursty"
	// state expects to find.
	KleinbergFactor = 3.0

	// KleinbergSwitchCost is the price, in log-likelihood, of switching
	// state. It's what keeps the detector from jumping between calm and bursty
	// on every frame.
	KleinbergSwitchCost = 2.0

	// LogStdDevFloor keeps the division from blowing up in a still scene,
	// where every frame has almost the same size and the log deviation goes to
	// zero.
	//
	// It's in units of LOG bytes, not bytes. Both hypotheses that this floor
	// was the lever for the cameras that fire too much were measured, and both
	// failed: this floor never touches them, and a floor in bytes makes the
	// trigger fire MORE.
	LogStdDevFloor = 0.05

	// FramesBeforeFirstScore is how many frames the score swallows before
	// returning any non-zero number. With fewer, the mean is still the first
	// frame and the z comes out absurd.
	FramesBeforeFirstScore = 30

	// FreezeAbove is the salience above which the score STOPS LEARNING.
	//
	// Without it the baseline chases the event itself and absorbs it: in an
	// earlier version, without freezing, a camera rose 53x above its baseline
	// in a confirmed event and the z came out at 2.8, which doesn't fire even
	// at the loosest threshold.
	//
	// It's an INTERNAL constant, not the trigger threshold, on purpose. If it
	// depended on the trigger, the salience would change with each sensitivity
	// level, and calibrating the thresholds would have to be done five times
	// instead of once.
	FreezeAbove = 3.0

	// KleinbergWarmup is how many frames the function needs to see, after
	// each Reset, before its number counts. During those frames the method
	// doesn't fire: it's the same cut applied when measuring the thresholds.
	KleinbergWarmup = 300
)

// DefaultMethod is `kleinberg`, in the variant that hides the I frame. Of the
// functions measured, it caught the most arrivals for the same cost: 41.5% at
// level 4, against 15.1% for firing periodically. It's what a newly added
// camera uses.
const DefaultMethod = "kleinberg-p"

// --- what the object detector finds, and what becomes a mark ---------------

// ClassFamilies groups the model's classes into the three families the
// timeline colors. A class not in here doesn't matter, and is ignored. It's
// the same map the cutoffs below were measured on.
var ClassFamilies = map[string]Family{
	"person":     Person,
	"bicycle":    Vehicle,
	"car":        Vehicle,
	"motorcycle": Vehicle,
	"bus":        Vehicle,
	"truck":      Vehicle,
	"train":      Vehicle,
	"cat":        Animal,
	"dog":        Animal,
	"horse":      Animal,
	"sheep":      Animal,
	"cow":        Animal,
}

// MinScore is the lowest confidence the Detector returns. It sits WELL below
// the cutoff on purpose: the tracker learns from boxes below the cutoff, and
// couldn't if the object detector already threw them away.
//
// Measured: a scarecrow, which the model mistakes for a person, comes out
// above 0.35 in 30% of daytime frames, and above 0.20 in 70%. Learning only
// from what passes the cutoff would leave the tracker blind to half of its
// appearances.
const MinScore = 0.20

// FrameWidth is the width, in pixels, of the frame JPEG the object detector
// returns with the detections - what the Detections screen shows as a
// thumbnail.
//
// 480 because it's twice the largest thumbnail the grid uses (240 px on a
// desktop screen with 5 columns), which keeps the image sharp on high-density
// screens and still serves the player sheet, which opens at 850. The height
// follows the camera's aspect ratio; the box is a fraction and depends on
// neither.
const FrameWidth = 480

// FrameQuality is the JPEG quality of the stored frame, from 1 to 100.
//
// Measured on 40 real frames from the test installation, at 480 px: 75 gives
// 13.8 KB on average and 15.8 KB at worst. On a day of 2,700 detections across
// nine cameras that's ~37 MB, and ~1.1 GB over 30 days of retention - against
// tens of GB of video per day. Those bytes count toward the camera's quota,
// like the video.
const FrameQuality = 75

// FamilyCutoff is the confidence from which a box can become a mark.
//
// 0.40, not 0.35: measured against hand-made labels, it wins on `pessoa` and
// `veiculo` and only loses on `animal`, the lowest-priority family.
var FamilyCutoff = map[Family]float64{
	Person:  0.40,
	Vehicle: 0.40,
	Animal:  0.40,
}

// MissesToRelease is how many CONSECUTIVE inferences without seeing an
// occupied place are enough to release it. It's the negative evidence that
// lets the next car, in the same street spot, arrive at a free place.
//
// 5 and 12 tie on `pessoa`; 12 loses `veiculo` arrivals on a camera facing the
// street.
const MissesToRelease = 5

// SamePlaceIoU is the overlap, between 0 and 1, above which two boxes of the
// same family are the same place.
//
// 0.7, not 0.5: with 0.5 a camera facing the street loses vehicle arrivals
// (one car behind the other in the same lane) in exchange for 1 fewer false
// `pessoa` per camera per day.
const SamePlaceIoU = 0.7

// DefaultMarker is what decides what becomes a mark on a newly added camera.
const DefaultMarker = "rastreio"

// --- the queue to the object detector ---------------------------------------
//
// The numbers below came from replaying three days of recordings from several
// cameras sharing a single dwnvr-detect.

// ClipsPerCamera is how many clips from ONE camera can wait their turn in the
// queue at the same time. The one being inferred has already left it and
// doesn't count. What arrives with the camera at its limit is dropped: it
// becomes just motion on the timeline.
//
// It's the queue's only limit, and it's what keeps a busy camera from taking
// the others' turn: it only loses what is ITS OWN. There's no fixed total -
// the queue's maximum size is ClipsPerCamera times the cameras with detection,
// and grows on its own with the installation. A total below that would do
// exactly what this limit exists to prevent: reject a quiet camera's mark
// because the busy ones filled the queue. RAM stays bounded, per camera: each
// clip is up to MaxGOPBytes, in practice ~230 KB on common cameras.
//
// Why 2, measured at 3.6 s per inference (two cores of an Orange Pi Zero 3):
// with 1 per camera, 3.8% dropped and some `pessoa` arrivals lost; with 2,
// 1.9% dropped, no `pessoa` lost, and p90 latency of 16 s against 13 s. With 3
// or 4 nothing more is gained, only latency: p99 of 40 s and 50 s against
// 30 s.
const ClipsPerCamera = 2

// InferenceTimeoutMs is how long the queue waits for an answer from the
// object detector before giving up on it. An inference takes ~6.5 s on one
// core of an Orange Pi Zero 3; ten times that is a stuck sidecar, not a slow
// one.
const InferenceTimeoutMs = 60_000

// InferencesInAverage is how many recent inferences the times shown on the
// Diagnostics screen come from - how long an inference takes and how long the
// clip waited its turn. Recent, not since dwnvr started: the question is "how
// is it now", and an average over days wouldn't move when the object detector
// got slow. With an inference every few seconds, 20 are the last minute or
// two.
const InferencesInAverage = 20

// PauseAfterFailureMs is how long the queue waits after a failed inference
// before sending the next one. A sidecar that's down must not turn into a
// tight reconnect loop; and while it's down the queue stays bounded, so
// recording feels nothing.
const PauseAfterFailureMs = 30_000

// There is NO REFRACTORY period. Measured in the same replay: a 10 s
// refractory period per camera loses 1% of `pessoa` arrivals at 6.5 s per
// inference and 11% at 3.4 s; 30 s loses a third. It drops exactly the second
// inference of a busy scene. What keeps the same event from firing twenty
// times is the Method's hysteresis.
