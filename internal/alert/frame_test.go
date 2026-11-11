package alert

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/area"
)

// background is the test frame's color: dark, far from the box and area
// colors, including the unknown family's gray.
var background = color.RGBA{0x20, 0x20, 0x20, 0xff}

// testFrame is a 480×270 JPEG, the size of the inferred frame
// (detect.FrameWidth), at its quality. With noise, the frame costs as much to
// decode and encode as a camera frame; without it, it's a flat background, for
// the test to read each pixel's color.
func testFrame(t testing.TB, noise bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 480, 270))
	random := rand.New(rand.NewPCG(1, 2))
	for y := range 270 {
		for x := range 480 {
			c := background
			if noise {
				v := uint8(x*200/480 + random.IntN(40))
				c = color.RGBA{v, uint8(y * 200 / 270), v / 2, 0xff}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// decode reads the drawn frame back, as whoever opens the attachment would.
func decode(t *testing.T, frame []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// near says whether the pixel ended up closer to the color than to the
// background. JPEG shifts the color, even more on a 2 px stroke, because it
// keeps the color at half resolution: comparing with a fixed tolerance would
// make a fragile test. Measured on 10/27/2026, at the frame quality, the middle
// of a dash gap sits at 2,336 from the background, and the faintest stroke at
// 26,353: halfway separates the two comfortably.
func near(img image.Image, x, y int, col color.RGBA) bool {
	c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	return distance(c, col) < distance(c, background)
}

func distance(a, b color.RGBA) int {
	dr, dg, db := int(a.R)-int(b.R), int(a.G)-int(b.G), int(a.B)-int(b.B)
	return dr*dr + dg*dg + db*db
}

// TestBoxInTheFamilyColor: the box comes out BoxPaddingPx away from the
// object, with the stroke inside, in the family color; inside and outside it
// the frame stays as it was.
func TestBoxInTheFamilyColor(t *testing.T) {
	// The box goes from (120, 54) to (240, 216) on the 480×270 frame, and the
	// drawn one, from (115, 49) to (245, 221).
	a := Alert{Frame: testFrame(t, false),
		Objects: []Object{{Family: "pessoa", Box: [4]float64{0.25, 0.2, 0.5, 0.8}}}}
	frame, err := drawnFrame(a)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, frame)
	if img.Bounds() != image.Rect(0, 0, 480, 270) {
		t.Fatalf("the frame changed size: %v", img.Bounds())
	}
	green := familyColors["pessoa"]
	for _, p := range []image.Point{{180, 49}, {180, 220}, {115, 130}, {244, 130}} {
		if !near(img, p.X, p.Y, green) {
			t.Errorf("the stroke isn't at %v", p)
		}
	}
	// The padding: the edge of the object detector's box stays clear.
	for _, p := range []image.Point{{180, 54}, {120, 130}, {180, 130}, {100, 130}} {
		if near(img, p.X, p.Y, green) {
			t.Errorf("painted %v, outside the stroke", p)
		}
	}
}

// TestPriorityOnTop: with two boxes in the same place, the one of the
// highest-priority family, which comes first, ends up on top.
func TestPriorityOnTop(t *testing.T) {
	box := [4]float64{0.25, 0.2, 0.5, 0.8}
	a := Alert{Frame: testFrame(t, false), Objects: []Object{
		{Family: "pessoa", Box: box}, {Family: "veiculo", Box: box}}}
	frame, err := drawnFrame(a)
	if err != nil {
		t.Fatal(err)
	}
	c := color.RGBAModel.Convert(decode(t, frame).At(180, 49)).(color.RGBA)
	if distance(c, familyColors["pessoa"]) >= distance(c, familyColors["veiculo"]) {
		t.Errorf("%v ended up on top, not the person", c)
	}
}

// TestDashedOutline: the area outline comes out dashed: on the top line of
// a rectangle, the middle of each 6 px dash painted, and the middle of each
// 4 px gap not.
func TestDashedOutline(t *testing.T) {
	// The top line goes from x = 48 to x = 432, at y = 54.
	a := Alert{Frame: testFrame(t, false), Objects: []Object{},
		Areas: []AlertArea{{Name: "Portão", Points: []area.Point{{0.1, 0.2}, {0.9, 0.2}, {0.9, 0.8}, {0.1, 0.8}}}}}
	frame, err := drawnFrame(a)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, frame)
	on, off := 0, 0
	for x := 48; x < 48+10*20; x++ {
		if (x-48)%10 == 2 && !near(img, x, 54, areaColor) {
			on++
		}
		if (x-48)%10 == 7 && near(img, x, 54, areaColor) {
			off++
		}
	}
	if on > 0 || off > 0 {
		t.Errorf("in 20 dashes, %d without ink in the middle of the dash and %d painted in the middle of the gap", on, off)
	}
	if near(img, 240, 135, areaColor) {
		t.Error("painted inside the area")
	}
}

// TestBoxAtTheEdge: a box touching the edge stops at the frame, without a
// panic, and one outside the frame draws nothing.
func TestBoxAtTheEdge(t *testing.T) {
	a := Alert{Frame: testFrame(t, false), Objects: []Object{
		{Family: "animal", Box: [4]float64{0, 0, 0.1, 0.1}},
		{Family: "outra", Box: [4]float64{0.9, 0.9, 1, 1}},
		{Family: "pessoa", Box: [4]float64{1.2, 1.2, 1.5, 1.5}}}}
	frame, err := drawnFrame(a)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, frame)
	if !near(img, 0, 10, familyColors["animal"]) || !near(img, 479, 260, unknownColor) {
		t.Error("the edge box didn't stay on the frame")
	}
}

// TestFrameThatDoesNotDecode: a broken frame is an error, and the
// Dispatcher sends the frame as it came.
func TestFrameThatDoesNotDecode(t *testing.T) {
	a := alertaDe(1)
	if _, err := drawnFrame(a); err == nil {
		t.Fatal("drew on a frame that isn't a JPEG")
	}
	if got := NewDispatcher(nil, silent()).withDrawnFrame(a); string(got.Frame) != "jpeg" {
		t.Errorf("the frame became %q", got.Frame)
	}
}

// TestNotifierGetsTheDrawnFrame: the notifier gets the drawn frame, not the
// inferred one.
func TestNotifierGetsTheDrawnFrame(t *testing.T) {
	notifier := &fakeNotifier{arrived: make(chan Alert, 1)}
	c := NewDispatcher([]Notifier{notifier}, silent())
	ctx := t.Context()
	go c.Run(ctx)

	a := alertaDe(1)
	a.Frame = testFrame(t, false)
	a.Objects[0].Box = [4]float64{0.25, 0.2, 0.5, 0.8}
	c.Receive(a)
	got := <-notifier.arrived
	if bytes.Equal(got.Frame, a.Frame) {
		t.Fatal("the notifier got the inferred frame")
	}
	if !near(decode(t, got.Frame), 180, 49, familyColors["pessoa"]) {
		t.Error("the frame the notifier got doesn't have the box")
	}
}

// BenchmarkDrawnFrame measures drawing a typical alert, two people and one
// area, on a 480×270 frame with camera texture.
func BenchmarkDrawnFrame(b *testing.B) {
	a := Alert{Frame: testFrame(b, true),
		Objects: []Object{
			{Family: "pessoa", Box: [4]float64{0.25, 0.2, 0.35, 0.6}},
			{Family: "pessoa", Box: [4]float64{0.6, 0.3, 0.7, 0.9}}},
		Areas: []AlertArea{{Name: "Portão",
			Points: []area.Point{{0.1, 0.3}, {0.5, 0.25}, {0.8, 0.5}, {0.7, 0.95}, {0.15, 0.9}}}}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := drawnFrame(a); err != nil {
			b.Fatal(err)
		}
	}
}
