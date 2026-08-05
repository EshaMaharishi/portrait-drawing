// Package drawing implements a generic service that supports a "draw" DoCommand.
package drawing

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
)

// Model is the full model triplet for this service.
var Model = resource.NewModel("esha", "portrait-drawing", "drawing")

const (
	imagePath = "image.png"
	// squareSizeInches is the side length of the square the image is mapped onto.
	squareSizeInches = 10.0
	// defaultThreshold is the grayscale value (0-255) at or below which a pixel
	// is considered dark enough to draw.
	defaultThreshold = 128
)

func init() {
	resource.RegisterService(generic.API, Model, resource.Registration[resource.Resource, resource.NoNativeConfig]{
		Constructor: newDrawing,
	})
}

type drawingService struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger logging.Logger
}

func newDrawing(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (resource.Resource, error) {
	return &drawingService{
		Named:  conf.ResourceName().AsNamed(),
		logger: logger,
	}, nil
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "draw"} - reads image.png from the current directory and returns
//	the dark pixels as [x, y] coordinates (in inches) over a 10in x 10in square.
//	An optional "threshold" (0-255, default 128) sets the grayscale cutoff for
//	which pixels are included.
func (s *drawingService) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
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

		f, err := os.Open(imagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open %s: %w", imagePath, err)
		}
		defer f.Close()

		img, err := png.Decode(f)
		if err != nil {
			return nil, fmt.Errorf("failed to decode %s as PNG: %w", imagePath, err)
		}

		points := imageToPoints(img, threshold, squareSizeInches)
		s.logger.Infof("converted %s to %d points over a %.0fin x %.0fin square", imagePath, len(points), squareSizeInches, squareSizeInches)

		coords := make([]interface{}, len(points))
		for i, p := range points {
			coords[i] = []interface{}{p[0], p[1]}
		}
		return map[string]interface{}{
			"points":      coords,
			"count":       len(points),
			"size_inches": squareSizeInches,
		}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// imageToPoints returns the coordinates (in inches) of every pixel whose
// grayscale value is at or below threshold, scaled to fit within a
// sizeInches x sizeInches square. Aspect ratio is preserved and the image is
// centered in the square; the origin is the top-left corner.
func imageToPoints(img image.Image, threshold uint8, sizeInches float64) [][2]float64 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil
	}

	longSide := w
	if h > w {
		longSide = h
	}
	inchesPerPixel := sizeInches / float64(longSide)
	xOffset := (sizeInches - float64(w)*inchesPerPixel) / 2
	yOffset := (sizeInches - float64(h)*inchesPerPixel) / 2

	var points [][2]float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if gray.Y <= threshold {
				points = append(points, [2]float64{
					xOffset + (float64(x-bounds.Min.X)+0.5)*inchesPerPixel,
					yOffset + (float64(y-bounds.Min.Y)+0.5)*inchesPerPixel,
				})
			}
		}
	}
	return points
}
