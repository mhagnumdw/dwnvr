package detect

import (
	"math"
	"testing"
)

// Boxes as a fraction of the frame, as the object detector returns them.
var (
	streetSpot  = [4]float64{0.10, 0.50, 0.30, 0.70}
	otherSpot   = [4]float64{0.60, 0.50, 0.80, 0.70}
	scarecrow   = [4]float64{0.30, 0.20, 0.34, 0.34}
	nearTheSpot = [4]float64{0.11, 0.51, 0.31, 0.71} // the same car, the box jittered
)

func detection(class string, score float64, box [4]float64) Detection {
	return Detection{Class: class, Score: score, Box: box}
}

// marked returns the classes that became a mark, in order.
func marked(m Marker, detections ...Detection) []string {
	var out []string
	for _, a := range m.Mark(detections) {
		out = append(out, a.Class)
	}
	return out
}

func TestTrackerDoesNotRemarkTheParkedCar(t *testing.T) {
	m := NewMarker("rastreio")
	if got := marked(m, detection("car", 0.9, streetSpot)); len(got) != 1 {
		t.Fatalf("the arrival should mark: %v", got)
	}
	for i := range 50 {
		if got := marked(m, detection("car", 0.9, nearTheSpot)); len(got) != 0 {
			t.Fatalf("inference %d: the parked car marked again", i)
		}
	}
}

// TestTrackerFreesThePlaceWhenTheCarLeaves is what a time window can't do: the
// next car, in the same spot, is an arrival.
func TestTrackerFreesThePlaceWhenTheCarLeaves(t *testing.T) {
	m := NewMarker("rastreio")
	marked(m, detection("car", 0.9, streetSpot))

	// Four inferences without it: the place stays occupied.
	for range MissesToRelease - 1 {
		marked(m)
	}
	if got := marked(m, detection("car", 0.9, streetSpot)); len(got) != 0 {
		t.Fatal("released the place before MissesToRelease inferences")
	}

	// Five in a row without it: free.
	for range MissesToRelease {
		marked(m)
	}
	if got := marked(m, detection("car", 0.9, streetSpot)); len(got) != 1 {
		t.Fatal("the car that came back to the free spot should mark")
	}
}

// TestTrackerLearnsBelowTheCutoff: the scarecrow comes out sometimes at 0.30,
// sometimes at 0.45. If the tracker only learned from what passes the cutoff,
// the first appearance above it would become a false person.
func TestTrackerLearnsBelowTheCutoff(t *testing.T) {
	m := NewMarker("rastreio")
	if got := marked(m, detection("person", 0.30, scarecrow)); len(got) != 0 {
		t.Fatal("a box below the cutoff must not mark")
	}
	if got := marked(m, detection("person", 0.45, scarecrow)); len(got) != 0 {
		t.Fatal("the scarecrow was already known and marked anyway")
	}
	// Below the floor it isn't even seen: it doesn't renew the place.
	for range MissesToRelease {
		marked(m, detection("person", MinScore-0.01, scarecrow))
	}
	if got := marked(m, detection("person", 0.45, scarecrow)); len(got) != 1 {
		t.Fatal("a box below the floor renewed the place")
	}
}

func TestTrackerSeparatesPlacesAndFamilies(t *testing.T) {
	m := NewMarker("rastreio")
	got := marked(m,
		detection("car", 0.9, streetSpot),
		detection("car", 0.8, otherSpot),
		detection("chair", 0.9, scarecrow), // not in the families: doesn't even exist for the tracker
	)
	if len(got) != 2 {
		t.Fatalf("two cars in two places are two marks: %v", got)
	}
	// A person in the car's place is another family, and arrives.
	if got := marked(m, detection("person", 0.9, streetSpot)); len(got) != 1 {
		t.Fatalf("a person in the car's place should mark: %v", got)
	}
}

func TestNoMemoryMarksEverythingAboveCutoff(t *testing.T) {
	m := NewMarker("nenhum")
	for range 3 {
		got := marked(m,
			detection("car", 0.9, streetSpot),
			detection("person", 0.39, scarecrow),
			detection("chair", 0.9, otherSpot),
		)
		if len(got) != 1 || got[0] != "car" {
			t.Fatalf("expected only the car, on every inference: %v", got)
		}
	}
}

func TestNewMarkerFallsBackToDefault(t *testing.T) {
	if got := NewMarker("nao-existe").Name(); got != DefaultMarker {
		t.Errorf("an unknown name gave %q, expected %q", got, DefaultMarker)
	}
}

func TestIoUDoesNotDependOnUnit(t *testing.T) {
	inPixels := func(c [4]float64) [4]float64 { return [4]float64{c[0] * 640, c[1] * 360, c[2] * 640, c[3] * 360} }
	f := iou(streetSpot, nearTheSpot)
	p := iou(inPixels(streetSpot), inPixels(nearTheSpot))
	if math.Abs(f-p) > 1e-12 || f < SamePlaceIoU {
		t.Errorf("IoU as a fraction %v, in pixels %v", f, p)
	}
	if got := iou(streetSpot, otherSpot); got != 0 {
		t.Errorf("separate boxes gave IoU %v", got)
	}
	if got := iou(streetSpot, streetSpot); got != 1 {
		t.Errorf("the same box gave IoU %v", got)
	}
}
