package detect

import (
	"math"
	"sort"
)

// Family groups the model's classes that the timeline colors. There are three
// because 9 colors can't be told apart on a 14 px icon; 3 can.
type Family string

const (
	Person  Family = "pessoa"
	Vehicle Family = "veiculo"
	Animal  Family = "animal"
)

// Families are the three, in priority order: `pessoa` is the one that matters
// most.
var Families = []Family{Person, Vehicle, Animal}

// Marker is the fourth contract: it decides, inference by inference, which of
// the object detector's findings become a MARK on the timeline.
//
// It exists because the timeline marks ARRIVAL, not presence. Without it, a
// parked car would light up `veiculo` on every onset - hundreds of times a day
// in a parking lot - and a scarecrow the model mistakes for a person would be
// a false `pessoa` on every movement in the scene. What sets them apart from
// an arrival is the box in the SAME PLACE, and only something that remembers
// the previous inferences can see that.
//
// That's why it has state, and why it lives in dwnvr and not in the sidecar:
// the object detector may restart, and dwnvr must not inherit that as a false
// mark.
//
// Not safe for concurrent use: each camera has its own, and the caller hands
// over its inferences one at a time, in the order they happened.
type Marker interface {
	// Mark takes everything the object detector found in one inference -
	// every box from MinScore up, of any class - and returns the ones that
	// become a mark.
	//
	// An inference with nothing must also come here: it's what counts as a
	// miss for the occupied places. An object detector that failed is not an
	// inference, and doesn't come.
	Mark(detections []Detection) []Detection

	Name() string
}

// Markers is the constructor map, twin of the other three.
var Markers = map[string]func() Marker{
	"nenhum":   func() Marker { return noMemory{} },
	"rastreio": func() Marker { return newTracker(MissesToRelease, SamePlaceIoU) },
}

// NewMarker returns a camera's marker. An unknown name falls back to the
// default, for the same reason as NewMethod.
func NewMarker(name string) Marker {
	ctor, ok := Markers[name]
	if !ok {
		ctor = Markers[DefaultMarker]
	}
	return ctor()
}

// markable says whether the box belongs to a family and passes its cutoff.
func markable(a Detection) (Family, bool) {
	f, ok := ClassFamilies[a.Class]
	return f, ok && a.Score >= FamilyCutoff[f]
}

// --- no memory --------------------------------------------------------------

// noMemory marks every family box that passes the cutoff. It's what you'd get
// with no tracking at all, and the reference the tracker was measured
// against.
type noMemory struct{}

func (noMemory) Name() string { return "nenhum" }

func (noMemory) Mark(detections []Detection) []Detection {
	var marks []Detection
	for _, a := range detections {
		if _, ok := markable(a); ok {
			marks = append(marks, a)
		}
	}
	return marks
}

// --- tracking occupied places -----------------------------------------------

// place is a spot in the frame where a family was seen and hasn't gone away.
type place struct {
	family Family
	box    [4]float64
	misses int
}

// tracker remembers the OCCUPIED places, per family. A box that falls on an
// occupied place doesn't mark and renews the place; a box on a free place
// marks, if it passes the cutoff, and occupies it - even below the cutoff. The
// place is released after `misses` consecutive inferences without being seen.
//
// The order of operations below is that of the version MissesToRelease and
// SamePlaceIoU were measured on, including details that seem not to matter:
// boxes by decreasing confidence, a place created in this inference can
// already be matched by the next box, and on an IoU tie the oldest place wins.
type tracker struct {
	misses   int
	minIoU   float64
	occupied []place
}

func newTracker(misses int, minIoU float64) *tracker {
	return &tracker{misses: misses, minIoU: minIoU}
}

func (r *tracker) Name() string { return "rastreio" }

func (r *tracker) Mark(detections []Detection) []Detection {
	type entry struct {
		a Detection
		f Family
	}
	var queue []entry
	for _, a := range detections {
		if f, ok := ClassFamilies[a.Class]; ok && a.Score >= MinScore {
			queue = append(queue, entry{a, f})
		}
	}
	sort.SliceStable(queue, func(i, j int) bool { return queue[i].a.Score > queue[j].a.Score })

	var marks []Detection
	seen := make([]bool, len(r.occupied), len(r.occupied)+len(queue))
	for _, c := range queue {
		matched, best := -1, 0.0
		for j, o := range r.occupied {
			if o.family != c.f {
				continue
			}
			if v := iou(c.a.Box, o.box); v > r.minIoU && (matched < 0 || v > best) {
				matched, best = j, v
			}
		}
		if matched >= 0 {
			r.occupied[matched].box, r.occupied[matched].misses = c.a.Box, 0
			seen[matched] = true
			continue
		}
		if c.a.Score >= FamilyCutoff[c.f] {
			marks = append(marks, c.a)
		}
		r.occupied = append(r.occupied, place{family: c.f, box: c.a.Box})
		seen = append(seen, true)
	}

	alive := r.occupied[:0]
	for j, o := range r.occupied {
		if !seen[j] {
			o.misses++
		}
		if o.misses < r.misses {
			alive = append(alive, o)
		}
	}
	r.occupied = alive
	return marks
}

// iou is the intersection over union of two boxes (x1, y1, x2, y2), from 0 to
// 1. It doesn't depend on the unit: frame fraction or pixels give the same
// number.
//
// Each product goes through float64() so it doesn't become a fused
// multiply-add, which the compiler does on arm64: the result would change in
// the last bit, and the same inference would make different decisions on
// amd64 and arm64 on a tie.
func iou(a, b [4]float64) float64 {
	ix := math.Max(0, math.Min(a[2], b[2])-math.Max(a[0], b[0]))
	iy := math.Max(0, math.Min(a[3], b[3])-math.Max(a[1], b[1]))
	i := float64(ix * iy)
	u := float64((a[2]-a[0])*(a[3]-a[1])) + float64((b[2]-b[0])*(b[3]-b[1])) - i
	if u <= 0 {
		return 0
	}
	return i / u
}
