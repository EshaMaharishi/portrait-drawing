// Package tablesurface implements a generic service that calibrates the
// drawing surface's plane. The user jogs the arm until the marker touches
// the surface and records touch points; the service fits a plane to them
// and serves it to the image-to-poses camera. A flat mode is available for
// surfaces known to be level.
package tablesurface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.viam.com/rdk/components/arm"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
)

// Model is the full model triplet for this service.
var Model = resource.NewModel("esha", "portrait-drawing", "table-surface")

func init() {
	resource.RegisterService(generic.API, Model, resource.Registration[resource.Resource, *Config]{
		Constructor: newTableSurface,
	})
}

// Config describes the attributes for the table surface calibration service.
type Config struct {
	// Arm is the name of the arm whose end position is recorded by the
	// record_point command. Required.
	Arm string `json:"arm"`
}

// Validate ensures the config is valid; arm is required.
func (c *Config) Validate(path string) ([]string, []string, error) {
	if c.Arm == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "arm")
	}
	return []string{c.Arm}, nil, nil
}

// state is the calibration state, persisted to disk so it survives restarts.
type state struct {
	// Points are the probed [x, y, z] touch points in millimeters.
	Points [][]float64 `json:"points"`
	// FlatZMM, when set, overrides the probed points with a flat surface at
	// this height.
	FlatZMM *float64 `json:"flat_z_mm"`
}

type tableSurface struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger    logging.Logger
	arm       arm.Arm
	statePath string

	mu    sync.Mutex
	state state
}

func newTableSurface(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (resource.Resource, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}
	armRes, err := arm.FromDependencies(deps, cfg.Arm)
	if err != nil {
		return nil, err
	}

	s := &tableSurface{
		Named:     conf.ResourceName().AsNamed(),
		logger:    logger,
		arm:       armRes,
		statePath: statePath(conf.ResourceName().Name),
	}
	if err := s.loadState(); err != nil {
		logger.Warnf("could not load saved calibration state, starting empty: %v", err)
	}
	return s, nil
}

// statePath returns the file the calibration state is persisted to. The
// module data directory is provided by viam-server via VIAM_MODULE_DATA;
// fall back to the working directory when running outside viam-server.
func statePath(resourceName string) string {
	dir := os.Getenv("VIAM_MODULE_DATA")
	if dir == "" {
		dir = "."
	}
	safe := strings.ReplaceAll(resourceName, string(filepath.Separator), "_")
	return filepath.Join(dir, "table-surface-"+safe+".json")
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "record_point"} - records the arm's current end position as a
//	touch point on the surface. Jog the arm until the marker just touches the
//	surface before calling. Recording a point switches out of flat mode.
//	{"command": "undo"} - removes the most recently recorded point.
//	{"command": "clear"} - removes all recorded points and unsets flat mode.
//	{"command": "set_flat", "z_mm": 300} - uses a flat surface at the given
//	height instead of probed points.
//	{"command": "status"} - reports the mode, points, and fitted plane.
//	{"command": "get_plane"} - returns the plane coefficients {a, b, c} such
//	that z = a*x + b*y + c; errors if the surface is not calibrated yet.
func (s *tableSurface) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "record_point":
		pose, err := s.arm.EndPosition(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get arm end position: %w", err)
		}
		pt := pose.Point()

		s.mu.Lock()
		s.state.Points = append(s.state.Points, []float64{pt.X, pt.Y, pt.Z})
		s.state.FlatZMM = nil
		err = s.saveStateLocked()
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		s.logger.Infof("recorded touch point (%.2f, %.2f, %.2f)", pt.X, pt.Y, pt.Z)

		resp := s.status()
		resp["recorded"] = []interface{}{pt.X, pt.Y, pt.Z}
		return resp, nil
	case "undo":
		s.mu.Lock()
		if len(s.state.Points) == 0 {
			s.mu.Unlock()
			return nil, errors.New("no points to undo")
		}
		s.state.Points = s.state.Points[:len(s.state.Points)-1]
		err := s.saveStateLocked()
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return s.status(), nil
	case "clear":
		s.mu.Lock()
		s.state = state{}
		err := s.saveStateLocked()
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return s.status(), nil
	case "set_flat":
		z, ok := cmd["z_mm"].(float64)
		if !ok {
			return nil, errors.New(`set_flat requires a "z_mm" number`)
		}
		s.mu.Lock()
		s.state.FlatZMM = &z
		err := s.saveStateLocked()
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return s.status(), nil
	case "status":
		return s.status(), nil
	case "get_plane":
		a, b, c, _, err := s.plane()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"a": a, "b": b, "c": c}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// plane returns the calibrated plane coefficients and the mode they came
// from ("flat" or "probed").
func (s *tableSurface) plane() (a, b, c float64, mode string, err error) {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()

	if st.FlatZMM != nil {
		return 0, 0, *st.FlatZMM, "flat", nil
	}
	if len(st.Points) < 3 {
		return 0, 0, 0, "uncalibrated",
			fmt.Errorf("surface not calibrated: have %d of at least 3 points; record touch points with record_point or use set_flat", len(st.Points))
	}
	a, b, c, err = fitPlane(st.Points)
	if err != nil {
		return 0, 0, 0, "uncalibrated", fmt.Errorf("recorded points do not determine a plane: %w", err)
	}
	return a, b, c, "probed", nil
}

// status builds the response shared by the state-changing commands.
func (s *tableSurface) status() map[string]interface{} {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()

	points := make([]interface{}, len(st.Points))
	for i, p := range st.Points {
		points[i] = []interface{}{p[0], p[1], p[2]}
	}
	resp := map[string]interface{}{
		"points": points,
		"count":  len(st.Points),
	}

	a, b, c, mode, err := s.plane()
	resp["mode"] = mode
	if err != nil {
		resp["plane_error"] = err.Error()
		return resp
	}
	resp["plane"] = map[string]interface{}{"a": a, "b": b, "c": c}
	if mode == "probed" {
		maxResidual := 0.0
		for _, p := range st.Points {
			if r := a*p[0] + b*p[1] + c - p[2]; r > maxResidual {
				maxResidual = r
			} else if -r > maxResidual {
				maxResidual = -r
			}
		}
		resp["max_residual_mm"] = maxResidual
	}
	return resp
}

func (s *tableSurface) loadState() error {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
	s.logger.Infof("loaded calibration state from %s: %d points, flat=%v", s.statePath, len(st.Points), st.FlatZMM != nil)
	return nil
}

// saveStateLocked persists the state; s.mu must be held.
func (s *tableSurface) saveStateLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.statePath, data, 0o644); err != nil {
		return fmt.Errorf("failed to save calibration state to %s: %w", s.statePath, err)
	}
	return nil
}

// fitPlane least-squares fits a plane z = a*x + b*y + c to the given
// [x, y, z] points. With exactly 3 non-collinear points the fit is exact.
// It errors if the points are collinear (or nearly so), since they then do
// not determine a plane.
func fitPlane(points [][]float64) (a, b, c float64, err error) {
	// Solve the normal equations of minimizing sum((a*x + b*y + c - z)^2):
	//   [sxx sxy sx] [a]   [sxz]
	//   [sxy syy sy] [b] = [syz]
	//   [sx  sy  n ] [c]   [sz ]
	var sx, sy, sz, sxx, syy, sxy, sxz, syz float64
	for _, p := range points {
		x, y, z := p[0], p[1], p[2]
		sx += x
		sy += y
		sz += z
		sxx += x * x
		syy += y * y
		sxy += x * y
		sxz += x * z
		syz += y * z
	}
	n := float64(len(points))

	det := sxx*(syy*n-sy*sy) - sxy*(sxy*n-sy*sx) + sx*(sxy*sy-syy*sx)
	// Normalize the determinant by the matrix's scale so the collinearity
	// check does not depend on the units of the inputs.
	scale := math.Cbrt((sxx + 1) * (syy + 1) * n)
	if math.Abs(det) < 1e-9*scale*scale*scale {
		return 0, 0, 0, errors.New("points are collinear and do not determine a plane")
	}

	detA := sxz*(syy*n-sy*sy) - sxy*(syz*n-sy*sz) + sx*(syz*sy-syy*sz)
	detB := sxx*(syz*n-sz*sy) - sxz*(sxy*n-sy*sx) + sx*(sxy*sz-syz*sx)
	detC := sxx*(syy*sz-syz*sy) - sxy*(sxy*sz-syz*sx) + sxz*(sxy*sy-syy*sx)
	return detA / det, detB / det, detC / det, nil
}
