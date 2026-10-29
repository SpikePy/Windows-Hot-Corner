// Package taskviewicon describes the glyph used for both the tray icon
// and the .exe file icon: the taskbar's Task View button - two overlapping
// rounded windows, an outlined one behind and a filled, accented one in
// front, with a thin gap between them. It is pure geometry with no OS
// dependency, so the tray (which paints it into a Windows bitmap) and
// tools/genicon (which renders it into an .ico) can share one definition,
// and it can be tested anywhere.
package taskviewicon

// GridSize is the canvas the glyph is defined on. Other sizes are derived
// by scaling in AtScaled.
const GridSize = 32

// Part identifies what the glyph covers at a given pixel.
type Part int

const (
	// PartNone is the transparent background.
	PartNone Part = iota
	// PartBack is the outline of the window behind.
	PartBack
	// PartFront is the filled window in front, in the accent colour.
	PartFront
)

// The two windows on the GridSize canvas: left, top, right, bottom
// (exclusive) and corner radius, in grid units.
type box struct{ l, t, r, b, radius float64 }

var (
	front = box{3, 12, 20, 29, 3}
	back  = box{12, 3, 29, 20, 3}
)

const (
	// outline is how thick the back window's frame is.
	outline = 3
	// gap is the transparent margin around the front window that cuts
	// the back window's frame where they overlap.
	gap = 2
)

// At returns the part at (x, y) on a GridSize x GridSize canvas.
func At(x, y int) Part { return AtScaled(x, y, GridSize) }

// AtScaled returns the part at (x, y) on a size x size canvas, sampling
// the shape at the pixel's centre so it scales to any resolution.
func AtScaled(x, y, size int) Part {
	if size <= 0 || x < 0 || y < 0 || x >= size || y >= size {
		return PartNone
	}
	scale := float64(GridSize) / float64(size)
	px, py := (float64(x)+0.5)*scale, (float64(y)+0.5)*scale

	if front.contains(px, py, 0) {
		return PartFront
	}
	if front.contains(px, py, gap) {
		return PartNone
	}
	if back.contains(px, py, 0) && !back.contains(px, py, -outline) {
		return PartBack
	}
	return PartNone
}

// contains reports whether (x, y) lies in the rounded box grown by
// grow units on every side (shrunk when grow is negative); the corner
// radius grows and shrinks with it.
func (bx box) contains(x, y, grow float64) bool {
	l, t, r, b := bx.l-grow, bx.t-grow, bx.r+grow, bx.b+grow
	radius := bx.radius + grow
	if radius < 0 {
		radius = 0
	}
	if x < l || x >= r || y < t || y >= b {
		return false
	}
	// Outside the rounded-off corners: measure from the centre of the
	// nearest corner's arc.
	cx, cy := x, y
	switch {
	case x < l+radius:
		cx = l + radius
	case x >= r-radius:
		cx = r - radius
	}
	switch {
	case y < t+radius:
		cy = t + radius
	case y >= b-radius:
		cy = b - radius
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}
