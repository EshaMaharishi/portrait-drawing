// Package imagetoposes implements a camera component that converts an image
// into drawable poses via its generate DoCommand and serves a preview of the
// generated points as its camera image.
package imagetoposes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sync"
	"time"

	"github.com/golang/geo/r3"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
	"go.viam.com/rdk/spatialmath"
	rdkutils "go.viam.com/rdk/utils"
)

// Model is the full model triplet for this camera.
var Model = resource.NewModel("esha", "portrait-drawing", "image-to-poses")

const (
	// defaultSizeMM is the width and height in millimeters of the area the
	// image is mapped onto when size_x_mm/size_y_mm are not set (254mm = 10in).
	defaultSizeMM = 254.0
	// defaultThreshold is the grayscale value (0-255) at or below which a pixel
	// is considered dark enough to draw.
	defaultThreshold = 128
	// previewPxPerMM is the resolution of the preview image returned by
	// Images, in pixels per millimeter of the drawing area.
	previewPxPerMM = 4.0
)

func init() {
	resource.RegisterComponent(camera.API, Model, resource.Registration[camera.Camera, *Config]{
		Constructor: newImageToPoses,
	})
}

// Config describes the attributes for this camera.
type Config struct {
	// ImagePath is the path to the PNG file to convert. Exactly one of
	// ImagePath and Camera is required.
	ImagePath string `json:"image_path"`
	// Camera is the name of a camera whose image is converted instead of a
	// file. Exactly one of ImagePath and Camera is required.
	Camera string `json:"camera"`
	// TopLeftXMM and TopLeftYMM are the position in millimeters of the
	// top-left corner of the drawing area; the image's [x, y] coordinates
	// (origin at the image's top-left) are offset by them. Required.
	TopLeftXMM *float64 `json:"top_left_x_mm"`
	TopLeftYMM *float64 `json:"top_left_y_mm"`
	// Surface is the name of the table-surface calibration service that
	// provides the drawing surface's plane; each pose's z is evaluated from
	// that plane at its x/y. Required.
	Surface string `json:"surface"`
	// SizeXMM and SizeYMM are the extent in millimeters of the drawing area
	// along the x and y axes; the image is scaled to fit inside it, preserving
	// aspect ratio, and centered. Both default to 254mm (10in).
	SizeXMM float64 `json:"size_x_mm"`
	SizeYMM float64 `json:"size_y_mm"`
	// PointSpacingMM is the physical spacing in millimeters between generated
	// points: the drawing area is divided into a grid of this cell size, and
	// each cell whose average darkness passes the threshold becomes one pose.
	// Match it to the pen tip width. Required.
	PointSpacingMM *float64 `json:"point_spacing_mm"`
	// HoverAboveMM lifts the pen between points: after each point's pose, an
	// additional pose is generated this many millimeters above it. Set to 0
	// to disable the hover poses. Required.
	HoverAboveMM *float64 `json:"hover_above_mm"`
}

// Validate ensures the config is valid.
func (c *Config) Validate(path string) ([]string, []string, error) {
	if c.ImagePath == "" && c.Camera == "" {
		return nil, nil, fmt.Errorf(`%s: exactly one of "image_path" or "camera" is required`, path)
	}
	if c.ImagePath != "" && c.Camera != "" {
		return nil, nil, fmt.Errorf(`%s: set only one of "image_path" and "camera"`, path)
	}
	if c.TopLeftXMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "top_left_x_mm")
	}
	if c.TopLeftYMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "top_left_y_mm")
	}
	if c.Surface == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "surface")
	}
	if c.SizeXMM < 0 {
		return nil, nil, fmt.Errorf("size_x_mm must be positive, got %v", c.SizeXMM)
	}
	if c.SizeYMM < 0 {
		return nil, nil, fmt.Errorf("size_y_mm must be positive, got %v", c.SizeYMM)
	}
	if c.PointSpacingMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "point_spacing_mm")
	}
	if *c.PointSpacingMM <= 0 {
		return nil, nil, fmt.Errorf("point_spacing_mm must be positive, got %v", *c.PointSpacingMM)
	}
	if c.HoverAboveMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "hover_above_mm")
	}
	if *c.HoverAboveMM < 0 {
		return nil, nil, fmt.Errorf("hover_above_mm must be non-negative, got %v", *c.HoverAboveMM)
	}
	deps := []string{c.Surface}
	if c.Camera != "" {
		deps = append(deps, c.Camera)
	}
	return deps, nil, nil
}

type imageToPosesCamera struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger    logging.Logger
	imagePath string
	// srcCam is the camera the image is read from; nil when image_path is
	// configured instead.
	srcCam    camera.Camera
	xMM       float64
	yMM       float64
	sizeXMM   float64
	sizeYMM   float64
	spacingMM float64
	hoverMM   float64
	// surface is the table-surface calibration service; its get_plane
	// command provides the drawing surface's plane.
	surface resource.Resource

	// mu guards the fields below, set by the generate command. cachedPlane
	// records the surface plane the poses were generated with, so a
	// recalibration invalidates the cache.
	mu              sync.Mutex
	cachedPoses     []spatialmath.Pose
	cachedSpacingMM float64
	cachedPlane     [3]float64
}

func newImageToPoses(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (camera.Camera, error) {
	cfg, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return nil, err
	}
	var srcCam camera.Camera
	if cfg.Camera != "" {
		srcCam, err = camera.FromDependencies(deps, cfg.Camera)
		if err != nil {
			return nil, err
		}
	}
	sizeXMM := cfg.SizeXMM
	if sizeXMM == 0 {
		sizeXMM = defaultSizeMM
	}
	sizeYMM := cfg.SizeYMM
	if sizeYMM == 0 {
		sizeYMM = defaultSizeMM
	}

	surface, err := resource.FromDependencies[resource.Resource](deps, generic.Named(cfg.Surface))
	if err != nil {
		return nil, err
	}

	return &imageToPosesCamera{
		Named:     conf.ResourceName().AsNamed(),
		logger:    logger,
		imagePath: cfg.ImagePath,
		srcCam:    srcCam,
		xMM:       *cfg.TopLeftXMM,
		yMM:       *cfg.TopLeftYMM,
		sizeXMM:   sizeXMM,
		sizeYMM:   sizeYMM,
		spacingMM: *cfg.PointSpacingMM,
		hoverMM:   *cfg.HoverAboveMM,
		surface:   surface,
	}, nil
}

// sourceImage returns the image to convert - read from the configured
// camera, or decoded from the configured PNG file - along with a description
// of the source for logging.
func (s *imageToPosesCamera) sourceImage(ctx context.Context) (image.Image, string, error) {
	if s.srcCam != nil {
		img, err := camera.DecodeImageFromCamera(ctx, s.srcCam, nil, nil)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get image from camera: %w", err)
		}
		return img, "camera image", nil
	}

	f, err := os.Open(s.imagePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open %s: %w", s.imagePath, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode %s as PNG: %w", s.imagePath, err)
	}
	return img, s.imagePath, nil
}

// surfacePlane fetches the drawing surface's plane coefficients from the
// table-surface service, such that z = a*x + b*y + c.
func (s *imageToPosesCamera) surfacePlane(ctx context.Context) ([3]float64, error) {
	resp, err := s.surface.DoCommand(ctx, map[string]interface{}{"command": "get_plane"})
	if err != nil {
		return [3]float64{}, fmt.Errorf("failed to get plane from surface service: %w", err)
	}
	var plane [3]float64
	for i, key := range []string{"a", "b", "c"} {
		v, ok := resp[key].(float64)
		if !ok {
			return [3]float64{}, fmt.Errorf("surface service get_plane response is missing %q: %v", key, resp)
		}
		plane[i] = v
	}
	return plane, nil
}

// Images returns a preview of the generated XY points: one black dot per
// point on a white canvas the size of the drawing area. It errors until the
// generate command has cached a set of poses.
func (s *imageToPosesCamera) Images(
	ctx context.Context,
	filterSourceNames []string,
	extra map[string]interface{},
) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	s.mu.Lock()
	cached := s.cachedPoses
	spacingMM := s.cachedSpacingMM
	s.mu.Unlock()
	if len(cached) == 0 {
		return nil, resource.ResponseMetadata{}, errors.New("no image, call generate Do command")
	}

	// When hovering is enabled the cached sequence alternates touch, hover;
	// skip the hover poses since they duplicate a drawn point's x/y.
	step := 1
	if s.hoverMM > 0 {
		step = 2
	}
	points := make([][2]float64, 0, (len(cached)+step-1)/step)
	for i := 0; i < len(cached); i += step {
		pt := cached[i].Point()
		points = append(points, [2]float64{pt.X - s.xMM, pt.Y - s.yMM})
	}
	preview := renderPoints(points, s.sizeXMM, s.sizeYMM, spacingMM)
	var buf bytes.Buffer
	if err := png.Encode(&buf, preview); err != nil {
		return nil, resource.ResponseMetadata{}, fmt.Errorf("failed to encode preview image: %w", err)
	}
	named, err := camera.NamedImageFromBytes(buf.Bytes(), "points", rdkutils.MimeTypePNG, data.Annotations{})
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	return []camera.NamedImage{named}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
}

// NextPointCloud is unimplemented; this camera only serves 2D images.
func (s *imageToPosesCamera) NextPointCloud(ctx context.Context, extra map[string]interface{}) (pointcloud.PointCloud, error) {
	return nil, errors.New("point clouds are not supported")
}

// Properties returns the intrinsic properties of this camera.
func (s *imageToPosesCamera) Properties(ctx context.Context) (camera.Properties, error) {
	return camera.Properties{
		SupportsPCD: false,
		ImageType:   camera.ColorStream,
		MimeTypes:   []string{rdkutils.MimeTypePNG},
	}, nil
}

// Geometries returns no geometries; this camera has no physical footprint.
func (s *imageToPosesCamera) Geometries(ctx context.Context, extra map[string]interface{}) ([]spatialmath.Geometry, error) {
	return nil, nil
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "generate"} - reads the configured PNG file or camera image
//	and returns the dark cells of a point_spacing_mm grid as poses over a
//	size_x_mm x size_y_mm drawing area, caching them for the pose executor
//	and the preview image. Once a set of poses is cached, generate returns it
//	without recomputing, unless "threshold" (0-255, default 128) or
//	"point_spacing_mm" overrides are passed, "force": true is passed (useful
//	to re-grab a camera image), or the surface calibration has changed, any
//	of which forces a regeneration. Each pose has x and y in
//	millimeters offset by the configured top_left_x_mm and top_left_y_mm
//	attributes (the area's top-left corner), z evaluated at that x/y from the
//	surface service's calibrated plane, and an orientation vector pointing
//	straight down (0, 0, -1) with theta 0, suitable for use as a motion
//	service Move destination. When hover_above_mm is non-zero, each point's
//	pose is followed by an additional pose that far above it, so the pen
//	lifts between points.
func (s *imageToPosesCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "generate":
		threshold := uint8(defaultThreshold)
		overridden := false
		if t, ok := cmd["threshold"].(float64); ok {
			if t < 0 || t > 255 {
				return nil, fmt.Errorf("threshold must be between 0 and 255, got %v", t)
			}
			threshold = uint8(t)
			overridden = true
		}
		spacingMM := s.spacingMM
		if sp, ok := cmd["point_spacing_mm"].(float64); ok {
			if sp <= 0 {
				return nil, fmt.Errorf("point_spacing_mm must be positive, got %v", sp)
			}
			spacingMM = sp
			overridden = true
		}
		if force, ok := cmd["force"].(bool); ok && force {
			overridden = true
		}

		plane, err := s.surfacePlane(ctx)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		cached := s.cachedPoses
		cachedSpacingMM := s.cachedSpacingMM
		cachedPlane := s.cachedPlane
		s.mu.Unlock()
		if len(cached) > 0 && !overridden && plane == cachedPlane {
			return s.posesResponse(cached, cachedSpacingMM), nil
		}

		img, source, err := s.sourceImage(ctx)
		if err != nil {
			return nil, err
		}

		points := imageToPoints(img, threshold, s.sizeXMM, s.sizeYMM, spacingMM)
		s.logger.Infof("converted %s to %d points over a %.0fmm x %.0fmm area at %.1fmm spacing",
			source, len(points), s.sizeXMM, s.sizeYMM, spacingMM)

		downward := &spatialmath.OrientationVectorDegrees{OX: 0, OY: 0, OZ: -1, Theta: 0}
		poses := make([]spatialmath.Pose, 0, len(points)*2)
		for _, p := range points {
			x, y := s.xMM+p[0], s.yMM+p[1]
			z := plane[0]*x + plane[1]*y + plane[2]
			poses = append(poses, spatialmath.NewPose(r3.Vector{X: x, Y: y, Z: z}, downward))
			if s.hoverMM > 0 {
				poses = append(poses, spatialmath.NewPose(r3.Vector{X: x, Y: y, Z: z + s.hoverMM}, downward))
			}
		}

		s.mu.Lock()
		s.cachedPoses = poses
		s.cachedSpacingMM = spacingMM
		s.cachedPlane = plane
		s.mu.Unlock()

		return s.posesResponse(poses, spacingMM), nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// posesResponse builds the generate response for a set of poses.
func (s *imageToPosesCamera) posesResponse(poses []spatialmath.Pose, spacingMM float64) map[string]interface{} {
	out := make([]interface{}, len(poses))
	for i, pose := range poses {
		pt := pose.Point()
		out[i] = map[string]interface{}{
			"x":     pt.X,
			"y":     pt.Y,
			"z":     pt.Z,
			"o_x":   0.0,
			"o_y":   0.0,
			"o_z":   -1.0,
			"theta": 0.0,
		}
	}
	return map[string]interface{}{
		"poses":            out,
		"count":            len(out),
		"size_x_mm":        s.sizeXMM,
		"size_y_mm":        s.sizeYMM,
		"point_spacing_mm": spacingMM,
	}
}

// imageToPoints converts the image to points (in millimeters) on a grid of
// spacingMM cells. The image is scaled to fit within a sizeXMM x sizeYMM
// area, preserving aspect ratio, and centered; the origin is the top-left
// corner. Each grid cell whose average grayscale value is at or below
// threshold becomes one point at the cell's center, so spacingMM controls
// the density of the output regardless of the image's resolution.
//
// Points are ordered by greedy nearest neighbor: the first point is the one
// closest to the area's top-left corner, and each subsequent point is the
// unvisited point closest to the previous one.
func imageToPoints(img image.Image, threshold uint8, sizeXMM, sizeYMM, spacingMM float64) [][2]float64 {
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

	// Average the pixels falling in each spacingMM x spacingMM grid cell.
	cols := int(math.Ceil(float64(w) * mmPerPixel / spacingMM))
	rows := int(math.Ceil(float64(h) * mmPerPixel / spacingMM))
	if cols == 0 || rows == 0 {
		return nil
	}
	sums := make([]float64, cols*rows)
	counts := make([]int, cols*rows)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		cy := min(int(float64(y-bounds.Min.Y)*mmPerPixel/spacingMM), rows-1)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cx := min(int(float64(x-bounds.Min.X)*mmPerPixel/spacingMM), cols-1)
			gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			sums[cy*cols+cx] += float64(gray.Y)
			counts[cy*cols+cx]++
		}
	}

	// Mark the dark cells and find the one closest to the area's top-left
	// corner (0, 0) to start from.
	dark := make([]bool, cols*rows)
	total := 0
	startCX, startCY, bestStart := 0, 0, math.MaxFloat64
	for cy := 0; cy < rows; cy++ {
		for cx := 0; cx < cols; cx++ {
			cell := cy*cols + cx
			if counts[cell] == 0 || sums[cell]/float64(counts[cell]) > float64(threshold) {
				continue
			}
			dark[cell] = true
			total++
			pxMM := xOffset + (float64(cx)+0.5)*spacingMM
			pyMM := yOffset + (float64(cy)+0.5)*spacingMM
			if d := pxMM*pxMM + pyMM*pyMM; d < bestStart {
				bestStart = d
				startCX, startCY = cx, cy
			}
		}
	}
	if total == 0 {
		return nil
	}

	// Greedy nearest-neighbor walk over the dark cells.
	points := make([][2]float64, 0, total)
	cx, cy := startCX, startCY
	for {
		dark[cy*cols+cx] = false
		points = append(points, [2]float64{
			xOffset + (float64(cx)+0.5)*spacingMM,
			yOffset + (float64(cy)+0.5)*spacingMM,
		})
		if len(points) == total {
			return points
		}
		cx, cy = nearestDark(dark, cols, rows, cx, cy)
	}
}

// nearestDark returns the dark cell closest (by Euclidean distance) to
// (fromCX, fromCY), searching outward ring by ring. A match found in ring r
// is only accepted once no closer match can exist in a farther ring.
func nearestDark(dark []bool, cols, rows, fromCX, fromCY int) (int, int) {
	maxR := cols
	if rows > maxR {
		maxR = rows
	}
	bestCX, bestCY, bestD := -1, -1, math.MaxInt
	check := func(cx, cy int) {
		if cx < 0 || cx >= cols || cy < 0 || cy >= rows || !dark[cy*cols+cx] {
			return
		}
		dx, dy := cx-fromCX, cy-fromCY
		if d := dx*dx + dy*dy; d < bestD {
			bestD = d
			bestCX, bestCY = cx, cy
		}
	}
	for r := 1; r <= maxR; r++ {
		// Any cell in ring r is at least distance r away; stop once the best
		// match found so far is provably closer than all remaining rings.
		if bestCX >= 0 && r*r > bestD {
			break
		}
		for cx := fromCX - r; cx <= fromCX+r; cx++ {
			check(cx, fromCY-r)
			check(cx, fromCY+r)
		}
		for cy := fromCY - r + 1; cy <= fromCY+r-1; cy++ {
			check(fromCX-r, cy)
			check(fromCX+r, cy)
		}
	}
	return bestCX, bestCY
}

// renderPoints draws the given points as black dots on a white canvas
// spanning sizeXMM x sizeYMM, at previewPxPerMM resolution. Each dot's
// radius is half the point spacing, matching the coverage of a pen tip of
// that width.
func renderPoints(points [][2]float64, sizeXMM, sizeYMM, spacingMM float64) image.Image {
	w := int(math.Ceil(sizeXMM * previewPxPerMM))
	h := int(math.Ceil(sizeYMM * previewPxPerMM))
	canvas := image.NewGray(image.Rect(0, 0, w, h))
	for i := range canvas.Pix {
		canvas.Pix[i] = 255
	}

	radius := spacingMM * previewPxPerMM / 2
	if radius < 1 {
		radius = 1
	}
	r := int(math.Ceil(radius))
	for _, p := range points {
		cx, cy := p[0]*previewPxPerMM, p[1]*previewPxPerMM
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				x, y := int(cx)+dx, int(cy)+dy
				if x < 0 || x >= w || y < 0 || y >= h {
					continue
				}
				fx, fy := float64(x)+0.5-cx, float64(y)+0.5-cy
				if fx*fx+fy*fy <= radius*radius {
					canvas.SetGray(x, y, color.Gray{Y: 0})
				}
			}
		}
	}
	return canvas
}
