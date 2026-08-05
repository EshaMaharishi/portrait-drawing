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

	points := imageToPoints(img, 128, 10.0)

	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d: %v", len(points), points)
	}

	// Long side is 4px -> 2.5 inches per pixel. Height is 2px = 5in, so the
	// image is centered vertically with a 2.5in offset.
	want := [][2]float64{
		{1.25, 3.75}, // pixel (0,0): center at 0.5*2.5, 2.5 + 0.5*2.5
		{8.75, 6.25}, // pixel (3,1): center at 3.5*2.5, 2.5 + 1.5*2.5
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
	if points := imageToPoints(img, 128, 10.0); len(points) != 0 {
		t.Errorf("expected no points for an all-white image, got %v", points)
	}
}
