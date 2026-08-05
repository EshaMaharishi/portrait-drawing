package imagetoposes

import (
	"image"
	"image/color"
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
