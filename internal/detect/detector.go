package detect

import "context"

// Detection is an object the object detector saw in a frame.
//
// The box is a FRACTION of the frame, not pixels, because that's the only form
// that still holds when the camera's resolution changes - and it does: some
// cameras' init advertises 2560x1440 while the stream delivers 1920x1080.
type Detection struct {
	Class string  `json:"classe"`
	Score float64 `json:"score"`
	// X1, Y1, X2, Y2 as a fraction of the frame, from 0 to 1.
	Box [4]float64 `json:"caixa"`
}

// Output is what the object detector returns from an inference: the
// detections and, when the frame was asked for, the frame itself as JPEG.
//
// The frame is the SAME one the model looked at - the clip's last one -, so
// the Detection box, being a fraction, lands in the right place over it at any
// size. It's what the Detections screen shows.
type Output struct {
	// Detections is every box from MinScore up. An empty slice is a result:
	// "this onset was inferred, and it was nothing".
	Detections []Detection

	// Frame is the JPEG of the inferred frame, already scaled down to
	// FrameWidth, or nil. It only comes when there was a detection: nobody
	// looks at the frame of an onset without an object, and storing it would
	// cost disk for nothing.
	Frame []byte
}

// Detector is the third contract: what looks at the video clip and says what
// was in its last frame.
//
// It's the only one of the contracts that is expensive - ~6.5 s wall time and
// 244 MB peak on an Orange Pi Zero 3 -, which is why it lives behind a trigger
// and in a separate container: decoding H264/HEVC and running a model require
// native code, and dwnvr stays pure Go, CGO_ENABLED=0, in an 8 MB image.
//
// IT HAS NO STATE, and doesn't decide what becomes a mark. It returns every
// box from MinScore up, of any class; the per-family cutoff and the memory of
// still objects belong to the Marker, in dwnvr. An object detector that
// restarts must not turn into a false mark on the timeline.
type Detector interface {
	// Infer sends the clip and returns what was in its LAST frame, which is
	// the frame the Method picked. An Output with empty Detections is a
	// result, not a missing one: it's what says "this onset was already
	// inferred, and it was nothing" - and it's what counts as a miss in the
	// tracker.
	Infer(ctx context.Context, p Clip) (Output, error)

	Name() string
}

// Detectors is the constructor map, twin of the others. The argument is the
// object detector's address, for those who have one; `nenhum` ignores it.
//
// `nenhum` is what makes motion detection work without any sidecar: the
// trigger marks motion on the timeline, and nobody infers.
var Detectors = map[string]func(url string) Detector{
	"nenhum":  func(string) Detector { return none{} },
	"sidecar": NewSidecar,
}

// none is the object detector that doesn't infer. It's not a stub: it's the
// correct behavior for someone who didn't install the sidecar, and what
// guarantees motion marking is usable on its own.
type none struct{}

func (none) Name() string { return "nenhum" }

func (none) Infer(context.Context, Clip) (Output, error) { return Output{}, nil }
