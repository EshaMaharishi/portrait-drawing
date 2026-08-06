// Package backgroundremoval implements a camera component that removes the
// background from another camera's image using its depth frame: color pixels
// farther than a depth cutoff are painted white, keeping only the subject.
package backgroundremoval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sort"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
	"go.viam.com/rdk/spatialmath"
	rdkutils "go.viam.com/rdk/utils"
)

// Model is the full model triplet for this camera.
var Model = resource.NewModel("esha", "portrait-drawing", "background-removal")

func init() {
	resource.RegisterComponent(camera.API, Model, resource.Registration[camera.Camera, *Config]{
		Constructor: newBackgroundRemoval,
	})
}

// Config describes the attributes for this camera.
type Config struct {
	// Camera is the name of the source camera; it must serve both a color
	// and a depth image (e.g. an Orbbec Astra 2). Required.
	Camera string `json:"camera"`
	// MaxDepthMM is the depth cutoff in millimeters: color pixels whose
	// depth reading is farther than this (or missing) are treated as
	// background and painted white. Required.
	MaxDepthMM *float64 `json:"max_depth_mm"`
	// DepthSource and ColorSource pin which of the source camera's named
	// images to use, for cameras that serve more than two (e.g. color,
	// depth, and IR). When unset, the first image that decodes as a depth
	// map is used as depth and the first that does not as color.
	DepthSource string `json:"depth_source"`
	ColorSource string `json:"color_source"`
}

// Validate ensures the config is valid; camera and max_depth_mm are required.
func (c *Config) Validate(path string) ([]string, []string, error) {
	if c.Camera == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "camera")
	}
	if c.MaxDepthMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "max_depth_mm")
	}
	if *c.MaxDepthMM <= 0 {
		return nil, nil, fmt.Errorf("max_depth_mm must be positive, got %v", *c.MaxDepthMM)
	}
	return []string{c.Camera}, nil, nil
}

type backgroundRemovalCamera struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger      logging.Logger
	srcCam      camera.Camera
	maxDepthMM  float64
	depthSource string
	colorSource string
}

func newBackgroundRemoval(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (camera.Camera, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}
	srcCam, err := camera.FromDependencies(deps, cfg.Camera)
	if err != nil {
		return nil, err
	}
	return &backgroundRemovalCamera{
		Named:       conf.ResourceName().AsNamed(),
		logger:      logger,
		srcCam:      srcCam,
		maxDepthMM:  *cfg.MaxDepthMM,
		depthSource: cfg.DepthSource,
		colorSource: cfg.ColorSource,
	}, nil
}

// Images returns the source camera's color image with pixels beyond the
// depth cutoff painted white.
func (s *backgroundRemovalCamera) Images(
	ctx context.Context,
	filterSourceNames []string,
	extra map[string]interface{},
) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	namedImages, meta, err := s.srcCam.Images(ctx, nil, extra)
	if err != nil {
		return nil, resource.ResponseMetadata{}, fmt.Errorf("failed to get images from source camera: %w", err)
	}

	colorImg, depthMap, err := splitColorAndDepth(ctx, namedImages, s.colorSource, s.depthSource)
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}

	masked := maskByDepth(colorImg, depthMap, s.maxDepthMM)
	var buf bytes.Buffer
	if err := png.Encode(&buf, masked); err != nil {
		return nil, resource.ResponseMetadata{}, fmt.Errorf("failed to encode masked image: %w", err)
	}
	named, err := camera.NamedImageFromBytes(buf.Bytes(), "color", rdkutils.MimeTypePNG, data.Annotations{})
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	return []camera.NamedImage{named}, meta, nil
}

// splitColorAndDepth finds the color image and the depth map among a
// camera's named images. When colorSource/depthSource are non-empty, images
// are matched by source name; otherwise the depth image is the first that
// decodes as a depth map and the color image is the first that does not.
func splitColorAndDepth(
	ctx context.Context,
	namedImages []camera.NamedImage,
	colorSource, depthSource string,
) (image.Image, *rimage.DepthMap, error) {
	var colorImg image.Image
	var depthMap *rimage.DepthMap
	var sources []string
	for i := range namedImages {
		name := namedImages[i].SourceName
		sources = append(sources, name)
		if colorSource != "" && name != colorSource && depthSource != "" && name != depthSource {
			continue
		}
		img, err := namedImages[i].Image(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode image from source %q: %w", name, err)
		}
		dm, dmErr := rimage.ConvertImageToDepthMap(ctx, img)

		switch {
		case depthSource != "" && name == depthSource:
			if dmErr != nil {
				return nil, nil, fmt.Errorf("depth_source %q does not decode as a depth map (%T)", name, img)
			}
			depthMap = dm
		case colorSource != "" && name == colorSource:
			colorImg = img
		case depthSource == "" && dmErr == nil && depthMap == nil:
			depthMap = dm
		case colorSource == "" && dmErr != nil && colorImg == nil:
			colorImg = img
		}
	}
	if colorImg == nil || depthMap == nil {
		return nil, nil, fmt.Errorf(
			"could not find both a color and a depth image, got sources %v (color found: %t, depth found: %t); "+
				"set color_source and depth_source to pin them by name",
			sources, colorImg != nil, depthMap != nil)
	}
	return colorImg, depthMap, nil
}

// maskByDepth returns a copy of the color image with every pixel whose depth
// is missing or beyond maxDepthMM painted white. The depth map may have a
// different resolution than the color image; coordinates are scaled
// proportionally, which assumes the two frames are aligned.
func maskByDepth(colorImg image.Image, depthMap *rimage.DepthMap, maxDepthMM float64) image.Image {
	bounds := colorImg.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dw, dh := depthMap.Width(), depthMap.Height()
	out := image.NewRGBA(image.Rect(0, 0, w, h))

	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 0; y < h; y++ {
		dy := y * dh / h
		for x := 0; x < w; x++ {
			dx := x * dw / w
			depth := float64(depthMap.GetDepth(dx, dy))
			// A depth of 0 means no reading; treat it as background.
			if depth == 0 || depth > maxDepthMM {
				out.SetRGBA(x, y, white)
			} else {
				out.Set(x, y, colorImg.At(bounds.Min.X+x, bounds.Min.Y+y))
			}
		}
	}
	return out
}

// NextPointCloud is unimplemented.
func (s *backgroundRemovalCamera) NextPointCloud(ctx context.Context, extra map[string]interface{}) (pointcloud.PointCloud, error) {
	return nil, errors.New("point clouds are not supported")
}

// Properties returns the intrinsic properties of this camera.
func (s *backgroundRemovalCamera) Properties(ctx context.Context) (camera.Properties, error) {
	return camera.Properties{
		SupportsPCD: false,
		ImageType:   camera.ColorStream,
		MimeTypes:   []string{rdkutils.MimeTypePNG},
	}, nil
}

// Geometries returns no geometries; this camera has no physical footprint.
func (s *backgroundRemovalCamera) Geometries(ctx context.Context, extra map[string]interface{}) ([]spatialmath.Geometry, error) {
	return nil, nil
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "depth_stats"} - reports every named image the source camera
//	serves (name, decoded type, whether it decodes as a depth map) and, for
//	each depth-map candidate, statistics of its current frame (center value,
//	non-zero min/median/max, percent with no reading). Use it to identify
//	which source is the real depth stream (pin it with depth_source) and to
//	calibrate max_depth_mm: stand at a known distance and compare the center
//	value to deduce the depth units.
func (s *backgroundRemovalCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "depth_stats":
		namedImages, _, err := s.srcCam.Images(ctx, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get images from source camera: %w", err)
		}

		sources := make([]interface{}, 0, len(namedImages))
		for i := range namedImages {
			entry := map[string]interface{}{"source": namedImages[i].SourceName}
			img, err := namedImages[i].Image(ctx)
			if err != nil {
				entry["decode_error"] = err.Error()
				sources = append(sources, entry)
				continue
			}
			bounds := img.Bounds()
			entry["type"] = fmt.Sprintf("%T", img)
			entry["width"] = bounds.Dx()
			entry["height"] = bounds.Dy()
			if dm, err := rimage.ConvertImageToDepthMap(ctx, img); err == nil {
				entry["is_depth_candidate"] = true
				entry["stats"] = depthStats(dm)
			} else {
				entry["is_depth_candidate"] = false
			}
			sources = append(sources, entry)
		}
		return map[string]interface{}{"sources": sources}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// depthStats summarizes a depth frame's values.
func depthStats(depthMap *rimage.DepthMap) map[string]interface{} {
	w, h := depthMap.Width(), depthMap.Height()
	var nonZero []float64
	zeros := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := float64(depthMap.GetDepth(x, y))
			if d == 0 {
				zeros++
			} else {
				nonZero = append(nonZero, d)
			}
		}
	}
	stats := map[string]interface{}{
		"center_value": float64(depthMap.GetDepth(w/2, h/2)),
		"percent_zero": 100 * float64(zeros) / float64(w*h),
	}
	if len(nonZero) > 0 {
		sort.Float64s(nonZero)
		stats["min_nonzero"] = nonZero[0]
		stats["median_nonzero"] = nonZero[len(nonZero)/2]
		stats["max"] = nonZero[len(nonZero)-1]
	}
	return stats
}
