// Package imagetoposes implements a camera component that converts an image
// into drawable poses via its get_poses DoCommand and serves a preview of
// the generated points as its camera image.
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
	"time"

	"github.com/golang/geo/r3"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
	rdkutils "go.viam.com/rdk/utils"

	"portrait-drawing/paperimage"
)

// Model is the full model triplet for this camera.
var Model = resource.NewModel("chess-piece-detection", "portrait-drawing", "image-to-poses")

const (
	// Default paper: US Letter, long side along x, and a 1in margin.
	defaultPaperWidthMM  = 279.4
	defaultPaperHeightMM = 215.9
	defaultMarginMM      = 25.4
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
	// PaperXMM is the x position in millimeters (arm world frame) of the
	// paper's near edge. The paper extends from there along +x for
	// paper_width_mm and is centered on y = 0. Required.
	PaperXMM *float64 `json:"paper_x_mm"`
	// PaperWidthMM and PaperHeightMM are the paper's extent along x and y.
	// Default to US Letter landscape: 279.4 x 215.9 (11in x 8.5in).
	PaperWidthMM  float64 `json:"paper_width_mm"`
	PaperHeightMM float64 `json:"paper_height_mm"`
	// MarginMM is the border kept clear on all four sides of the paper; the
	// image is scaled to fit inside the remaining area, preserving aspect
	// ratio, and centered. Defaults to 25.4 (1in).
	MarginMM *float64 `json:"margin_mm"`
	// ImageUp says which way the top of the image points on the table: "+x"
	// (away from the arm, the default) or "-x" (toward the arm).
	ImageUp string `json:"image_up"`
	// Mirror flips the image left-to-right before drawing, so a portrait
	// drawn from a camera reads like the subject's reflection. Defaults to
	// true.
	Mirror *bool `json:"mirror"`
	// FitToContent crops the image to the bounding box of its dark pixels
	// (plus a small padding) before scaling, so the subject fills the
	// drawing area instead of the whole camera frame being fitted. Defaults
	// to true.
	FitToContent *bool `json:"fit_to_content"`
	// SurfaceZMM is the z position in millimeters (in the arm's world frame)
	// of the drawing surface; every contact pose is generated at this height.
	// Jog the arm until the pen touches the paper and use its end position's
	// z. Required.
	SurfaceZMM *float64 `json:"surface_z_mm"`
	// PointSpacingMM is the physical spacing in millimeters between generated
	// points: the drawing area is divided into a grid of this cell size, and
	// each cell whose average darkness passes the threshold becomes one pose.
	// Match it to the pen tip width. Required.
	PointSpacingMM *float64 `json:"point_spacing_mm"`
	// Threshold is the grayscale value (0-255) at or below which a grid cell
	// is dark enough to draw; lower values keep only darker cells. Defaults
	// to 128.
	Threshold *float64 `json:"threshold"`
	// HoverAboveMM lifts the pen between points: after each point's pose, an
	// additional pose is generated this many millimeters above it. Set to 0
	// to disable the hover poses. Required.
	HoverAboveMM *float64 `json:"hover_above_mm"`
	// MaxHoverTravelMM guards long traverses: when the XY distance from one
	// point to the next exceeds it, an extra hover pose above the next point
	// is inserted and marked linear, so the arm crosses the gap flat at
	// hover height instead of arcing. 0 (the default) disables it. Only
	// applies when hover_above_mm is non-zero.
	MaxHoverTravelMM float64 `json:"max_hover_travel_mm"`
	// DenseBlockSize thins solid fills: every fully dark n x n block of grid
	// cells is replaced by a single dot at its center, so dense regions
	// don't take as long to draw. Partially dark blocks (edges, detail)
	// keep all their dots. 0 or 1 (the default) disables it.
	DenseBlockSize float64 `json:"dense_block_size"`
}

// Validate ensures the config is valid.
func (c *Config) Validate(path string) ([]string, []string, error) {
	if c.ImagePath == "" && c.Camera == "" {
		return nil, nil, fmt.Errorf(`%s: exactly one of "image_path" or "camera" is required`, path)
	}
	if c.ImagePath != "" && c.Camera != "" {
		return nil, nil, fmt.Errorf(`%s: set only one of "image_path" and "camera"`, path)
	}
	if c.PaperXMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "paper_x_mm")
	}
	if c.PaperWidthMM < 0 || c.PaperHeightMM < 0 {
		return nil, nil, fmt.Errorf("paper_width_mm and paper_height_mm must be positive, got %v x %v", c.PaperWidthMM, c.PaperHeightMM)
	}
	if c.MarginMM != nil {
		if *c.MarginMM < 0 {
			return nil, nil, fmt.Errorf("margin_mm must be non-negative, got %v", *c.MarginMM)
		}
		w, h := c.paperSize()
		if 2*(*c.MarginMM) >= w || 2*(*c.MarginMM) >= h {
			return nil, nil, fmt.Errorf("margin_mm %v leaves no drawing area on %v x %v paper", *c.MarginMM, w, h)
		}
	}
	switch c.ImageUp {
	case "", "+x", "-x":
	default:
		return nil, nil, fmt.Errorf(`image_up must be "+x" or "-x", got %q`, c.ImageUp)
	}
	if c.SurfaceZMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "surface_z_mm")
	}
	if c.PointSpacingMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "point_spacing_mm")
	}
	if *c.PointSpacingMM <= 0 {
		return nil, nil, fmt.Errorf("point_spacing_mm must be positive, got %v", *c.PointSpacingMM)
	}
	if c.Threshold != nil && (*c.Threshold < 0 || *c.Threshold > 255) {
		return nil, nil, fmt.Errorf("threshold must be between 0 and 255, got %v", *c.Threshold)
	}
	if c.MaxHoverTravelMM < 0 {
		return nil, nil, fmt.Errorf("max_hover_travel_mm must be non-negative, got %v", c.MaxHoverTravelMM)
	}
	if c.DenseBlockSize < 0 || c.DenseBlockSize != float64(int(c.DenseBlockSize)) {
		return nil, nil, fmt.Errorf("dense_block_size must be a non-negative integer, got %v", c.DenseBlockSize)
	}
	if c.HoverAboveMM == nil {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "hover_above_mm")
	}
	if *c.HoverAboveMM < 0 {
		return nil, nil, fmt.Errorf("hover_above_mm must be non-negative, got %v", *c.HoverAboveMM)
	}
	var deps []string
	if c.Camera != "" {
		deps = append(deps, c.Camera)
	}
	return deps, nil, nil
}

// paperSize returns the configured paper size, applying the defaults.
func (c *Config) paperSize() (float64, float64) {
	w, h := c.PaperWidthMM, c.PaperHeightMM
	if w == 0 {
		w = defaultPaperWidthMM
	}
	if h == 0 {
		h = defaultPaperHeightMM
	}
	return w, h
}

type imageToPosesCamera struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger    logging.Logger
	imagePath string
	// srcCam is the camera the image is read from; nil when image_path is
	// configured instead.
	srcCam camera.Camera
	// Paper geometry in the arm's world frame: the paper spans x in
	// [paperXMM, paperXMM+paperWMM] and y in [-paperHMM/2, paperHMM/2]; the
	// drawing area is the paper inset by marginMM on every side.
	paperXMM    float64
	paperWMM    float64
	paperHMM    float64
	marginMM    float64
	imageUp     string
	mirror      bool
	fitContent  bool
	spacingMM   float64
	threshold   uint8
	hoverMM     float64
	maxTravelMM float64
	denseN      int
	surfaceZMM  float64
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
		srcCam, err = camera.FromProvider(deps, cfg.Camera)
		if err != nil {
			return nil, err
		}
	}
	paperW, paperH := cfg.paperSize()
	margin := defaultMarginMM
	if cfg.MarginMM != nil {
		margin = *cfg.MarginMM
	}
	imageUp := cfg.ImageUp
	if imageUp == "" {
		imageUp = "+x"
	}
	mirror := true
	if cfg.Mirror != nil {
		mirror = *cfg.Mirror
	}
	fitContent := true
	if cfg.FitToContent != nil {
		fitContent = *cfg.FitToContent
	}
	threshold := uint8(defaultThreshold)
	if cfg.Threshold != nil {
		threshold = uint8(*cfg.Threshold)
	}

	return &imageToPosesCamera{
		Named:       conf.ResourceName().AsNamed(),
		logger:      logger,
		imagePath:   cfg.ImagePath,
		srcCam:      srcCam,
		paperXMM:    *cfg.PaperXMM,
		paperWMM:    paperW,
		paperHMM:    paperH,
		marginMM:    margin,
		imageUp:     imageUp,
		mirror:      mirror,
		fitContent:  fitContent,
		spacingMM:   *cfg.PointSpacingMM,
		threshold:   threshold,
		hoverMM:     *cfg.HoverAboveMM,
		maxTravelMM: cfg.MaxHoverTravelMM,
		denseN:      int(cfg.DenseBlockSize),
		surfaceZMM:  *cfg.SurfaceZMM,
	}, nil
}

// sourceImage returns the un-rotated image to convert - read from the
// configured camera, or decoded from the configured PNG file - along with a
// description of the source for logging. Callers that need the drawing
// orientation apply rotate_degrees via rotateImage.
// drawingArea returns the drawing area's origin (its minimum x and y corner)
// and extent along x and y: the paper inset by the margin.
func (s *imageToPosesCamera) drawingArea() (x0, y0, alongX, alongY float64) {
	return s.paperXMM + s.marginMM, -s.paperHMM/2 + s.marginMM,
		s.paperWMM - 2*s.marginMM, s.paperHMM - 2*s.marginMM
}

// transform orients the source image for the table: optionally mirrored
// left-to-right (in the image's own frame), then rotated so the image's top
// points along image_up. After it, image x runs along world x and image y
// along world y.
func (s *imageToPosesCamera) transform(img image.Image) image.Image {
	if s.mirror {
		img = mirrorImage(img)
	}
	// Rotating 90 degrees clockwise sends the image's top to +x; 270 to -x.
	if s.imageUp == "-x" {
		return rotateImage(img, 270)
	}
	return rotateImage(img, 90)
}

// paperCorners lists the paper's corners in world x/y, going around: near
// edge (low x) from -y to +y, then the far edge back.
func (s *imageToPosesCamera) paperCorners() [][2]float64 {
	x0, x1 := s.paperXMM, s.paperXMM+s.paperWMM
	y0, y1 := -s.paperHMM/2, s.paperHMM/2
	return [][2]float64{{x0, y0}, {x0, y1}, {x1, y1}, {x1, y0}}
}

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

// mirrorImage flips the image left-right.
func mirrorImage(img image.Image) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			out.Set(x, y, img.At(bounds.Min.X+w-1-x, bounds.Min.Y+y))
		}
	}
	return out
}

// rotateImage rotates the image clockwise by the given degrees, which must
// be 0, 90, 180, or 270.
func rotateImage(img image.Image, degrees int) image.Image {
	if degrees == 0 {
		return img
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	var out *image.RGBA
	switch degrees {
	case 90:
		out = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := range w {
			for x := range h {
				out.Set(x, y, img.At(bounds.Min.X+y, bounds.Min.Y+h-1-x))
			}
		}
	case 180:
		out = image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				out.Set(x, y, img.At(bounds.Min.X+w-1-x, bounds.Min.Y+h-1-y))
			}
		}
	case 270:
		out = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := range w {
			for x := range h {
				out.Set(x, y, img.At(bounds.Min.X+w-1-y, bounds.Min.Y+x))
			}
		}
	default:
		return img
	}
	return out
}

// Images returns a preview of the XY points the get_poses command would
// produce, computed fresh from the source image on every call: one black dot
// per point drawn on an outline of the paper with its margin, at true scale.
// Unlike get_poses, the image is neither rotated nor mirrored, so the preview
// reads like the source image (the paper is shown in portrait orientation,
// as the image's top points along the paper's long side). Like get_poses,
// "threshold" and
// "point_spacing_mm" in extra override the configured values for this call.
func (s *imageToPosesCamera) Images(
	ctx context.Context,
	filterSourceNames []string,
	extra map[string]any,
) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	threshold, spacingMM, err := s.overrides(extra)
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	img, _, err := s.sourceImage(ctx)
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	if s.fitContent {
		img = cropToContent(img, threshold)
	}
	pngBytes, err := s.previewPNG(img, threshold, spacingMM)
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	named, err := camera.NamedImageFromBytes(pngBytes, "points", rdkutils.MimeTypePNG, data.Annotations{})
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	return []camera.NamedImage{named}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
}

// paper describes the sheet as the upright preview shows it: portrait, since
// the image's top runs along the paper's long (x) side.
func (s *imageToPosesCamera) paper(spacingMM float64) paperimage.Paper {
	return paperimage.Paper{WidthMM: s.paperHMM, HeightMM: s.paperWMM, MarginMM: s.marginMM, SpacingMM: spacingMM}
}

// previewPosition maps a point of the transformed (table-oriented) image,
// given in millimeters within the drawing area, back to the upright preview
// frame: the inverse of transform for image_up and mirror.
func (s *imageToPosesCamera) previewPosition(x, y float64) (u, v float64) {
	_, _, alongX, alongY := s.drawingArea()
	// Upright drawing area is alongY wide (u) and alongX tall (v).
	if s.imageUp == "-x" {
		// 270 clockwise: new_x = orig_y, new_y = W - orig_x.
		u, v = alongY-y, x
	} else {
		// 90 clockwise: new_x = H - orig_y, new_y = orig_x.
		u, v = y, alongX-x
	}
	if s.mirror {
		u = alongY - u
	}
	return u, v
}

// previewPNG renders the upright, unmirrored preview of img (already cropped
// to content if configured) on the paper outline and encodes it as PNG. The
// image's top points along the paper's long (x) side, so in the image's own
// orientation the drawing area is alongY wide and alongX tall.
func (s *imageToPosesCamera) previewPNG(img image.Image, threshold uint8, spacingMM float64) ([]byte, error) {
	_, _, alongX, alongY := s.drawingArea()
	points := imageToPoints(img, threshold, alongY, alongX, spacingMM, s.denseN)
	dots := make([]paperimage.Dot, len(points))
	for i, pt := range points {
		dots[i] = paperimage.Dot{U: pt[0], V: pt[1], Done: true}
	}
	preview := paperimage.Render(s.paper(spacingMM), dots)
	var buf bytes.Buffer
	if err := png.Encode(&buf, preview); err != nil {
		return nil, fmt.Errorf("failed to encode preview image: %w", err)
	}
	return buf.Bytes(), nil
}

// NextPointCloud is unimplemented; this camera only serves 2D images.
func (s *imageToPosesCamera) NextPointCloud(ctx context.Context, extra map[string]any) (pointcloud.PointCloud, error) {
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
func (s *imageToPosesCamera) Geometries(ctx context.Context, extra map[string]any) ([]spatialmath.Geometry, error) {
	return nil, nil
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "get_poses"} - reads the configured PNG file or camera image
//	and returns the dark cells of a point_spacing_mm grid as poses over a
//	size_x_mm x size_y_mm drawing area, computed fresh on every call.
//	Optional "threshold" (0-255, default 128) and "point_spacing_mm" override
//	the defaults for this call; "include_preview": true adds "u" and "v" to
//	each contact pose (its position in millimeters in the upright preview
//	frame, from the drawing area's top-left) and a "preview" object with the
//	preview's paper_width_mm, paper_height_mm and margin_mm, so a caller can
//	render the drawing's progress with paperimage. Each pose has x and y in millimeters inside
//	the paper's drawing area (the paper inset by margin_mm), z at the
//	configured surface_z_mm, and an orientation vector pointing straight down
//	(0, 0, -1) with theta 0, suitable for use as a motion service Move
//	destination. When hover_above_mm is non-zero, each point's pose is
//	followed by an additional pose that far above it, so the pen lifts
//	between points, and the sequence starts and ends with a pose at 10x
//	hover height above the first point.
//	{"command": "get_paper"} - returns the paper's four corners in world x/y
//	("corners", going around from the near edge), plus paper_x_mm,
//	paper_width_mm, paper_height_mm, margin_mm, surface_z_mm and
//	hover_above_mm, for showing where the paper goes.
func (s *imageToPosesCamera) DoCommand(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}

	switch command {
	case "get_poses":
		threshold, spacingMM, err := s.overrides(cmd)
		if err != nil {
			return nil, err
		}

		img, source, err := s.sourceImage(ctx)
		if err != nil {
			return nil, err
		}
		if s.fitContent {
			img = cropToContent(img, threshold)
		}
		img = s.transform(img)

		x0, y0, alongX, alongY := s.drawingArea()
		points := imageToPoints(img, threshold, alongX, alongY, spacingMM, s.denseN)
		s.logger.Infof("converted %s to %d points over a %.0fmm x %.0fmm area at %.1fmm spacing",
			source, len(points), alongX, alongY, spacingMM)

		downward := &spatialmath.OrientationVectorDegrees{OX: 0, OY: 0, OZ: -1, Theta: 0}
		poses := make([]poseEntry, 0, len(points)*2+2)

		// Start and end at 10x hover height above the first point, so the
		// arm approaches from safely above and retreats there when done.
		var homeEntry *poseEntry
		if s.hoverMM > 0 && len(points) > 0 {
			x, y := x0+points[0][0], y0+points[0][1]
			z := s.surfaceZMM
			homeEntry = &poseEntry{pose: spatialmath.NewPose(r3.Vector{X: x, Y: y, Z: z + 10*s.hoverMM}, downward), darkness: 0}
			poses = append(poses, *homeEntry)
		}

		var prevX, prevY float64
		for i, p := range points {
			x, y := x0+p[0], y0+p[1]
			z := s.surfaceZMM
			// Guard long traverses: cross to above the next point flat at
			// hover height, on a straight (linear-constrained) path, before
			// descending.
			if s.hoverMM > 0 && s.maxTravelMM > 0 && i > 0 &&
				math.Hypot(x-prevX, y-prevY) > s.maxTravelMM {
				poses = append(poses, poseEntry{
					pose:     spatialmath.NewPose(r3.Vector{X: x, Y: y, Z: z + s.hoverMM}, downward),
					linear:   true,
					darkness: 0,
				})
			}
			u, v := s.previewPosition(p[0], p[1])
			poses = append(poses, poseEntry{
				pose:     spatialmath.NewPose(r3.Vector{X: x, Y: y, Z: z}, downward),
				darkness: p[2],
				preview:  &[2]float64{u, v},
			})
			if s.hoverMM > 0 {
				poses = append(poses, poseEntry{pose: spatialmath.NewPose(r3.Vector{X: x, Y: y, Z: z + s.hoverMM}, downward), darkness: 0})
			}
			prevX, prevY = x, y
		}
		if homeEntry != nil {
			poses = append(poses, *homeEntry)
		}

		include, _ := cmd["include_preview"].(bool)
		return s.posesResponse(poses, spacingMM, include), nil
	case "get_paper":
		corners := make([]any, 0, 4)
		for _, c := range s.paperCorners() {
			corners = append(corners, []any{c[0], c[1]})
		}
		return map[string]any{
			"corners":         corners,
			"paper_x_mm":      s.paperXMM,
			"paper_width_mm":  s.paperWMM,
			"paper_height_mm": s.paperHMM,
			"margin_mm":       s.marginMM,
			"surface_z_mm":    s.surfaceZMM,
			"hover_above_mm":  s.hoverMM,
		}, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

// overrides returns the threshold and point spacing to use for one call:
// the configured values unless m carries a valid "threshold" (0-255) or
// "point_spacing_mm" (> 0) override.
func (s *imageToPosesCamera) overrides(m map[string]any) (uint8, float64, error) {
	threshold := s.threshold
	if t, ok := m["threshold"].(float64); ok {
		if t < 0 || t > 255 {
			return 0, 0, fmt.Errorf("threshold must be between 0 and 255, got %v", t)
		}
		threshold = uint8(t)
	}
	spacingMM := s.spacingMM
	if sp, ok := m["point_spacing_mm"].(float64); ok {
		if sp <= 0 {
			return 0, 0, fmt.Errorf("point_spacing_mm must be positive, got %v", sp)
		}
		spacingMM = sp
	}
	return threshold, spacingMM, nil
}

// poseEntry is a pose in the drawing sequence; linear marks poses that
// should be reached on a straight-line (linear-constrained) path.
type poseEntry struct {
	pose   spatialmath.Pose
	linear bool
	// 0 is not dark at all, 1 is full darkness
	darkness float64
	// preview is the dot's position in the upright preview frame, in
	// millimeters from the drawing area's top-left; set for contact poses.
	preview *[2]float64
}

// posesResponse builds the get_poses response for a set of poses.
func (s *imageToPosesCamera) posesResponse(poses []poseEntry, spacingMM float64, includePreview bool) map[string]any {
	out := make([]any, len(poses))
	for i, entry := range poses {
		pt := entry.pose.Point()
		m := map[string]any{
			"x":        pt.X,
			"y":        pt.Y,
			"z":        pt.Z,
			"o_x":      0.0,
			"o_y":      0.0,
			"o_z":      -1.0,
			"theta":    0.0,
			"linear":   entry.linear,
			"darkness": entry.darkness,
		}
		if includePreview && entry.preview != nil {
			m["u"], m["v"] = entry.preview[0], entry.preview[1]
		}
		out[i] = m
	}
	resp := map[string]any{
		"poses":            out,
		"count":            len(out),
		"size_x_mm":        s.paperWMM - 2*s.marginMM,
		"size_y_mm":        s.paperHMM - 2*s.marginMM,
		"point_spacing_mm": spacingMM,
	}
	if includePreview {
		// The paper as the upright preview shows it (portrait).
		paper := s.paper(spacingMM)
		resp["preview"] = map[string]any{
			"paper_width_mm":  paper.WidthMM,
			"paper_height_mm": paper.HeightMM,
			"margin_mm":       paper.MarginMM,
		}
	}
	return resp
}

// contentPaddingFrac is the padding added around the dark pixels' bounding
// box by cropToContent, as a fraction of the box's larger side.
const contentPaddingFrac = 0.03

// cropToContent returns the sub-image covering every pixel at or below
// threshold, padded by contentPaddingFrac and clamped to the image. An image
// with no dark pixels is returned unchanged.
func cropToContent(img image.Image, threshold uint8) image.Image {
	bounds := img.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X-1, bounds.Min.Y-1
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y <= threshold {
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxX < minX {
		return img
	}
	pad := int(math.Ceil(contentPaddingFrac * float64(max(maxX-minX+1, maxY-minY+1))))
	crop := image.Rect(minX-pad, minY-pad, maxX+pad+1, maxY+pad+1).Intersect(bounds)
	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(crop)
	}
	out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := crop.Min.Y; y < crop.Max.Y; y++ {
		for x := crop.Min.X; x < crop.Max.X; x++ {
			out.Set(x-crop.Min.X, y-crop.Min.Y, img.At(x, y))
		}
	}
	return out
}

// imageToPoints converts the image to points (in millimeters) on a grid of
// spacingMM cells. The image is scaled to fit within a sizeXMM x sizeYMM
// area, preserving aspect ratio, and centered; the origin is the top-left
// corner. Each grid cell whose average grayscale value is at or below
// threshold becomes one point at the cell's center, so spacingMM controls
// the density of the output regardless of the image's resolution.
//
// When denseN is greater than 1, every fully dark denseN x denseN block of
// cells is replaced by a single dot at its center, thinning solid fills.
//
// Points are ordered by greedy nearest neighbor: the first point is the one
// closest to the area's top-left corner, and each subsequent point is the
// unvisited point closest to the previous one.
func imageToPoints(img image.Image, threshold uint8, sizeXMM, sizeYMM, spacingMM float64, denseN int) [][3]float64 {
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

	// Mark the dark cells and how dark each one is.
	dark := make([]grayscale_color, cols*rows)
	darknesses := make([]float64, cols*rows)
	for cy := range rows {
		for cx := range cols {
			cell := cy*cols + cx
			if counts[cell] > 0 {
				avg := sums[cell] / float64(counts[cell])
				if avg <= float64(max(0, threshold/2)) {
					dark[cell] = grayscale_black
				} else if avg <= float64(threshold) {
					dark[cell] = grayscale_gray
				} else {
					dark[cell] = grayscale_white
				}
				darknesses[cell] = cellDarkness(avg, float64(threshold))
			} else {
				dark[cell] = grayscale_white
			}
		}
	}
	collapseDenseBlocks(dark, darknesses, cols, rows, denseN)

	// Count the remaining dark cells and find the one closest to the area's
	// top-left corner (0, 0) to start from.
	total := 0
	startCX, startCY, bestStart := 0, 0, math.MaxFloat64
	for cy := range rows {
		for cx := range cols {
			if dark[cy*cols+cx] == grayscale_white {
				continue
			}
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
	points := make([][3]float64, 0, total)
	cx, cy := startCX, startCY
	for {
		dark[cy*cols+cx] = grayscale_white
		points = append(points, [3]float64{
			xOffset + (float64(cx)+0.5)*spacingMM,
			yOffset + (float64(cy)+0.5)*spacingMM,
			darknesses[cy*cols+cx],
		})
		if len(points) == total {
			return points
		}
		cx, cy = nearestDark(dark, cols, rows, cx, cy)
	}
}

// collapseDenseBlocks replaces every fully dark n x n block of grid cells
// with its single center cell. Blocks are tiled from the grid's top-left;
// partial blocks at the right/bottom edges and blocks with any light cell
// are left untouched.
func collapseDenseBlocks(dark []grayscale_color, darknesses []float64, cols, rows, n int) {
	if n <= 1 {
		return
	}
	for by := 0; by+n <= rows; by += n {
		for bx := 0; bx+n <= cols; bx += n {
			full := true
			numGray := 0
			numFullBlack := 0
			for cy := by; cy < by+n && full; cy++ {
				for cx := bx; cx < bx+n; cx++ {
					color := dark[cy*cols+cx]
					if color == grayscale_white {
						full = false
						break
					}
					if color == grayscale_black {
						numFullBlack++
					} else {
						numGray++
					}
				}
			}
			if !full {
				continue
			}
			for cy := by; cy < by+n; cy++ {
				for cx := bx; cx < bx+n; cx++ {
					dark[cy*cols+cx] = grayscale_white
					darknesses[cy*cols+cx] = 0
				}
			}
			if numFullBlack > numGray {
				dark[(by+n/2)*cols+(bx+n/2)] = grayscale_black
			} else {
				dark[(by+n/2)*cols+(bx+n/2)] = grayscale_gray
			}
			darknesses[(by+n/2)*cols+(bx+n/2)] = float64(numFullBlack) / (float64(numFullBlack) + float64(numGray))
		}
	}
}

// cellDarkness maps a grid cell's average gray value to a dwell level: 1.0 at
// or below the black cutoff (half the threshold), ramping linearly down to 0 at
// the threshold itself, where the cell is too light to draw at all.
func cellDarkness(avg, threshold float64) float64 {
	black := threshold / 2
	if avg <= black {
		return 1
	}
	if avg >= threshold {
		return 0
	}
	return (threshold - avg) / (threshold - black)
}

// nearestDark returns the dark cell closest (by Euclidean distance) to
// (fromCX, fromCY), searching outward ring by ring. A match found in ring r
// is only accepted once no closer match can exist in a farther ring.
func nearestDark(dark []grayscale_color, cols, rows, fromCX, fromCY int) (int, int) {
	maxR := max(rows, cols)
	bestCX, bestCY, bestD := -1, -1, math.MaxInt
	check := func(cx, cy int) {
		if cx < 0 || cx >= cols || cy < 0 || cy >= rows || dark[cy*cols+cx] == grayscale_white {
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
