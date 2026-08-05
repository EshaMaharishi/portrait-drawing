package imagetoposes

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestImageToPoints(t *testing.T) {
	// 4x2 image: black pixels at (0,0) and (3,1), everything else white.
	img := image.NewGray(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(3, 1, color.Gray{Y: 0})

	// 63.5mm spacing matches the pixel size, so cells map 1:1 to pixels.
	points := imageToPoints(img, 128, 254.0, 254.0, 63.5)

	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d: %v", len(points), points)
	}

	// Long side is 4px -> 63.5mm per pixel. Height is 2px = 127mm, so the
	// image is centered vertically with a 63.5mm offset.
	want := [][2]float64{
		{31.75, 95.25},   // pixel (0,0): center at 0.5*63.5, 63.5 + 0.5*63.5
		{222.25, 158.75}, // pixel (3,1): center at 3.5*63.5, 63.5 + 1.5*63.5
	}
	for i, p := range points {
		if p != want[i] {
			t.Errorf("point %d: got %v, want %v", i, p, want[i])
		}
	}
}

func TestImageToPointsNearestNeighborOrder(t *testing.T) {
	// 4x3 image with black pixels at (0,0), (1,0), (0,2), and (3,2).
	img := image.NewGray(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 0})
	img.SetGray(0, 2, color.Gray{Y: 0})
	img.SetGray(3, 2, color.Gray{Y: 0})

	points := imageToPoints(img, 128, 254.0, 254.0, 63.5)

	// 63.5mm per pixel; height is 3px = 190.5mm, so yOffset is 31.75mm.
	// The walk starts at (0,0) (closest to the top-left corner), then its
	// neighbor (1,0) one cell away, then (0,2) (sqrt(5) cells from (1,0),
	// closer than (3,2) at sqrt(8)), then (3,2).
	want := [][2]float64{
		{31.75, 63.5},
		{95.25, 63.5},
		{31.75, 190.5},
		{222.25, 190.5},
	}
	if len(points) != len(want) {
		t.Fatalf("expected %d points, got %d: %v", len(want), len(points), points)
	}
	for i, p := range points {
		if p != want[i] {
			t.Errorf("point %d: got %v, want %v", i, p, want[i])
		}
	}
}

func TestFitPlaneExact(t *testing.T) {
	// Three points on the plane z = 0.01*x - 0.02*y + 300.
	points := [][]float64{
		{150, -127, 0.01*150 - 0.02*-127 + 300},
		{450, -127, 0.01*450 - 0.02*-127 + 300},
		{150, 123, 0.01*150 - 0.02*123 + 300},
	}
	a, b, c, err := fitPlane(points)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const tol = 1e-9
	if math.Abs(a-0.01) > tol || math.Abs(b+0.02) > tol || math.Abs(c-300) > tol {
		t.Errorf("expected plane (0.01, -0.02, 300), got (%v, %v, %v)", a, b, c)
	}
}

func TestFitPlaneLeastSquares(t *testing.T) {
	// Four points on z = 302 with +/-0.5mm probing noise on two of them; the
	// fit should land near flat z = 302.
	points := [][]float64{
		{100, 100, 302.5},
		{400, 100, 301.5},
		{100, 300, 302},
		{400, 300, 302},
	}
	a, b, c, err := fitPlane(points)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range []([2]float64){{250, 200}, {100, 100}, {400, 300}} {
		z := a*p[0] + b*p[1] + c
		if math.Abs(z-302) > 0.5 {
			t.Errorf("fitted z at (%v, %v) = %v, want within 0.5 of 302", p[0], p[1], z)
		}
	}
}

func TestFitPlaneCollinear(t *testing.T) {
	points := [][]float64{
		{100, 100, 300},
		{200, 200, 301},
		{300, 300, 302},
	}
	if _, _, _, err := fitPlane(points); err == nil {
		t.Fatal("expected an error for collinear points")
	}
}

func TestRenderPoints(t *testing.T) {
	// 100mm x 50mm area at 4px/mm gives a 400x200 canvas. One point at
	// (25, 25) with 2mm spacing should paint a dot at pixel (100, 100).
	img := renderPoints([][2]float64{{25, 25}}, 100, 50, 2)

	bounds := img.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 200 {
		t.Fatalf("expected 400x200 canvas, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	if c := color.GrayModel.Convert(img.At(100, 100)).(color.Gray); c.Y != 0 {
		t.Errorf("expected black at the point center, got gray %d", c.Y)
	}
	if c := color.GrayModel.Convert(img.At(0, 0)).(color.Gray); c.Y != 255 {
		t.Errorf("expected white background at (0,0), got gray %d", c.Y)
	}
	if c := color.GrayModel.Convert(img.At(300, 100)).(color.Gray); c.Y != 255 {
		t.Errorf("expected white far from the point, got gray %d", c.Y)
	}
}

func TestImageToPointsAllWhite(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	if points := imageToPoints(img, 128, 254.0, 254.0, 63.5); len(points) != 0 {
		t.Errorf("expected no points for an all-white image, got %v", points)
	}
}

func TestImageToPointsDownsamples(t *testing.T) {
	// 4x2 image with 127mm spacing: the grid is 2x1 cells of 2x2 pixels each.
	// Left cell has two black pixels (average 127.5 <= 128, kept); right cell
	// is all white (dropped).
	img := image.NewGray(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 0})

	points := imageToPoints(img, 128, 254.0, 254.0, 127.0)

	// The image spans 254mm x 127mm, so yOffset is 63.5mm. The kept cell's
	// center is at (0.5*127, 63.5 + 0.5*127).
	want := [2]float64{63.5, 127.0}
	if len(points) != 1 || points[0] != want {
		t.Fatalf("expected exactly [%v], got %v", want, points)
	}
}
