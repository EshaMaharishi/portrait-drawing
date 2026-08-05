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
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
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
	// WidthMM and HeightMM are the size in millimeters of the drawing area the
	// output poses cover; the image is scaled to fit inside it, preserving
	// aspect ratio, and centered. Both default to 254mm (10in).
	WidthMM  float64 `json:"width_mm"`
	HeightMM float64 `json:"height_mm"`
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
	if c.WidthMM < 0 {
		return nil, nil, fmt.Errorf("width_mm must be positive, got %v", c.WidthMM)
	}
	if c.HeightMM < 0 {
		return nil, nil, fmt.Errorf("height_mm must be positive, got %v", c.HeightMM)
	}
	return nil, nil, nil
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
	widthMM   float64
	heightMM  float64
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
	widthMM := cfg.WidthMM
	if widthMM == 0 {
		widthMM = defaultSizeMM
	}
	heightMM := cfg.HeightMM
	if heightMM == 0 {
		heightMM = defaultSizeMM
	}
	return &drawingCamera{
		Named:     conf.ResourceName().AsNamed(),
		logger:    logger,
		imagePath: imagePath,
		xMM:       *cfg.TopLeftXMM,
		yMM:       *cfg.TopLeftYMM,
		zMM:       *cfg.TopLeftZMM,
		widthMM:   widthMM,
		heightMM:  heightMM,
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
//	{"command": "draw"} - reads the configured PNG file and returns the dark
//	pixels as poses over a width_mm x height_mm drawing area. Each pose has x
//	and y in millimeters offset by the configured top_left_x_mm and
//	top_left_y_mm attributes (the area's top-left corner), z set to the
//	configured top_left_z_mm attribute, and an orientation vector pointing
//	straight down (0, 0, -1) with theta 0, suitable for use as a motion
//	service Move destination. An optional "threshold" (0-255, default 128)
//	sets the grayscale cutoff for which pixels are included.
func (s *drawingCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "draw":
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

		points := imageToPoints(img, threshold, s.widthMM, s.heightMM)
		s.logger.Infof("converted %s to %d points over a %.0fmm x %.0fmm area", s.imagePath, len(points), s.widthMM, s.heightMM)

		poses := make([]interface{}, len(points))
		for i, p := range points {
			poses[i] = map[string]interface{}{
				"x":     s.xMM + p[0],
				"y":     s.yMM + p[1],
				"z":     s.zMM,
				"o_x":   0.0,
				"o_y":   0.0,
				"o_z":   -1.0,
				"theta": 0.0,
			}
		}
		return map[string]interface{}{
			"poses":     poses,
			"count":     len(poses),
			"width_mm":  s.widthMM,
			"height_mm": s.heightMM,
		}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// imageToPoints returns the coordinates (in millimeters) of every pixel whose
// grayscale value is at or below threshold, scaled to fit within a
// widthMM x heightMM area. Aspect ratio is preserved and the image is
// centered in the area; the origin is the top-left corner.
func imageToPoints(img image.Image, threshold uint8, widthMM, heightMM float64) [][2]float64 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil
	}

	mmPerPixel := widthMM / float64(w)
	if perPixelY := heightMM / float64(h); perPixelY < mmPerPixel {
		mmPerPixel = perPixelY
	}
	xOffset := (widthMM - float64(w)*mmPerPixel) / 2
	yOffset := (heightMM - float64(h)*mmPerPixel) / 2

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
