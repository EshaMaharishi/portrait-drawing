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
	// squareSizeMM is the side length in millimeters of the square the image
	// is mapped onto (254mm = 10in).
	squareSizeMM = 254.0
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
}

// Validate ensures the config is valid; all attributes are optional.
func (c *Config) Validate(path string) ([]string, []string, error) {
	return nil, nil, nil
}

type drawingCamera struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger    logging.Logger
	imagePath string
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
	return &drawingCamera{
		Named:     conf.ResourceName().AsNamed(),
		logger:    logger,
		imagePath: imagePath,
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
//	pixels as [x, y] coordinates (in millimeters) over a 254mm x 254mm square.
//	An optional "threshold" (0-255, default 128) sets the grayscale cutoff for
//	which pixels are included.
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

		points := imageToPoints(img, threshold, squareSizeMM)
		s.logger.Infof("converted %s to %d points over a %.0fmm x %.0fmm square", s.imagePath, len(points), squareSizeMM, squareSizeMM)

		coords := make([]interface{}, len(points))
		for i, p := range points {
			coords[i] = []interface{}{p[0], p[1]}
		}
		return map[string]interface{}{
			"points":  coords,
			"count":   len(points),
			"size_mm": squareSizeMM,
		}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// imageToPoints returns the coordinates (in millimeters) of every pixel whose
// grayscale value is at or below threshold, scaled to fit within a
// sizeMM x sizeMM square. Aspect ratio is preserved and the image is
// centered in the square; the origin is the top-left corner.
func imageToPoints(img image.Image, threshold uint8, sizeMM float64) [][2]float64 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil
	}

	longSide := w
	if h > w {
		longSide = h
	}
	mmPerPixel := sizeMM / float64(longSide)
	xOffset := (sizeMM - float64(w)*mmPerPixel) / 2
	yOffset := (sizeMM - float64(h)*mmPerPixel) / 2

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
