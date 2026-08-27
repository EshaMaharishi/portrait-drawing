package paperimage

import (
	"image"
	"testing"
)

func TestRenderColors(t *testing.T) {
	// 100x50mm paper, 10mm margin: 400x200 canvas; dots at (25,25) done and
	// (50,25) pending land at pixels (140,140) and (240,140).
	img := Render(Paper{WidthMM: 100, HeightMM: 50, MarginMM: 10, SpacingMM: 2},
		[]Dot{{U: 25, V: 25, Done: true}, {U: 50, V: 25}})
	if img.Bounds() != image.Rect(0, 0, 400, 200) {
		t.Fatalf("bounds %v", img.Bounds())
	}
	if c := img.GrayAt(140, 140).Y; c != 0 {
		t.Errorf("done dot: got %d, want 0", c)
	}
	if c := img.GrayAt(240, 140).Y; c != 153 {
		t.Errorf("pending dot: got %d, want 153", c)
	}
	if c := img.GrayAt(0, 0).Y; c == 255 {
		t.Error("expected the outline at (0,0)")
	}
	if c := img.GrayAt(40, 100).Y; c != 200 {
		t.Errorf("margin line: got %d, want 200", c)
	}
	if c := img.GrayAt(300, 100).Y; c != 255 {
		t.Errorf("background: got %d, want 255", c)
	}
}
