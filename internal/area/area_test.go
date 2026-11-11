package area

import (
	"errors"
	"testing"
)

// The areas, as a fraction of the frame.
var (
	// wholeImage is the area the size of the image: it must give the same as
	// no filter.
	wholeImage = []Point{{0, 0}, {1, 0}, {1, 1}, {0, 1}}

	// gate is a rectangle in the middle of the ground, from 0.4 to 0.6 in X
	// and from 0.5 to 0.7 in Y.
	gate = []Point{{0.4, 0.5}, {0.6, 0.5}, {0.6, 0.7}, {0.4, 0.7}}

	// lShape is concave: the bottom leg goes from X 0.1 to 0.9 and from Y 0.7
	// to 0.9, and the top one goes up the left side to Y 0.3. The inner corner
	// (X 0.3 to 0.9, Y 0.3 to 0.7) is outside.
	lShape = []Point{{0.1, 0.3}, {0.3, 0.3}, {0.3, 0.7}, {0.9, 0.7}, {0.9, 0.9}, {0.1, 0.9}}
)

func area(t *testing.T, points []Point) Area {
	t.Helper()
	a, err := New(points)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func rule(t *testing.T, name string) Rule {
	t.Helper()
	r, err := NewRule(name)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// check runs the cases against both rules: each case says what each one
// answers.
func check(t *testing.T, a Area, cases []struct {
	name        string
	box         [4]float64
	touch, foot bool
},
) {
	t.Helper()
	for _, c := range cases {
		if got := rule(t, "touch").Inside(a, c.box); got != c.touch {
			t.Errorf("%s: touch gave %v, want %v", c.name, got, c.touch)
		}
		if got := rule(t, "foot").Inside(a, c.box); got != c.foot {
			t.Errorf("%s: foot gave %v, want %v", c.name, got, c.foot)
		}
	}
}

// TestWholeImageMatchesNoFilter includes the boxes cut off at the image edges,
// the most common ones: someone close to the camera is cut off at the bottom,
// and the foot lands on Y = 1, on the area's edge.
func TestWholeImageMatchesNoFilter(t *testing.T) {
	check(t, area(t, wholeImage), []struct {
		name        string
		box         [4]float64
		touch, foot bool
	}{
		{"in the middle", [4]float64{0.4, 0.3, 0.5, 0.6}, true, true},
		{"cut off at the bottom", [4]float64{0.4, 0.5, 0.6, 1}, true, true},
		{"cut off on the left", [4]float64{0, 0.2, 0.1, 0.5}, true, true},
		{"cut off on the right", [4]float64{0.9, 0.2, 1, 0.5}, true, true},
		{"the whole image", [4]float64{0, 0, 1, 1}, true, true},
		// The object detector returns boxes slightly past the edge.
		{"past the bottom edge", [4]float64{0.4, 0.5, 0.6, 1.0015}, true, true},
		{"past the sides", [4]float64{-0.0293, 0.5, 1.0115, 0.9}, true, true},
	})
}

// TestPassingInFrontIsNotFoot is the reason for the two rules: a person close
// to the camera covers, in the image, the area behind them.
func TestPassingInFrontIsNotFoot(t *testing.T) {
	check(t, area(t, gate), []struct {
		name        string
		box         [4]float64
		touch, foot bool
	}{
		{"at the gate", [4]float64{0.45, 0.3, 0.55, 0.65}, true, true},
		{"in front, covering the gate", [4]float64{0.35, 0.2, 0.65, 0.95}, true, false},
		{"foot on the gate's bottom edge", [4]float64{0.45, 0.3, 0.55, 0.7}, true, true},
		{"far away", [4]float64{0.05, 0.05, 0.15, 0.2}, false, false},
		{"small box inside the gate", [4]float64{0.45, 0.55, 0.5, 0.6}, true, true},
		{"the gate inside the box, whose foot is below it", [4]float64{0.3, 0.4, 0.7, 0.8}, true, false},
	})
}

// TestBoxThatOnlyTouchesTheEdge: one shared point is already touching.
func TestBoxThatOnlyTouchesTheEdge(t *testing.T) {
	check(t, area(t, gate), []struct {
		name        string
		box         [4]float64
		touch, foot bool
	}{
		{"box's right edge on the gate's left", [4]float64{0.3, 0.55, 0.4, 0.65}, true, false},
		{"a box corner on the gate corner", [4]float64{0.6, 0.7, 0.7, 0.8}, true, false},
		{"one thousandth left of the gate", [4]float64{0.3, 0.55, 0.399, 0.65}, false, false},
		{"box bottom on the gate top", [4]float64{0.45, 0.3, 0.55, 0.5}, true, true},
		{"across the gate side to side, no corner inside", [4]float64{0.3, 0.55, 0.7, 0.6}, true, true},
		{"across the gate side to side, foot below", [4]float64{0.3, 0.55, 0.7, 0.75}, true, false},
	})
}

// TestConcaveArea: the inner corner of the L is outside, for both rules.
func TestConcaveArea(t *testing.T) {
	check(t, area(t, lShape), []struct {
		name        string
		box         [4]float64
		touch, foot bool
	}{
		{"in the bottom leg", [4]float64{0.5, 0.6, 0.6, 0.8}, true, true},
		{"in the top leg", [4]float64{0.15, 0.2, 0.25, 0.5}, true, true},
		{"in the inner corner", [4]float64{0.5, 0.35, 0.7, 0.6}, false, false},
		{"in the inner corner, foot on the bottom leg", [4]float64{0.5, 0.35, 0.7, 0.75}, true, true},
		{"over the inner corner, foot outside", [4]float64{0.25, 0.4, 0.4, 0.6}, true, false},
	})
}

func TestNewRejectsWithReason(t *testing.T) {
	tooMany := make([]Point, MaxAreaPoints+1)
	for _, c := range []struct {
		name   string
		points []Point
	}{
		{"two points", []Point{{0, 0}, {1, 1}}},
		{"too many points", tooMany},
		{"X above 1", []Point{{0, 0}, {1.2, 0}, {1, 1}}},
		{"negative Y", []Point{{0, 0}, {1, -0.1}, {1, 1}}},
	} {
		if _, err := New(c.points); err == nil {
			t.Errorf("%s: should be rejected", c.name)
		}
	}
}

func TestNewRule(t *testing.T) {
	if r := rule(t, ""); r.Name() != DefaultRule {
		t.Errorf("no name should give the default, %q; got %q", DefaultRule, r.Name())
	}
	for name := range Rules {
		if r := rule(t, name); r.Name() != name {
			t.Errorf("Rules[%q] returns a rule named %q", name, r.Name())
		}
	}
	if _, err := NewRule("pisa"); !errors.Is(err, ErrUnknownRule) {
		t.Errorf("a name not in the map should be ErrUnknownRule, got %v", err)
	}
}

// TestRulesDoNotAllocate: the Detections calendar goes through every mark of
// every day.
func TestRulesDoNotAllocate(t *testing.T) {
	a := area(t, lShape)
	box := [4]float64{0.25, 0.4, 0.4, 0.6}
	for name := range Rules {
		r := rule(t, name)
		if n := testing.AllocsPerRun(100, func() { r.Inside(a, box) }); n != 0 {
			t.Errorf("%s allocates %v times per box", name, n)
		}
	}
}
