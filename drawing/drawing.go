// Package drawing implements a camera component that serves image.png and
// supports a "draw" DoCommand.
package drawing

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"time"

	"github.com/golang/geo/r3"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/motion"
	"go.viam.com/rdk/spatialmath"
	rdkutils "go.viam.com/rdk/utils"
)

// Model is the full model triplet for this camera.
var Model = resource.NewModel("esha", "portrait-drawing", "drawing")

const (
	// defaultImagePath is used when the image_path attribute is not set.
	defaultImagePath = "image.png"
	// defaultSizeMM is the width and height in millimeters of the area the
	// image is mapped onto when width_mm/height_mm are not set (254mm = 10in).
	defaultSizeMM = 254.0
	// defaultThreshold is the grayscale value (0-255) at or below which a pixel
	// is considered dark enough to draw.
	defaultThreshold = 128
)

func init() {
	resource.RegisterComponent(camera.API, Model, resource.Registration[camera.Camera, *Config]{
		Constructor: newDrawing,
	})
}

// Config describes the attributes for this camera.
type Config struct {
	// ImagePath is the path to the PNG file to serve; defaults to "image.png"
	// relative to the module's working directory.
	ImagePath string `json:"image_path"`
	// TopLeftXMM and TopLeftYMM are the position in millimeters of the
	// top-left corner of the drawing area; the image's [x, y] coordinates
	// (origin at the image's top-left) are offset by them. Required.
	TopLeftXMM *float64 `json:"top_left_x_mm"`
	TopLeftYMM *float64 `json:"top_left_y_mm"`
	// TopLeftZMM is the z value in millimeters used for the poses returned by
	// the draw command. Required.
	TopLeftZMM *float64 `json:"top_left_z_mm"`
	// SizeXMM and SizeYMM are the extent in millimeters of the drawing area
	// along the x and y axes; the image is scaled to fit inside it, preserving
	// aspect ratio, and centered. Both default to 254mm (10in).
	SizeXMM float64 `json:"size_x_mm"`
	SizeYMM float64 `json:"size_y_mm"`
	// Arm is the name of the arm the draw command moves through the cached
	// poses, using the motion service. Required.
	Arm string `json:"arm"`
}

// Validate ensures the config is valid; the top_left attributes are required.
func (c *Config) Validate(path string) ([]string, []string, error) {
	if c.TopLeftXMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "top_left_x_mm")
	}
	if c.TopLeftYMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "top_left_y_mm")
	}
	if c.TopLeftZMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "top_left_z_mm")
	}
	if c.SizeXMM < 0 {
		return nil, nil, fmt.Errorf("size_x_mm must be positive, got %v", c.SizeXMM)
	}
	if c.SizeYMM < 0 {
		return nil, nil, fmt.Errorf("size_y_mm must be positive, got %v", c.SizeYMM)
	}
	if c.Arm == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "arm")
	}
	return []string{c.Arm, motion.Named("builtin").String()}, nil, nil
}

type drawingCamera struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger    logging.Logger
	imagePath string
	xMM       float64
	yMM       float64
	zMM       float64
	sizeXMM   float64
	sizeYMM   float64
	armName   string
	motion    motion.Service

	// mu guards cachedPoses, the poses from the last generate command.
	mu          sync.Mutex
	cachedPoses []spatialmath.Pose
}

func newDrawing(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (camera.Camera, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}
	imagePath := cfg.ImagePath
	if imagePath == "" {
		imagePath = defaultImagePath
	}
	sizeXMM := cfg.SizeXMM
	if sizeXMM == 0 {
		sizeXMM = defaultSizeMM
	}
	sizeYMM := cfg.SizeYMM
	if sizeYMM == 0 {
		sizeYMM = defaultSizeMM
	}
	motionSvc, err := motion.FromDependencies(deps, "builtin")
	if err != nil {
		return nil, err
	}
	return &drawingCamera{
		Named:     conf.ResourceName().AsNamed(),
		logger:    logger,
		imagePath: imagePath,
		xMM:       *cfg.TopLeftXMM,
		yMM:       *cfg.TopLeftYMM,
		zMM:       *cfg.TopLeftZMM,
		sizeXMM:   sizeXMM,
		sizeYMM:   sizeYMM,
		armName:   cfg.Arm,
		motion:    motionSvc,
	}, nil
}

// Images returns the configured PNG file as the camera image.
func (s *drawingCamera) Images(
	ctx context.Context,
	filterSourceNames []string,
	extra map[string]interface{},
) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	imgBytes, err := os.ReadFile(s.imagePath)
	if err != nil {
		return nil, resource.ResponseMetadata{}, fmt.Errorf("failed to read %s: %w", s.imagePath, err)
	}
	named, err := camera.NamedImageFromBytes(imgBytes, "image", rdkutils.MimeTypePNG, data.Annotations{})
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	return []camera.NamedImage{named}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
}

// NextPointCloud is unimplemented; this camera only serves 2D images.
func (s *drawingCamera) NextPointCloud(ctx context.Context, extra map[string]interface{}) (pointcloud.PointCloud, error) {
	return nil, errors.New("point clouds are not supported")
}

// Properties returns the intrinsic properties of this camera.
func (s *drawingCamera) Properties(ctx context.Context) (camera.Properties, error) {
	return camera.Properties{
		SupportsPCD: false,
		ImageType:   camera.ColorStream,
		MimeTypes:   []string{rdkutils.MimeTypePNG},
	}, nil
}

// Geometries returns no geometries; this camera has no physical footprint.
func (s *drawingCamera) Geometries(ctx context.Context, extra map[string]interface{}) ([]spatialmath.Geometry, error) {
	return nil, nil
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "generate"} - reads the configured PNG file and returns the
//	dark pixels as poses over a size_x_mm x size_y_mm drawing area. Each pose
//	has x and y in millimeters offset by the configured top_left_x_mm and
//	top_left_y_mm attributes (the area's top-left corner), z set to the
//	configured top_left_z_mm attribute, and an orientation vector pointing
//	straight down (0, 0, -1) with theta 0, suitable for use as a motion
//	service Move destination. An optional "threshold" (0-255, default 128)
//	sets the grayscale cutoff for which pixels are included. The generated
//	poses are cached for the draw command.
//	{"command": "draw"} - moves the configured arm through the poses cached
//	by the last generate command, using the motion service.
func (s *drawingCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "generate":
		threshold := uint8(defaultThreshold)
		if t, ok := cmd["threshold"].(float64); ok {
			if t < 0 || t > 255 {
				return nil, fmt.Errorf("threshold must be between 0 and 255, got %v", t)
			}
			threshold = uint8(t)
		}

		f, err := os.Open(s.imagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open %s: %w", s.imagePath, err)
		}
		defer f.Close()

		img, err := png.Decode(f)
		if err != nil {
			return nil, fmt.Errorf("failed to decode %s as PNG: %w", s.imagePath, err)
		}

		points := imageToPoints(img, threshold, s.sizeXMM, s.sizeYMM)
		s.logger.Infof("converted %s to %d points over a %.0fmm x %.0fmm area", s.imagePath, len(points), s.sizeXMM, s.sizeYMM)

		downward := &spatialmath.OrientationVectorDegrees{OX: 0, OY: 0, OZ: -1, Theta: 0}
		cached := make([]spatialmath.Pose, len(points))
		poses := make([]interface{}, len(points))
		for i, p := range points {
			x, y := s.xMM+p[0], s.yMM+p[1]
			cached[i] = spatialmath.NewPose(r3.Vector{X: x, Y: y, Z: s.zMM}, downward)
			poses[i] = map[string]interface{}{
				"x":     x,
				"y":     y,
				"z":     s.zMM,
				"o_x":   0.0,
				"o_y":   0.0,
				"o_z":   -1.0,
				"theta": 0.0,
			}
		}

		s.mu.Lock()
		s.cachedPoses = cached
		s.mu.Unlock()

		return map[string]interface{}{
			"poses":     poses,
			"count":     len(poses),
			"size_x_mm": s.sizeXMM,
			"size_y_mm": s.sizeYMM,
		}, nil
	case "draw":
		s.mu.Lock()
		cached := s.cachedPoses
		s.mu.Unlock()
		if len(cached) == 0 {
			return nil, errors.New(`no cached poses; run the "generate" command first`)
		}

		for i, pose := range cached {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("draw canceled after %d of %d poses: %w", i, len(cached), err)
			}
			if _, err := s.motion.Move(ctx, motion.MoveReq{
				ComponentName: s.armName,
				Destination:   referenceframe.NewPoseInFrame(referenceframe.World, pose),
			}); err != nil {
				return nil, fmt.Errorf("failed to move to pose %d of %d: %w", i+1, len(cached), err)
			}
			if (i+1)%100 == 0 {
				s.logger.Infof("drew %d of %d poses", i+1, len(cached))
			}
		}

		return map[string]interface{}{
			"status": "drawing complete",
			"count":  len(cached),
		}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// imageToPoints returns the coordinates (in millimeters) of every pixel whose
// grayscale value is at or below threshold, scaled to fit within a
// sizeXMM x sizeYMM area. Aspect ratio is preserved and the image is
// centered in the area; the origin is the top-left corner.
func imageToPoints(img image.Image, threshold uint8, sizeXMM, sizeYMM float64) [][2]float64 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil
	}

	mmPerPixel := sizeXMM / float64(w)
	if perPixelY := sizeYMM / float64(h); perPixelY < mmPerPixel {
		mmPerPixel = perPixelY
	}
	xOffset := (sizeXMM - float64(w)*mmPerPixel) / 2
	yOffset := (sizeYMM - float64(h)*mmPerPixel) / 2

	var points [][2]float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if gray.Y <= threshold {
				points = append(points, [2]float64{
					xOffset + (float64(x-bounds.Min.X)+0.5)*mmPerPixel,
					yOffset + (float64(y-bounds.Min.Y)+0.5)*mmPerPixel,
				})
			}
		}
	}
	return points
}
