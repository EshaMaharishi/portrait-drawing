package imagetoposes

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"go.viam.com/rdk/logging"
)

func TestImageToPoints(t *testing.T) {
	// 4x2 image: black pixels at (0,0) and (3,1), everything else white.
	img := image.NewGray(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(3, 1, color.Gray{Y: 0})

	// 63.5mm spacing matches the pixel size, so cells map 1:1 to pixels.
	points := imageToPoints(img, 128, 254.0, 254.0, 63.5, 1)

	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d: %v", len(points), points)
	}

	// Long side is 4px -> 63.5mm per pixel. Height is 2px = 127mm, so the
	// image is centered vertically with a 63.5mm offset.
	want := [][3]float64{
		{31.75, 95.25, 0},   // pixel (0,0): center at 0.5*63.5, 63.5 + 0.5*63.5
		{222.25, 158.75, 0}, // pixel (3,1): center at 3.5*63.5, 63.5 + 1.5*63.5
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

	points := imageToPoints(img, 128, 254.0, 254.0, 63.5, 1)

	// 63.5mm per pixel; height is 3px = 190.5mm, so yOffset is 31.75mm.
	// The walk starts at (0,0) (closest to the top-left corner), then its
	// neighbor (1,0) one cell away, then (0,2) (sqrt(5) cells from (1,0),
	// closer than (3,2) at sqrt(8)), then (3,2).
	want := [][3]float64{
		{31.75, 63.5, 0},
		{95.25, 63.5, 0},
		{31.75, 190.5, 0},
		{222.25, 190.5, 0},
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

func TestRotateImage(t *testing.T) {
	// 2x1 image: black at (0,0), white at (1,0).
	img := image.NewGray(image.Rect(0, 0, 2, 1))
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 255})

	grayAt := func(img image.Image, x, y int) uint8 {
		return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
	}

	// 90 clockwise: the black left pixel moves to the top.
	r90 := rotateImage(img, 90)
	if b := r90.Bounds(); b.Dx() != 1 || b.Dy() != 2 {
		t.Fatalf("expected 1x2 after 90, got %dx%d", b.Dx(), b.Dy())
	}
	if grayAt(r90, 0, 0) != 0 || grayAt(r90, 0, 1) != 255 {
		t.Errorf("after 90, expected black on top, got top=%d bottom=%d", grayAt(r90, 0, 0), grayAt(r90, 0, 1))
	}

	// 180: black moves to the right.
	r180 := rotateImage(img, 180)
	if grayAt(r180, 0, 0) != 255 || grayAt(r180, 1, 0) != 0 {
		t.Errorf("after 180, expected black on the right, got left=%d right=%d", grayAt(r180, 0, 0), grayAt(r180, 1, 0))
	}

	// 270 clockwise: black moves to the bottom.
	r270 := rotateImage(img, 270)
	if b := r270.Bounds(); b.Dx() != 1 || b.Dy() != 2 {
		t.Fatalf("expected 1x2 after 270, got %dx%d", b.Dx(), b.Dy())
	}
	if grayAt(r270, 0, 0) != 255 || grayAt(r270, 0, 1) != 0 {
		t.Errorf("after 270, expected black on the bottom, got top=%d bottom=%d", grayAt(r270, 0, 0), grayAt(r270, 0, 1))
	}

	// 0 returns the image unchanged.
	if r0 := rotateImage(img, 0); r0 != image.Image(img) {
		t.Error("expected 0 degrees to return the original image")
	}
}

func TestMirrorImage(t *testing.T) {
	// 2x1 image: black at (0,0), white at (1,0); mirrored, black moves right.
	img := image.NewGray(image.Rect(0, 0, 2, 1))
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 255})

	m := mirrorImage(img)
	left := color.GrayModel.Convert(m.At(0, 0)).(color.Gray).Y
	right := color.GrayModel.Convert(m.At(1, 0)).(color.Gray).Y
	if left != 255 || right != 0 {
		t.Errorf("expected black to move to the right, got left=%d right=%d", left, right)
	}
}

func TestRenderPaperPreview(t *testing.T) {
	// 100mm x 50mm paper with a 10mm margin at 4px/mm gives a 400x200 canvas.
	// One point at (25, 25) in the drawing area with 2mm spacing lands at
	// pixel (40+100, 40+100).
	img := renderPaperPreview([][3]float64{{25, 25}}, 100, 50, 10, 2)

	bounds := img.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 200 {
		t.Fatalf("expected 400x200 canvas, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	if c := color.GrayModel.Convert(img.At(140, 140)).(color.Gray); c.Y != 0 {
		t.Errorf("expected black at the point center, got gray %d", c.Y)
	}
	if c := color.GrayModel.Convert(img.At(0, 0)).(color.Gray); c.Y == 255 {
		t.Error("expected the paper outline at (0,0)")
	}
	if c := color.GrayModel.Convert(img.At(40, 100)).(color.Gray); c.Y == 255 || c.Y == 0 {
		t.Errorf("expected the light margin line at x=40, got gray %d", c.Y)
	}
	if c := color.GrayModel.Convert(img.At(300, 100)).(color.Gray); c.Y != 255 {
		t.Errorf("expected white far from the point, got gray %d", c.Y)
	}
}

func TestTransformMatchesLegacyRotate270Mirror(t *testing.T) {
	// The defaults (mirror, image_up "+x") must reproduce the previous
	// behaviour of rotating 270 degrees clockwise and then mirroring.
	img := image.NewGray(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			img.SetGray(x, y, color.Gray{Y: uint8(40*x + 100*y)})
		}
	}
	s := &imageToPosesCamera{imageUp: "+x", mirror: true}
	got := s.transform(img)
	want := mirrorImage(rotateImage(img, 270))
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds differ: %v vs %v", got.Bounds(), want.Bounds())
	}
	for y := 0; y < got.Bounds().Dy(); y++ {
		for x := 0; x < got.Bounds().Dx(); x++ {
			if grayAt(got, x, y) != grayAt(want, x, y) {
				t.Fatalf("pixel (%d,%d): got %d want %d", x, y, grayAt(got, x, y), grayAt(want, x, y))
			}
		}
	}
}

func TestDrawingArea(t *testing.T) {
	s := &imageToPosesCamera{paperXMM: 300, paperWMM: 279.4, paperHMM: 215.9, marginMM: 25.4}
	x0, y0, alongX, alongY := s.drawingArea()
	if x0 != 325.4 || math.Abs(y0-(-82.55)) > 1e-9 {
		t.Errorf("origin: got (%v, %v), want (325.4, -82.55)", x0, y0)
	}
	if math.Abs(alongX-228.6) > 1e-9 || math.Abs(alongY-165.1) > 1e-9 {
		t.Errorf("extent: got %v x %v, want 228.6 x 165.1", alongX, alongY)
	}
}

func TestImageToPointsDenseBlocks(t *testing.T) {
	// 4x4 all-black image with 63.5mm spacing gives a fully dark 4x4 grid.
	// With dense_block_size 2, each of the four full 2x2 blocks collapses to
	// its center cell (offset +1,+1 from the block corner).
	img := image.NewGray(image.Rect(0, 0, 4, 4))

	points := imageToPoints(img, 128, 254.0, 254.0, 63.5, 2)

	// Kept cells: (1,1), (3,1), (1,3), (3,3). The walk starts at (1,1); the
	// two-cell jumps to (1,3) and (3,1) tie, and the ring search checks rows
	// above/below before columns, so (1,3) wins, then (3,3), then (3,1).
	want := [][3]float64{
		{95.25, 95.25, 1},
		{95.25, 222.25, 1},
		{222.25, 222.25, 1},
		{222.25, 95.25, 1},
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

func TestImageToPointsDenseBlocksKeepsPartial(t *testing.T) {
	// 2x2 image where only three pixels are black: the block is not fully
	// dark, so dense_block_size 2 must leave all three dots in place.
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	img.SetGray(1, 1, color.Gray{Y: 255})

	points := imageToPoints(img, 128, 254.0, 254.0, 127.0, 2)
	if len(points) != 3 {
		t.Fatalf("expected 3 points for a partially dark block, got %d: %v", len(points), points)
	}
}

func TestImageToPointsAllWhite(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 3, 3))
	for y := range 3 {
		for x := range 3 {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	if points := imageToPoints(img, 128, 254.0, 254.0, 63.5, 1); len(points) != 0 {
		t.Errorf("expected no points for an all-white image, got %v", points)
	}
}

func TestImageToPointsDownsamples(t *testing.T) {
	// 4x2 image with 127mm spacing: the grid is 2x1 cells of 2x2 pixels each.
	// Left cell has two black pixels (average 127.5 <= 128, kept); right cell
	// is all white (dropped).
	img := image.NewGray(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 0})

	points := imageToPoints(img, 128, 254.0, 254.0, 127.0, 1)

	// The image spans 254mm x 127mm, so yOffset is 63.5mm. The kept cell's
	// center is at (0.5*127, 63.5 + 0.5*127).
	want := [3]float64{63.5, 127.0, 0}
	if len(points) != 1 || points[0] != want {
		t.Fatalf("expected exactly [%v], got %v", want, points)
	}
}

func grayAt(img image.Image, x, y int) uint8 {
	return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
}

func TestCropToContent(t *testing.T) {
	// 100x100 white image with a black 10x20 block at (40..49, 30..49).
	img := image.NewGray(image.Rect(0, 0, 100, 100))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := 30; y < 50; y++ {
		for x := 40; x < 50; x++ {
			img.SetGray(x, y, color.Gray{Y: 0})
		}
	}
	got := cropToContent(img, 128).Bounds()
	// Padding is 3% of the larger side (20px) -> 1px.
	want := image.Rect(39, 29, 51, 51)
	if got != want {
		t.Errorf("crop: got %v, want %v", got, want)
	}

	// The crop is clamped to the image.
	edge := image.NewGray(image.Rect(0, 0, 10, 10))
	for i := range edge.Pix {
		edge.Pix[i] = 255
	}
	edge.SetGray(0, 0, color.Gray{Y: 0})
	if got := cropToContent(edge, 128).Bounds(); got.Min != image.Pt(0, 0) {
		t.Errorf("clamped crop: got %v", got)
	}

	// All white: unchanged.
	blank := image.NewGray(image.Rect(0, 0, 10, 10))
	for i := range blank.Pix {
		blank.Pix[i] = 255
	}
	if cropToContent(blank, 128).Bounds() != blank.Bounds() {
		t.Error("expected a blank image to be returned unchanged")
	}
}

// TestGetPosesDarknessAlignment guards the invariant posestoarm relies on:
// darkness_levels is index-aligned with poses and the same length, so a dwell
// can be looked up by pose index. Only contact poses (at the surface height)
// may carry a non-zero level - a hover or home pose that picked one up would
// mean the two slices had drifted out of step.
func TestGetPosesDarknessAlignment(t *testing.T) {
	// 4x4 all-black PNG: with denseN 2 every 2x2 block is fully black and
	// collapses to one center dot with darkness 1.0.
	path := filepath.Join(t.TempDir(), "src.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	f.Close()

	const surfaceZ = 12.5
	cam := &imageToPosesCamera{
		logger:     logging.NewTestLogger(t),
		imagePath:  path,
		paperWMM:   254.0,
		paperHMM:   254.0,
		spacingMM:  63.5,
		threshold:  128,
		hoverMM:    5, // generates the leading/trailing home poses and hovers
		denseN:     2,
		surfaceZMM: surfaceZ,
	}

	resp, err := cam.DoCommand(context.Background(), map[string]any{"command": "get_poses"})
	if err != nil {
		t.Fatal(err)
	}
	poses, ok := resp["poses"].([]any)
	if !ok {
		t.Fatalf("no poses in response: %v", resp)
	}
	levels, ok := resp["darkness_levels"].([]float64)
	if !ok {
		t.Fatalf("no darkness_levels in response: %v", resp)
	}
	if len(levels) != len(poses) {
		t.Fatalf("darkness_levels has %d entries for %d poses", len(levels), len(poses))
	}

	dwells := 0
	for i, raw := range poses {
		p := raw.(map[string]any)
		contact := p["z"].(float64) == surfaceZ
		if levels[i] != 0 && !contact {
			t.Errorf("pose %d at z=%v is not a contact pose but has darkness %v",
				i, p["z"], levels[i])
		}
		if contact && levels[i] == 1 {
			dwells++
		}
	}
	if dwells != 4 {
		t.Errorf("expected 4 collapsed dots at darkness 1.0, got %d", dwells)
	}
}
