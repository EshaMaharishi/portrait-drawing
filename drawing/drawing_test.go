package drawing

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

	points := imageToPoints(img, 128, 254.0, 254.0)

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

func TestImageToPointsSnakeOrder(t *testing.T) {
	// 4x3 image; row 1 is empty so it must not flip the sweep direction.
	// Row 0: black at x=0 and x=2. Row 2: black at x=1 and x=3.
	img := image.NewGray(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(2, 0, color.Gray{Y: 0})
	img.SetGray(1, 2, color.Gray{Y: 0})
	img.SetGray(3, 2, color.Gray{Y: 0})

	points := imageToPoints(img, 128, 254.0, 254.0)

	// 63.5mm per pixel; height is 3px = 190.5mm, so yOffset is 31.75mm.
	// Row 0 sweeps left to right, row 2 sweeps right to left.
	want := [][2]float64{
		{31.75, 63.5},   // row 0, x=0
		{158.75, 63.5},  // row 0, x=2
		{222.25, 190.5}, // row 2, x=3 (reversed)
		{95.25, 190.5},  // row 2, x=1
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

func TestImageToPointsAllWhite(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	if points := imageToPoints(img, 128, 254.0, 254.0); len(points) != 0 {
		t.Errorf("expected no points for an all-white image, got %v", points)
	}
}
