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

	logger     logging.Logger
	srcCam     camera.Camera
	maxDepthMM float64
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
		Named:      conf.ResourceName().AsNamed(),
		logger:     logger,
		srcCam:     srcCam,
		maxDepthMM: *cfg.MaxDepthMM,
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

	colorImg, depthMap, err := splitColorAndDepth(ctx, namedImages)
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
// camera's named images. The depth image is whichever decodes as a depth
// map; the color image is the first that does not.
func splitColorAndDepth(
	ctx context.Context,
	namedImages []camera.NamedImage,
) (image.Image, *rimage.DepthMap, error) {
	var colorImg image.Image
	var depthMap *rimage.DepthMap
	var sources []string
	for i := range namedImages {
		img, err := namedImages[i].Image(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode image from source %q: %w", namedImages[i].SourceName, err)
		}
		sources = append(sources, namedImages[i].SourceName)
		if dm, err := rimage.ConvertImageToDepthMap(ctx, img); err == nil {
			if depthMap == nil {
				depthMap = dm
			}
		} else if colorImg == nil {
			colorImg = img
		}
	}
	if colorImg == nil || depthMap == nil {
		return nil, nil, fmt.Errorf(
			"source camera must serve both a color and a depth image, got sources %v (color found: %t, depth found: %t)",
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

// DoCommand supports no commands.
func (s *backgroundRemovalCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, errors.New("no commands supported")
}
