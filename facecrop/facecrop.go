// Package facecrop implements a camera that crops its source camera's image
// to the largest face it finds, padded so the crop reads as a portrait, using
// the pigo pixel-intensity-comparison face detector (pure Go, no model
// download). When no face is found the image passes through unchanged.
package facecrop

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"time"

	pigo "github.com/esimov/pigo/core"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
	rdkutils "go.viam.com/rdk/utils"
)

// facefinder is pigo's frontal-face cascade (MIT, github.com/esimov/pigo).
//
//go:embed facefinder
var facefinderCascade []byte

// Model is the full model triplet for this camera.
var Model = resource.NewModel("chess-piece-detection", "portrait-drawing", "face-crop")

const (
	// defaultPadding grows the detected face box by this fraction of its size
	// on every side, so the crop includes hair, chin and some shoulders.
	defaultPadding = 0.6
	// defaultMinFacePx is the smallest face (in source pixels) worth cropping
	// to; smaller detections are ignored.
	defaultMinFacePx = 60
	// minQuality is pigo's detection score cutoff; ~5 is its usual default.
	minQuality = 5.0
	// clusterIOU merges overlapping detections of the same face.
	clusterIOU = 0.2
)

func init() {
	resource.RegisterComponent(camera.API, Model, resource.Registration[camera.Camera, *Config]{
		Constructor: newFaceCrop,
	})
}

// Config describes the attributes for this camera.
type Config struct {
	// Camera is the name of the source camera. Required.
	Camera string `json:"camera"`
	// Padding grows the face box by this fraction of its size on every side
	// before cropping. Defaults to 0.6.
	Padding *float64 `json:"padding"`
	// MinFacePx ignores faces smaller than this many source pixels. Defaults
	// to 60.
	MinFacePx float64 `json:"min_face_px"`
}

// Validate ensures the config is valid; camera is required.
func (c *Config) Validate(path string) ([]string, []string, error) {
	if c.Camera == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "camera")
	}
	if c.Padding != nil && *c.Padding < 0 {
		return nil, nil, fmt.Errorf("padding must be non-negative, got %v", *c.Padding)
	}
	if c.MinFacePx < 0 {
		return nil, nil, fmt.Errorf("min_face_px must be non-negative, got %v", c.MinFacePx)
	}
	return []string{c.Camera}, nil, nil
}

type faceCropCamera struct {
	resource.Named
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	logger    logging.Logger
	srcCam    camera.Camera
	padding   float64
	minFacePx int
	detector  *pigo.Pigo
}

func newFaceCrop(
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
	detector, err := pigo.NewPigo().Unpack(facefinderCascade)
	if err != nil {
		return nil, fmt.Errorf("failed to load the face cascade: %w", err)
	}
	padding := defaultPadding
	if cfg.Padding != nil {
		padding = *cfg.Padding
	}
	minFacePx := defaultMinFacePx
	if cfg.MinFacePx != 0 {
		minFacePx = int(cfg.MinFacePx)
	}
	return &faceCropCamera{
		Named:     conf.ResourceName().AsNamed(),
		logger:    logger,
		srcCam:    srcCam,
		padding:   padding,
		minFacePx: minFacePx,
		detector:  detector,
	}, nil
}

// Images returns the source image cropped to the largest detected face,
// padded; or the source image unchanged when no face is found.
func (s *faceCropCamera) Images(
	ctx context.Context,
	filterSourceNames []string,
	extra map[string]interface{},
) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	img, err := s.sourceImage(ctx, extra)
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	face, found := s.findFace(img)
	out := img
	if found {
		out = crop(img, padBox(face, s.padding, img.Bounds()))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, resource.ResponseMetadata{}, fmt.Errorf("failed to encode cropped image: %w", err)
	}
	named, err := camera.NamedImageFromBytes(buf.Bytes(), "face", rdkutils.MimeTypePNG, data.Annotations{})
	if err != nil {
		return nil, resource.ResponseMetadata{}, err
	}
	return []camera.NamedImage{named}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
}

// sourceImage decodes the first image from the source camera.
func (s *faceCropCamera) sourceImage(ctx context.Context, extra map[string]interface{}) (image.Image, error) {
	namedImages, _, err := s.srcCam.Images(ctx, nil, extra)
	if err != nil {
		return nil, fmt.Errorf("failed to get image from source camera: %w", err)
	}
	if len(namedImages) == 0 {
		return nil, errors.New("source camera returned no images")
	}
	img, err := namedImages[0].Image(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to decode source image: %w", err)
	}
	return img, nil
}

// findFace runs the detector and returns the largest face box at least
// minFacePx wide, if any.
func (s *faceCropCamera) findFace(img image.Image) (image.Rectangle, bool) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return image.Rectangle{}, false
	}
	pixels := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pixels[y*w+x] = color.GrayModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray).Y
		}
	}
	params := pigo.CascadeParams{
		MinSize:     s.minFacePx,
		MaxSize:     min(w, h),
		ShiftFactor: 0.1,
		ScaleFactor: 1.1,
		ImageParams: pigo.ImageParams{Pixels: pixels, Rows: h, Cols: w, Dim: w},
	}
	dets := s.detector.ClusterDetections(s.detector.RunCascade(params, 0), clusterIOU)

	best, found := image.Rectangle{}, false
	for _, d := range dets {
		if d.Q < minQuality || d.Scale < s.minFacePx {
			continue
		}
		half := d.Scale / 2
		r := image.Rect(d.Col-half, d.Row-half, d.Col+half, d.Row+half).Add(bounds.Min)
		if !found || r.Dx() > best.Dx() {
			best, found = r, true
		}
	}
	return best, found
}

// padBox grows box by padding times its size on every side and clamps it to
// bounds.
func padBox(box image.Rectangle, padding float64, bounds image.Rectangle) image.Rectangle {
	px := int(math.Round(padding * float64(box.Dx())))
	py := int(math.Round(padding * float64(box.Dy())))
	return image.Rect(box.Min.X-px, box.Min.Y-py, box.Max.X+px, box.Max.Y+py).Intersect(bounds)
}

// crop copies the region r of img into a new image with origin (0, 0).
func crop(img image.Image, r image.Rectangle) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

// NextPointCloud is unimplemented; this camera only serves 2D images.
func (s *faceCropCamera) NextPointCloud(ctx context.Context, extra map[string]interface{}) (pointcloud.PointCloud, error) {
	return nil, errors.New("point clouds are not supported")
}

// Properties returns the intrinsic properties of this camera.
func (s *faceCropCamera) Properties(ctx context.Context) (camera.Properties, error) {
	return camera.Properties{
		SupportsPCD: false,
		ImageType:   camera.ColorStream,
		MimeTypes:   []string{rdkutils.MimeTypePNG},
	}, nil
}

// Geometries returns no geometries; this camera has no physical footprint.
func (s *faceCropCamera) Geometries(ctx context.Context, extra map[string]interface{}) ([]spatialmath.Geometry, error) {
	return nil, nil
}

// DoCommand handles arbitrary commands. Supported commands:
//
//	{"command": "detect"} - runs the detector on the current source image and
//	returns whether a face was "found", its "face" box and the padded "crop"
//	box (each as x, y, width, height in source pixels), and the source image
//	"width" and "height".
func (s *faceCropCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok {
		return nil, fmt.Errorf(`expected a "command" string in the command map, got: %v`, cmd)
	}
	switch command {
	case "detect":
		img, err := s.sourceImage(ctx, nil)
		if err != nil {
			return nil, err
		}
		face, found := s.findFace(img)
		resp := map[string]interface{}{
			"found":  found,
			"width":  img.Bounds().Dx(),
			"height": img.Bounds().Dy(),
		}
		if found {
			resp["face"] = rectMap(face)
			resp["crop"] = rectMap(padBox(face, s.padding, img.Bounds()))
		}
		return resp, nil
	default:
		return nil, fmt.Errorf("unknown command: %q", command)
	}
}

func rectMap(r image.Rectangle) map[string]interface{} {
	return map[string]interface{}{"x": r.Min.X, "y": r.Min.Y, "width": r.Dx(), "height": r.Dy()}
}
