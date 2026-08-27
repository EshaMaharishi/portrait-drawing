// Package posesto3dscene implements a world state store service that exposes the
// drawing camera's poses as transforms, so they can be viewed in the
// visualizer on app.viam.com.
package posesto3dscene

import (
	"context"
	"errors"
	"fmt"
	"sync"

	commonpb "go.viam.com/api/common/v1"
	pb "go.viam.com/api/service/worldstatestore/v1"
	"google.golang.org/protobuf/types/known/structpb"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/worldstatestore"
)

// Model is the full model triplet for this service.
var Model = resource.NewModel("chess-piece-detection", "portrait-drawing", "poses-to-3d-scene")

const (
	// defaultMaxPoses caps how many transforms are stored per draw so the
	// visualizer is not overwhelmed; override per call with "max_poses".
	defaultMaxPoses = 2000
	// sphereRadiusMM is the radius of the sphere shown for each pose.
	sphereRadiusMM = 1.0
)

func init() {
	resource.RegisterService(worldstatestore.API, Model, resource.Registration[worldstatestore.Service, *Config]{
		Constructor: newPosesTo3DScene,
	})
}

// Config describes the attributes for the pose store.
type Config struct {
	// Camera is the name of the drawing camera whose get_poses command
	// supplies the poses. Required.
	Camera string `json:"camera"`
}

// Validate ensures the config is valid; camera is required.
func (c *Config) Validate(path string) ([]string, []string, error) {
	if c.Camera == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "camera")
	}
	return []string{c.Camera}, nil, nil
}

type posesTo3DScene struct {
	resource.Named
	resource.AlwaysRebuild

	logger logging.Logger
	cam    camera.Camera

	mu         sync.RWMutex
	transforms map[string]*commonpb.Transform

	changeChan chan worldstatestore.TransformChange
	streamCtx  context.Context
	cancel     context.CancelFunc
}

func newPosesTo3DScene(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (worldstatestore.Service, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}
	cam, err := camera.FromProvider(deps, cfg.Camera)
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	return &posesTo3DScene{
		Named:      conf.ResourceName().AsNamed(),
		logger:     logger,
		cam:        cam,
		transforms: make(map[string]*commonpb.Transform),
		changeChan: make(chan worldstatestore.TransformChange, 100),
		streamCtx:  streamCtx,
		cancel:     cancel,
	}, nil
}

// ListUUIDs returns all transform UUIDs currently in the store.
func (s *posesTo3DScene) ListUUIDs(ctx context.Context, extra map[string]any) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uuids := make([][]byte, 0, len(s.transforms))
	for _, transform := range s.transforms {
		uuids = append(uuids, transform.Uuid)
	}
	return uuids, nil
}

// GetTransform returns the transform for the given UUID.
func (s *posesTo3DScene) GetTransform(ctx context.Context, uuid []byte, extra map[string]any) (*commonpb.Transform, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transform, exists := s.transforms[string(uuid)]
	if !exists {
		return nil, errors.New("transform not found")
	}
	return transform, nil
}

// StreamTransformChanges returns a stream of transform changes.
func (s *posesTo3DScene) StreamTransformChanges(
	ctx context.Context,
	extra map[string]any,
) (*worldstatestore.TransformChangeStream, error) {
	return worldstatestore.NewTransformChangeStreamFromChannel(ctx, s.changeChan), nil
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "visualize"} - runs the camera's get_poses command and stores the
//	returned poses as world-frame transforms for the visualizer. Optional
//	"threshold" and "point_spacing_mm" are forwarded to the camera, and
//	"max_poses" (default 2000) caps how many poses are stored, sampled evenly.
//	{"command": "clear"} - removes all stored transforms.
func (s *posesTo3DScene) DoCommand(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "visualize":
		camCmd := map[string]any{"command": "get_poses"}
		for _, key := range []string{"threshold", "point_spacing_mm"} {
			if v, ok := cmd[key]; ok {
				camCmd[key] = v
			}
		}
		maxPoses := defaultMaxPoses
		if m, ok := cmd["max_poses"].(float64); ok {
			if m < 1 {
				return nil, fmt.Errorf("max_poses must be at least 1, got %v", m)
			}
			maxPoses = int(m)
		}

		resp, err := s.cam.DoCommand(ctx, camCmd)
		if err != nil {
			return nil, fmt.Errorf("camera get_poses command failed: %w", err)
		}
		rawPoses, ok := resp["poses"].([]any)
		if !ok {
			return nil, fmt.Errorf(`camera get_poses response has no "poses" list: %v`, resp)
		}

		total := len(rawPoses)
		sampled := samplePoses(rawPoses, maxPoses)
		if len(sampled) < total {
			s.logger.Infof("showing %d of %d poses; raise max_poses to show more", len(sampled), total)
		}

		transforms := make(map[string]*commonpb.Transform, len(sampled))
		for i, raw := range sampled {
			p, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unexpected pose format at index %d: %v", i, raw)
			}
			uuid := fmt.Sprintf("pose-%06d", i)
			transforms[uuid] = &commonpb.Transform{
				ReferenceFrame: uuid,
				PoseInObserverFrame: &commonpb.PoseInFrame{
					ReferenceFrame: "world",
					Pose: &commonpb.Pose{
						X:     asFloat(p["x"]),
						Y:     asFloat(p["y"]),
						Z:     asFloat(p["z"]),
						OX:    asFloat(p["o_x"]),
						OY:    asFloat(p["o_y"]),
						OZ:    asFloat(p["o_z"]),
						Theta: asFloat(p["theta"]),
					},
				},
				PhysicalObject: &commonpb.Geometry{
					GeometryType: &commonpb.Geometry_Sphere{
						Sphere: &commonpb.Sphere{RadiusMm: sphereRadiusMM},
					},
				},
				Uuid:     []byte(uuid),
				Metadata: poseMetadata,
			}
		}

		s.replaceTransforms(transforms)
		return map[string]any{
			"status": "poses stored",
			"shown":  len(transforms),
			"total":  total,
		}, nil
	case "clear":
		s.replaceTransforms(nil)
		return map[string]any{"status": "cleared"}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// Close stops the change stream.
func (s *posesTo3DScene) Close(ctx context.Context) error {
	s.cancel()
	close(s.changeChan)
	return nil
}

// replaceTransforms swaps the stored transforms for the given set, emitting
// removals for the old transforms and additions for the new ones.
func (s *posesTo3DScene) replaceTransforms(transforms map[string]*commonpb.Transform) {
	s.mu.Lock()
	old := s.transforms
	if transforms == nil {
		transforms = make(map[string]*commonpb.Transform)
	}
	s.transforms = transforms
	s.mu.Unlock()

	for _, t := range old {
		s.emitChange(t, pb.TransformChangeType_TRANSFORM_CHANGE_TYPE_REMOVED)
	}
	for _, t := range transforms {
		s.emitChange(t, pb.TransformChangeType_TRANSFORM_CHANGE_TYPE_ADDED)
	}
}

func (s *posesTo3DScene) emitChange(transform *commonpb.Transform, changeType pb.TransformChangeType) {
	change := worldstatestore.TransformChange{
		ChangeType: changeType,
		Transform:  transform,
	}
	select {
	case s.changeChan <- change:
	case <-s.streamCtx.Done():
	default:
		// Channel is full, skip this update; readers can re-list UUIDs.
	}
}

// samplePoses returns at most max poses, sampled evenly from the input.
func samplePoses(poses []any, max int) []any {
	if len(poses) <= max {
		return poses
	}
	sampled := make([]any, 0, max)
	for i := range max {
		sampled = append(sampled, poses[i*len(poses)/max])
	}
	return sampled
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

// poseMetadata renders each pose as an opaque black dot.
var poseMetadata = &structpb.Struct{
	Fields: map[string]*structpb.Value{
		"color": {
			Kind: &structpb.Value_StructValue{
				StructValue: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"r": {Kind: &structpb.Value_NumberValue{NumberValue: 0}},
						"g": {Kind: &structpb.Value_NumberValue{NumberValue: 0}},
						"b": {Kind: &structpb.Value_NumberValue{NumberValue: 0}},
					},
				},
			},
		},
		"opacity": {Kind: &structpb.Value_NumberValue{NumberValue: 1.0}},
	},
}
