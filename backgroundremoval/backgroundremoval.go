// Package backgroundremoval implements a camera component that removes the
// background from another camera's image using its point cloud: color pixels
// with no 3D point nearer than a depth cutoff are painted white, keeping
// only the subject.
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

	"github.com/golang/geo/r3"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
	"go.viam.com/rdk/rimage/transform"
	"go.viam.com/rdk/spatialmath"
	rdkutils "go.viam.com/rdk/utils"
)

// Model is the full model triplet for this camera.
var Model = resource.NewModel("chess-piece-detection", "portrait-drawing", "background-removal")

// maskGridDiv is the downscale factor of the foreground mask grid relative
// to the color image. Projected points splat into grid cells, so a coarser
// grid fills the gaps between neighboring points.
const maskGridDiv = 4

func init() {
	resource.RegisterComponent(camera.API, Model, resource.Registration[camera.Camera, *Config]{
		Constructor: newBackgroundRemoval,
	})
}

// Config describes the attributes for this camera.
type Config struct {
	// Camera is the name of the source camera; it must serve a color image,
	// a point cloud, and intrinsic parameters (e.g. an Orbbec Astra 2).
	// Required.
	Camera string `json:"camera"`
	// MaxDepthMM is the depth cutoff in millimeters: color pixels with no
	// point cloud return nearer than this are treated as background and
	// painted white. Required.
	MaxDepthMM *float64 `json:"max_depth_mm"`
	// ColorSource pins which of the source camera's named images is the
	// color image. When unset, the first image that does not decode as a
	// depth map is used.
	ColorSource string `json:"color_source"`
	// DepthScale converts the point cloud's units to millimeters:
	// mm = value * depth_scale. Defaults to 1 (values already in mm). Set to
	// 1000 for a camera that reports meters.
	DepthScale float64 `json:"depth_scale"`
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
	if c.DepthScale < 0 {
		return nil, nil, fmt.Errorf("depth_scale must be positive, got %v", c.DepthScale)
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
	depthScale  float64
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
	depthScale := cfg.DepthScale
	if depthScale == 0 {
		depthScale = 1
	}
	return &backgroundRemovalCamera{
		Named:       conf.ResourceName().AsNamed(),
		logger:      logger,
		srcCam:      srcCam,
		maxDepthMM:  *cfg.MaxDepthMM,
		depthScale:  depthScale,
		colorSource: cfg.ColorSource,
	}, nil
}

// Images returns the source camera's color image with pixels beyond the
// depth cutoff painted white, using the source camera's point cloud
// projected through its intrinsics.
func (s *backgroundRemovalCamera) Images(
	ctx context.Context,
	filterSourceNames []string,
	extra map[string]interface{},
) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	namedImages, meta, err := s.srcCam.Images(ctx, nil, extra)
	if err != nil {
		return nil, resource.ResponseMetadata{}, fmt.Errorf("failed to get images from source camera: %w", err)
	}
	colorImg, err := colorImage(ctx, namedImages, s.colorSource)
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}

	pc, err := s.srcCam.NextPointCloud(ctx, nil)
	if err != nil {
		return nil, resource.ResponseMetadata{}, fmt.Errorf("failed to get point cloud from source camera: %w", err)
	}
	intrinsics, err := s.intrinsics(ctx)
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}

	masked := maskByPointCloud(colorImg, pc, intrinsics, s.maxDepthMM/s.depthScale)
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

// intrinsics fetches the source camera's pinhole intrinsics, required to
// project point cloud points onto the color image.
func (s *backgroundRemovalCamera) intrinsics(ctx context.Context) (*transform.PinholeCameraIntrinsics, error) {
	props, err := s.srcCam.Properties(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get source camera properties: %w", err)
	}
	if props.IntrinsicParams == nil || props.IntrinsicParams.Fx == 0 || props.IntrinsicParams.Fy == 0 {
		return nil, errors.New("source camera does not provide intrinsic parameters, which are needed to project its point cloud onto the color image")
	}
	return props.IntrinsicParams, nil
}

// colorImage finds the color image among a camera's named images: the one
// named colorSource when set, otherwise the first that does not decode as a
// depth map.
func colorImage(ctx context.Context, namedImages []camera.NamedImage, colorSource string) (image.Image, error) {
	var sources []string
	for i := range namedImages {
		name := namedImages[i].SourceName
		sources = append(sources, name)
		if colorSource != "" && name != colorSource {
			continue
		}
		img, err := namedImages[i].Image(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to decode image from source %q: %w", name, err)
		}
		if colorSource != "" {
			return img, nil
		}
		if _, err := rimage.ConvertImageToDepthMap(ctx, img); err != nil {
			return img, nil
		}
	}
	return nil, fmt.Errorf("could not find a color image among sources %v; set color_source to pin it by name", sources)
}

// maskByPointCloud returns a copy of the color image where only pixels with
// a point cloud return nearer than maxDepth survive; everything else is
// painted white. Points are projected onto the image through the camera's
// intrinsics and splatted into a grid maskGridDiv times coarser than the
// image, which fills the gaps between neighboring points.
func maskByPointCloud(
	colorImg image.Image,
	pc pointcloud.PointCloud,
	intrinsics *transform.PinholeCameraIntrinsics,
	maxDepth float64,
) image.Image {
	bounds := colorImg.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	gw, gh := (w+maskGridDiv-1)/maskGridDiv, (h+maskGridDiv-1)/maskGridDiv
	foreground := make([]bool, gw*gh)

	// Intrinsics may be calibrated at a different resolution than the served
	// color image; scale projected pixels accordingly.
	scaleX, scaleY := 1.0, 1.0
	if intrinsics.Width > 0 && intrinsics.Height > 0 {
		scaleX = float64(w) / float64(intrinsics.Width)
		scaleY = float64(h) / float64(intrinsics.Height)
	}

	pc.Iterate(0, 0, func(p r3.Vector, _ pointcloud.Data) bool {
		if p.Z <= 0 || p.Z > maxDepth {
			return true
		}
		px, py := intrinsics.PointToPixel(p.X, p.Y, p.Z)
		gx, gy := int(px*scaleX)/maskGridDiv, int(py*scaleY)/maskGridDiv
		if gx >= 0 && gx < gw && gy >= 0 && gy < gh {
			foreground[gy*gw+gx] = true
		}
		return true
	})

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 0; y < h; y++ {
		gy := y / maskGridDiv
		for x := 0; x < w; x++ {
			if foreground[gy*gw+x/maskGridDiv] {
				out.Set(x, y, colorImg.At(bounds.Min.X+x, bounds.Min.Y+y))
			} else {
				out.SetRGBA(x, y, white)
			}
		}
	}
	return out
}

// NextPointCloud is unimplemented; use the source camera directly.
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
//	{"command": "depth_stats"} - reports statistics of the source camera's
//	current point cloud (size, min/median/max z) and whether intrinsics are
//	available, for calibrating max_depth_mm and depth_scale. Stand at a
//	known distance and compare the median z to deduce the units.
func (s *backgroundRemovalCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "depth_stats":
		resp := map[string]interface{}{}
		if intrinsics, err := s.intrinsics(ctx); err != nil {
			resp["intrinsics_error"] = err.Error()
		} else {
			resp["intrinsics"] = map[string]interface{}{
				"width":  intrinsics.Width,
				"height": intrinsics.Height,
				"fx":     intrinsics.Fx,
				"fy":     intrinsics.Fy,
				"ppx":    intrinsics.Ppx,
				"ppy":    intrinsics.Ppy,
			}
		}

		pc, err := s.srcCam.NextPointCloud(ctx, nil)
		if err != nil {
			resp["point_cloud_error"] = err.Error()
			return resp, nil
		}
		var zs []float64
		pc.Iterate(0, 0, func(p r3.Vector, _ pointcloud.Data) bool {
			zs = append(zs, p.Z)
			return true
		})
		resp["point_count"] = len(zs)
		if len(zs) > 0 {
			sort.Float64s(zs)
			resp["min_z"] = zs[0]
			resp["median_z"] = zs[len(zs)/2]
			resp["max_z"] = zs[len(zs)-1]
		}
		return resp, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}
