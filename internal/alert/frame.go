package alert

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"slices"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// The alert frame is the inferred frame with what made the alert go out
// drawn on top: the box of each object that passed the rule, in the family's
// color, and the dashed outline of the alert areas. It's the same drawing
// Detections shows with the "only alerts" filter, without the boxes that
// weren't in the alert and without labels: Go's standard library has no font,
// and the message text already says each object and the areas it fell in.
//
// The Dispatcher draws it, once per alert, on the send goroutine, and every
// notifier gets the same frame: neither the object detector queue nor the
// notifier does this work.

// familyColors are the screen's (web/src/lib/icones.ts): the e-mail box has
// the color of the Detections box.
var familyColors = map[detect.Family]color.RGBA{
	detect.Person:  {0x3f, 0xb9, 0x50, 0xff},
	detect.Vehicle: {0xa3, 0x71, 0xf7, 0xff},
	detect.Animal:  {0xdb, 0x61, 0xa2, 0xff},
}

// unknownColor is that of a family the screen doesn't know, the same as there.
var unknownColor = color.RGBA{0x8b, 0x94, 0x9e, 0xff}

// areaColor is `--accent` from web/src/app.css, the color of the area
// outline on screen.
var areaColor = color.RGBA{0x2f, 0x81, 0xf7, 0xff}

// drawnFrame returns the JPEG of the alert frame with the boxes and areas
// drawn. The error is that of a frame that doesn't decode or encode: the
// caller decides to send the frame as it came.
func drawnFrame(a Alert) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(a.Frame))
	if err != nil {
		return nil, fmt.Errorf("the frame doesn't decode: %w", err)
	}
	// JPEG decodes to YCbCr with the color at half resolution; drawing on it
	// would blur the stroke. draw.Draw from YCbCr to RGBA has a fast path in the
	// standard library.
	b := src.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)

	for _, ar := range a.Areas {
		dashedOutline(img, ar.Points, areaColor)
	}
	// The reverse of the objects' order, which comes with the highest-priority
	// family first: its box ends up on top of the others.
	for _, o := range slices.Backward(a.Objects) {

		col, ok := familyColors[detect.Family(o.Family)]
		if !ok {
			col = unknownColor
		}
		strokeBox(img, o.Box, col)
	}

	// The drawn frame comes out at 1.4 to 1.5 times the size of the inferred
	// one (measured on 4 real frames): reserving twice avoids growing the buffer
	// midway.
	var out bytes.Buffer
	out.Grow(len(a.Frame) * 2)
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: FrameQuality}); err != nil {
		return nil, fmt.Errorf("the frame doesn't encode: %w", err)
	}
	return out.Bytes(), nil
}

// strokeBox draws the rectangle of the box [x1, y1, x2, y2], as a fraction
// of the frame, BoxPaddingPx away from the object and with the stroke inside,
// like the screen (caixas.ts). Near the edge the box stops at the frame: the
// padding only goes away on that side.
func strokeBox(img *image.RGBA, box [4]float64, col color.RGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	r := image.Rect(
		int(math.Floor(box[0]*float64(w)))-BoxPaddingPx,
		int(math.Floor(box[1]*float64(h)))-BoxPaddingPx,
		int(math.Ceil(box[2]*float64(w)))+BoxPaddingPx,
		int(math.Ceil(box[3]*float64(h)))+BoxPaddingPx,
	).Intersect(img.Rect)
	if r.Empty() {
		return
	}
	t := BoxStrokePx
	ink := image.NewUniform(col)
	// Four solid bands: a single-color draw.Draw over RGBA is a line copy, with
	// no per-pixel math.
	for _, band := range [...]image.Rectangle{
		image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), // top
		image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), // bottom
		image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), // left
		image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), // right
	} {
		draw.Draw(img, band.Intersect(r), ink, image.Point{}, draw.Src)
	}
}

// dashedOutline draws the area polygon, closed, in AreaDashOnPx dashes
// with AreaDashOffPx gaps, like the screen's setLineDash (area.ts). The dash
// pattern carries over from one side to the next without restarting at the
// vertex, as on the canvas.
func dashedOutline(img *image.RGBA, points []area.Point, col color.RGBA) {
	if len(points) < 2 {
		return
	}
	w, h := float64(img.Rect.Dx()), float64(img.Rect.Dy())
	period := float64(AreaDashOnPx + AreaDashOffPx)
	walked := 0.0
	for i, p := range points {
		q := points[(i+1)%len(points)]
		x0, y0 := p[0]*w, p[1]*h
		dx, dy := q[0]*w-x0, q[1]*h-y0
		side := math.Hypot(dx, dy)
		// One pixel per step: no hole in the stroke, and a whole-frame perimeter
		// at 480 px is about 1500 steps.
		for s := 0.0; s < side; s++ {
			if math.Mod(walked+s, period) < AreaDashOnPx {
				dot(img, x0+dx*s/side, y0+dy*s/side, col)
			}
		}
		walked += side
	}
}

// dot paints a square of AreaStrokePx side centered at (x, y), the area
// outline's brush. What falls outside the frame is left out.
func dot(img *image.RGBA, x, y float64, col color.RGBA) {
	x0 := int(math.Round(x - AreaStrokePx/2.0))
	y0 := int(math.Round(y - AreaStrokePx/2.0))
	r := image.Rect(x0, y0, x0+AreaStrokePx, y0+AreaStrokePx).Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, col)
		}
	}
}
