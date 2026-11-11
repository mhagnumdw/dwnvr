package area

// This file holds every number in the package, each with its reason next to
// it.

const (
	// DefaultRule is the rule for those who don't pick one: `foot`. In the real
	// cases from 10/16 to 10/20/2026, 20% to 43% of what `touch` showed were
	// people who only appeared over the area, close to the camera, or a false
	// positive sitting still on the edge. The price is drawing the area over
	// all the ground where a foot can be.
	DefaultRule = "foot"

	// MinAreaPoints is the triangle: with fewer, there is no area.
	MinAreaPoints = 3

	// MaxAreaPoints leaves room for a hand-drawn outline along a backyard
	// floor, and caps the size: 20 points in thousandths are ~160 characters
	// in the Detections URL, and eight areas of 20 points take ~3 KB of the
	// 16 KB that POST /api/cameras reads.
	MaxAreaPoints = 20

	// EdgeTolerance is the distance below which a point counts as on the
	// edge. Coordinates arrive with 3 to 4 decimal places (thousandths in the
	// query, 4 places in the camera config), and float64 error in the math is
	// around 1e-16: the tolerance only absorbs that error, without fattening
	// the area.
	EdgeTolerance = 1e-9
)

// MaxAreasPerCamera caps the size of the camera config: eight areas of 20
// points take ~3 KB of the 16 KB that POST /api/cameras reads. It's more than
// a home camera has to watch (gate, garage, backyard, sidewalk).
const MaxAreasPerCamera = 8
