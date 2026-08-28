package posestoarm

import (
	"bytes"
	"context"
	"image/png"
	"testing"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/services/motion"
	"go.viam.com/rdk/testutils/inject"
	injectmotion "go.viam.com/rdk/testutils/inject/motion"

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

// fakePose is one fully dark contact pose, as get_poses would return it.
func fakePose(darkness float64) map[string]any {
	return map[string]any{
		"x": 0.0, "y": 0.0, "z": 0.0,
		"o_x": 0.0, "o_y": 0.0, "o_z": -1.0, "theta": 0.0,
		"linear": false, "darkness": darkness,
	}
}

// newTestService wires a posesToArm to a camera that returns n fully dark
// poses and a motion service that moves instantly.
func newTestService(t *testing.T, n int) (*posesToArm, *int) {
	t.Helper()
	poses := make([]any, n)
	for i := range poses {
		poses[i] = fakePose(1)
	}
	cam := &inject.Camera{}
	cam.DoFunc = func(ctx context.Context, cmd map[string]any) (map[string]any, error) {
		return map[string]any{"poses": poses, "count": n}, nil
	}
	moves := 0
	m := &injectmotion.MotionService{}
	m.MoveFunc = func(ctx context.Context, req motion.MoveReq) (bool, error) {
		moves++
		return true, nil
	}
	return &posesToArm{
		logger:  logging.NewTestLogger(t),
		cam:     cam,
		armName: "arm",
		motion:  m,
	}, &moves
}

// drawAndWait runs one draw command and blocks until it finishes.
func drawAndWait(t *testing.T, s *posesToArm, cmd map[string]any) time.Duration {
	t.Helper()
	start := time.Now()
	if _, err := s.DoCommand(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	s.drawWG.Wait()
	elapsed := time.Since(start)

	s.mu.Lock()
	state, lastErr := s.drawState, s.lastErr
	s.mu.Unlock()
	if state != stateComplete {
		t.Fatalf("draw ended in state %q (error: %q)", state, lastErr)
	}
	return elapsed
}

// The dwell is what shading controls, so the observable difference is time.
// 8 fully dark dots dwell 8*maxDwellSeconds with shading on and not at all
// with it off.
func TestDrawShadingControlsDwell(t *testing.T) {
	const dots = 8
	wantDwell := time.Duration(dots * maxDwellSeconds * float64(time.Second))

	t.Run("off", func(t *testing.T) {
		s, moves := newTestService(t, dots)
		elapsed := drawAndWait(t, s, map[string]any{"command": "draw", "shading": false})
		if *moves != dots {
			t.Errorf("moved to %d poses, want %d", *moves, dots)
		}
		if elapsed >= wantDwell {
			t.Errorf("draw took %v with shading off; expected well under the %v of dwell", elapsed, wantDwell)
		}
	})

	t.Run("on", func(t *testing.T) {
		s, moves := newTestService(t, dots)
		elapsed := drawAndWait(t, s, map[string]any{"command": "draw", "shading": true})
		if *moves != dots {
			t.Errorf("moved to %d poses, want %d", *moves, dots)
		}
		if elapsed < wantDwell {
			t.Errorf("draw took %v with shading on; expected at least the %v of dwell", elapsed, wantDwell)
		}
	})

	// An absent flag has to keep shading, so callers that predate it (and the
	// service's own defaults) are unchanged.
	t.Run("absent defaults to on", func(t *testing.T) {
		s, _ := newTestService(t, dots)
		elapsed := drawAndWait(t, s, map[string]any{"command": "draw"})
		if elapsed < wantDwell {
			t.Errorf("draw took %v with shading unspecified; expected at least the %v of dwell", elapsed, wantDwell)
		}
	})
}
