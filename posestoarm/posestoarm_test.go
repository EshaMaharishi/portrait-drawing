package posestoarm

import (
	"bytes"
	"image/png"
	"testing"

	"go.viam.com/rdk/logging"

	"portrait-drawing/paperimage"
)

// TestDrawingImage checks the progress image marks completed dots black,
// pending ones gray, and is cached per completed count.
func TestDrawingImage(t *testing.T) {
	s := &posesToArm{logger: logging.NewTestLogger(t), drawState: stateDrawing}
	if b, _, err := s.drawingImage(); err != nil || b != nil {
		t.Fatalf("no drawing: got %v bytes, err %v", len(b), err)
	}
	s.drawing = &drawing{
		paper:     paperimage.Paper{WidthMM: 100, HeightMM: 50, MarginMM: 10, SpacingMM: 2},
		dots:      []paperimage.Dot{{U: 25, V: 25}, {U: 50, V: 25}},
		poseIndex: []int{1, 3}, // pose 0 and 2 are hovers
		pngFor:    -1,
	}
	s.completed = 2 // pose 1 done, pose 3 not

	b, completed, err := s.drawingImage()
	if err != nil || completed != 2 {
		t.Fatalf("err %v completed %d", err, completed)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	gray := func(x, y int) uint8 { r, _, _, _ := img.At(x, y).RGBA(); return uint8(r >> 8) }
	if g := gray(140, 140); g != 0 {
		t.Errorf("completed dot: got %d, want 0", g)
	}
	if g := gray(240, 140); g != 153 {
		t.Errorf("pending dot: got %d, want 153", g)
	}

	// Same completed count: cached bytes are returned as-is.
	b2, _, _ := s.drawingImage()
	if !bytes.Equal(b, b2) || s.drawing.pngFor != 2 {
		t.Error("expected the cached image for the same completed count")
	}
	// Progress re-renders.
	s.completed = 4
	b3, completed, _ := s.drawingImage()
	if completed != 4 || bytes.Equal(b, b3) {
		t.Error("expected a new image once more dots are completed")
	}
}
