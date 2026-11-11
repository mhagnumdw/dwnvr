// Package area answers "is the object inside this area?": the area is a
// polygon drawn over a camera's image, and the object is the box the object
// detector returned.
//
// The area doesn't change what the detection records. It is a filter in
// Detections and, later, decides whether the alert goes out (see "O alerta" in
// docs/deteccao.md).
//
// Everything is a fraction of the frame, from 0 to 1, like the box: the area
// applies to the detection frame and to the segment keyframe, at any
// resolution, and is compared with the box without conversion.
package area

import (
	"errors"
	"fmt"
)

// Point is a vertex of the area: X and Y as a fraction of the frame, with Y
// growing downwards, as in the image.
type Point [2]float64

// Area is a simple polygon: the vertices in order, without repeating the first
// one at the end. It may be concave (an L).
type Area struct {
	Points []Point
}

// New checks the points and returns the area. The error gives the reason, so
// the API can send it back to the caller.
func New(points []Point) (Area, error) {
	if len(points) < MinAreaPoints {
		return Area{}, fmt.Errorf("a área precisa de pelo menos %d pontos, veio com %d", MinAreaPoints, len(points))
	}
	if len(points) > MaxAreaPoints {
		return Area{}, fmt.Errorf("a área aceita até %d pontos, veio com %d", MaxAreaPoints, len(points))
	}
	for i, p := range points {
		if p[0] < 0 || p[0] > 1 || p[1] < 0 || p[1] > 1 {
			return Area{}, fmt.Errorf("o ponto %d (%g, %g) está fora do quadro: as coordenadas vão de 0 a 1", i+1, p[0], p[1])
		}
	}
	return Area{Points: points}, nil
}

// Rule is the contract that decides when an object counts as inside the area.
// There are two, because the camera sees in perspective: someone close to it
// covers, in the image, the ground behind them.
//
// It keeps no state and doesn't allocate: the Detections calendar goes through
// every mark of every day.
type Rule interface {
	// Inside says whether the box [x1, y1, x2, y2], as a fraction of the
	// frame, counts as inside the area. The area's edge is inside.
	Inside(a Area, box [4]float64) bool

	// Name is the value in the JSON and in the query (`rule=`).
	Name() string
}

// Rules is the constructor map: a new rule is one more line. The key is the
// JSON value, in en-US.
var Rules = map[string]func() Rule{
	"touch": func() Rule { return touch{} },
	"foot":  func() Rule { return foot{} },
}

// ErrUnknownRule is a name that is not in Rules.
var ErrUnknownRule = errors.New("regra da área desconhecida")

// NewRule returns the rule by name, and DefaultRule when the name is empty.
// Unlike NovoMarcador, an unknown name is an error: it comes from the user (the
// query, the camera config), and silently falling back to the default would
// show a different list without saying so.
func NewRule(name string) (Rule, error) {
	if name == "" {
		name = DefaultRule
	}
	ctor, ok := Rules[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRule, name)
	}
	return ctor(), nil
}

// --- touch ------------------------------------------------------------------

// touch counts the box as inside when it and the area share any point. It's
// what one expects without thinking, and it catches an object half over the
// edge; in perspective, it also catches someone who only passes in front of
// the area.
type touch struct{}

func (touch) Name() string { return "touch" }

// Inside tests the three ways a rectangle and a polygon can touch: a vertex
// of the area inside the box (the area smaller than the box), a corner of the
// box inside the area (the box smaller than the area), or one edge crossing
// the other. Other than those, the two are apart.
func (touch) Inside(a Area, box [4]float64) bool {
	x1, y1, x2, y2 := box[0], box[1], box[2], box[3]
	for _, p := range a.Points {
		if p[0] >= x1-EdgeTolerance && p[0] <= x2+EdgeTolerance &&
			p[1] >= y1-EdgeTolerance && p[1] <= y2+EdgeTolerance {
			return true
		}
	}
	corners := [4]Point{{x1, y1}, {x2, y1}, {x2, y2}, {x1, y2}}
	for _, c := range corners {
		if contains(a, c) {
			return true
		}
	}
	n := len(a.Points)
	for i := range n {
		p, q := a.Points[i], a.Points[(i+1)%n]
		for j := range 4 {
			if segmentsIntersect(p, q, corners[j], corners[(j+1)%4]) {
				return true
			}
		}
	}
	return false
}

// --- foot -------------------------------------------------------------------

// foot counts the box as inside when the middle of its bottom edge, where
// feet and wheels are, is inside the area. The area is drawn on the ground,
// and someone in front of it, closer to the camera, doesn't count.
type foot struct{}

func (foot) Name() string { return "foot" }

// Inside clamps the foot to the frame before testing: the object detector
// returns boxes slightly past the edge (256 of 7,038 real boxes, from -0.029
// to 1.0115), and someone cut off at the bottom would have their foot outside
// even an area the size of the image.
func (foot) Inside(a Area, box [4]float64) bool {
	return contains(a, Point{clampToFrame((box[0] + box[2]) / 2), clampToFrame(box[3])})
}

// clampToFrame clamps the coordinate between 0 and 1.
func clampToFrame(v float64) float64 {
	return min(max(v, 0), 1)
}

// --- geometry ---------------------------------------------------------------

// contains says whether the point is inside the area or on its edge. The ray
// test (how many edges a ray to the right crosses) alone doesn't settle the
// edge: in an area the size of the image, the foot of someone cut off at the
// bottom lands on Y = 1, and would come out as outside.
func contains(a Area, pt Point) bool {
	n := len(a.Points)
	inside := false
	for i := range n {
		p, q := a.Points[i], a.Points[(i+1)%n]
		if onEdge(p, q, pt) {
			return true
		}
		if (p[1] > pt[1]) != (q[1] > pt[1]) &&
			pt[0] < p[0]+(pt[1]-p[1])*(q[0]-p[0])/(q[1]-p[1]) {
			inside = !inside
		}
	}
	return inside
}

// side is the cross product (q-p) x (r-p): positive on one side of line pq,
// negative on the other, close to zero on it.
func side(p, q, r Point) float64 {
	return (q[0]-p[0])*(r[1]-p[1]) - (q[1]-p[1])*(r[0]-p[0])
}

// inBoundingBox says whether r is in the rectangle with pq as its diagonal:
// together with side ≈ 0, that's r on segment pq.
func inBoundingBox(p, q, r Point) bool {
	return r[0] >= min(p[0], q[0])-EdgeTolerance && r[0] <= max(p[0], q[0])+EdgeTolerance &&
		r[1] >= min(p[1], q[1])-EdgeTolerance && r[1] <= max(p[1], q[1])+EdgeTolerance
}

// onEdge says whether r is on segment pq.
func onEdge(p, q, r Point) bool {
	return abs(side(p, q, r)) <= EdgeTolerance && inBoundingBox(p, q, r)
}

// segmentsIntersect says whether segments ab and cd share any point,
// including when they only touch or run over each other.
func segmentsIntersect(a, b, c, d Point) bool {
	d1, d2 := side(c, d, a), side(c, d, b)
	d3, d4 := side(a, b, c), side(a, b, d)
	if ((d1 > EdgeTolerance && d2 < -EdgeTolerance) || (d1 < -EdgeTolerance && d2 > EdgeTolerance)) &&
		((d3 > EdgeTolerance && d4 < -EdgeTolerance) || (d3 < -EdgeTolerance && d4 > EdgeTolerance)) {
		return true
	}
	return onEdge(c, d, a) || onEdge(c, d, b) || onEdge(a, b, c) || onEdge(a, b, d)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
