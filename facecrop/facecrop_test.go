package facecrop

import (
	"image"
	"image/color"
	"testing"

	pigo "github.com/esimov/pigo/core"
)

func TestCascadeLoads(t *testing.T) {
	if _, err := pigo.NewPigo().Unpack(facefinderCascade); err != nil {
		t.Fatalf("embedded cascade failed to load: %v", err)
	}
}

func TestPadBoxClampsToBounds(t *testing.T) {
	bounds := image.Rect(0, 0, 100, 80)
	got := padBox(image.Rect(10, 10, 30, 30), 0.5, bounds)
	if want := image.Rect(0, 0, 40, 40); got != want {
		t.Errorf("padBox: got %v, want %v", got, want)
	}
	got = padBox(image.Rect(80, 60, 100, 80), 0.5, bounds)
	if want := image.Rect(70, 50, 100, 80); got != want {
		t.Errorf("padBox at the far corner: got %v, want %v", got, want)
	}
}

func TestCropCopiesRegion(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 10, 10))
	img.SetGray(5, 5, color.Gray{Y: 0})
	out := crop(img, image.Rect(4, 4, 8, 8))
	if out.Bounds() != image.Rect(0, 0, 4, 4) {
		t.Fatalf("crop bounds: %v", out.Bounds())
	}
	if c := color.GrayModel.Convert(out.At(1, 1)).(color.Gray); c.Y != 0 {
		t.Errorf("expected the dark pixel at (1,1), got %d", c.Y)
	}
}

func TestNoFaceInBlankImage(t *testing.T) {
	det, err := pigo.NewPigo().Unpack(facefinderCascade)
	if err != nil {
		t.Fatal(err)
	}
	s := &faceCropCamera{detector: det, minFacePx: defaultMinFacePx, padding: defaultPadding}
	img := image.NewGray(image.Rect(0, 0, 320, 240))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	if _, found := s.findFace(img); found {
		t.Error("expected no face in a blank image")
	}
}
