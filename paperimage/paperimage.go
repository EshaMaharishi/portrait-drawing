// Package paperimage renders a sheet of paper with its margin and the dots of
// a drawing on it, for previews and progress images.
package paperimage

import (
	"image"
	"image/color"
	"math"
)

// PxPerMM is the render resolution.
const PxPerMM = 4.0

// Dot is one drawing dot in the paper's frame: U and V are millimeters from
// the drawing area's top-left corner (the paper inset by the margin). Done
// dots render black, the rest gray.
type Dot struct {
	U, V float64
	Done bool
}

// Paper describes the sheet: WidthMM across the image, HeightMM down it,
// MarginMM on every side, and SpacingMM the dot pitch (dot radius is half).
type Paper struct {
	WidthMM, HeightMM, MarginMM, SpacingMM float64
}

var (
	white   = color.Gray{Y: 255}
	outline = color.Gray{Y: 40}
	margin  = color.Gray{Y: 200}
	done    = color.Gray{Y: 0}
	pending = color.Gray{Y: 153}
)

// Render draws the paper outline, the margin box and the dots.
func Render(p Paper, dots []Dot) *image.Gray {
	w := int(math.Ceil(p.WidthMM * PxPerMM))
	h := int(math.Ceil(p.HeightMM * PxPerMM))
	canvas := image.NewGray(image.Rect(0, 0, w, h))
	for i := range canvas.Pix {
		canvas.Pix[i] = white.Y
	}
	drawRect(canvas, 0, 0, w-1, h-1, 3, outline)
	if m := int(math.Round(p.MarginMM * PxPerMM)); m > 0 {
		drawRect(canvas, m, m, w-1-m, h-1-m, 1, margin)
	}

	radius := p.SpacingMM * PxPerMM / 2
	if radius < 1 {
		radius = 1
	}
	r := int(math.Ceil(radius))
	offset := p.MarginMM * PxPerMM
	// Pending dots first so a completed dot is never hidden under one.
	for pass := 0; pass < 2; pass++ {
		for _, d := range dots {
			if d.Done != (pass == 1) {
				continue
			}
			c := pending
			if d.Done {
				c = done
			}
			cx, cy := offset+d.U*PxPerMM, offset+d.V*PxPerMM
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					x, y := int(cx)+dx, int(cy)+dy
					if x < 0 || x >= w || y < 0 || y >= h {
						continue
					}
					fx, fy := float64(x)+0.5-cx, float64(y)+0.5-cy
					if fx*fx+fy*fy <= radius*radius {
						canvas.SetGray(x, y, c)
					}
				}
			}
		}
	}
	return canvas
}

// drawRect strokes the rectangle with corners (x0,y0)-(x1,y1) inclusive,
// thick pixels wide, growing inward.
func drawRect(canvas *image.Gray, x0, y0, x1, y1, thick int, c color.Gray) {
	for t := 0; t < thick; t++ {
		for x := x0 + t; x <= x1-t; x++ {
			canvas.SetGray(x, y0+t, c)
			canvas.SetGray(x, y1-t, c)
		}
		for y := y0 + t; y <= y1-t; y++ {
			canvas.SetGray(x0+t, y, c)
			canvas.SetGray(x1-t, y, c)
		}
	}
}
