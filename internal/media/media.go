package media

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/chai2010/webp"
	"github.com/nfnt/resize"

	"traces/internal/models"
)

const (
	thumbMaxDim = 300
	smMaxDim    = 640
	mdMaxDim    = 1280
	FullMaxDim  = 1920

	webpQuality = 80
)

const (
	defaultJPEGQuality   = 82
	defaultNominatimURL  = "https://nominatim.openstreetmap.org/reverse"
	defaultPosterCapture = "00:00:01"
	defaultPosterScale   = "scale=640:-2"
)

// Config holds media-related configuration.
type Config struct {
	MediaPath     string
	JPEGQuality   int
	NominatimURL  string
	PosterCapture string
	PosterScale   string
}

// LoadConfig reads TRACES_JPEG_QUALITY and TRACES_NOMINATIM_URL from the
// environment over the media package defaults.
func LoadConfig(mediaPath string) Config {
	cfg := Config{
		MediaPath:     mediaPath,
		JPEGQuality:   defaultJPEGQuality,
		NominatimURL:  defaultNominatimURL,
		PosterCapture: defaultPosterCapture,
		PosterScale:   defaultPosterScale,
	}
	if q := os.Getenv("TRACES_JPEG_QUALITY"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= 100 {
			cfg.JPEGQuality = n
		}
	}
	if u := os.Getenv("TRACES_NOMINATIM_URL"); u != "" {
		cfg.NominatimURL = u
	}
	return cfg
}

// Media provides media operations configured via Config.
type Media struct{ cfg Config }

// New creates a Media service from cfg.
func New(cfg Config) *Media { return &Media{cfg: cfg} }

func (m *Media) MediaPath() string { return m.cfg.MediaPath }

// ResizeImage downscales img to fit within maxDim x maxDim using Lanczos3
// resampling. Images already within the limit are returned unchanged
// (no upscaling).
func ResizeImage(img image.Image, maxDim int) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w <= 0 || h <= 0 {
		return img
	}
	if w <= maxDim && h <= maxDim {
		return img
	}

	ratio := float64(maxDim) / float64(max(w, h))
	newW := uint(max(1, int(float64(w)*ratio)))
	newH := uint(max(1, int(float64(h)*ratio)))

	return resize.Resize(newW, newH, img, resize.Lanczos3)
}

// SaveImage encodes img to path in the given format. JPEG quality is
// configurable via Config.JPEGQuality.
func (m *Media) SaveImage(path string, img image.Image, format string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	switch format {
	case "png":
		return png.Encode(f, img)
	default:
		return jpeg.Encode(f, img, &jpeg.Options{Quality: m.cfg.JPEGQuality})
	}
}

func saveWebP(path string, img image.Image, quality float32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return webp.Encode(f, img, &webp.Options{Quality: quality})
}

// WriteImageVariants emits the responsive variant set for an uploaded image
// into mediaBase/subDir using the content-hash base name.
func (m *Media) WriteImageVariants(mediaBase, subDir, hashStr, ext, format string, img image.Image) map[string]string {
	variants := map[string]string{}
	base := filepath.Join(mediaBase, subDir)

	thumb := ResizeImage(img, thumbMaxDim)
	thumbFilename := hashStr + "_thumb" + ext
	if err := m.SaveImage(filepath.Join(base, thumbFilename), thumb, format); err == nil {
		variants["thumb"] = "/media/" + subDir + "/" + thumbFilename
	}

	if format == "jpeg" || format == "png" {
		sm := ResizeImage(img, smMaxDim)
		smFilename := hashStr + "_sm.webp"
		if err := saveWebP(filepath.Join(base, smFilename), sm, webpQuality); err == nil {
			variants["sm"] = "/media/" + subDir + "/" + smFilename
		}

		md := ResizeImage(img, mdMaxDim)
		mdFilename := hashStr + "_md.webp"
		if err := saveWebP(filepath.Join(base, mdFilename), md, webpQuality); err == nil {
			variants["md"] = "/media/" + subDir + "/" + mdFilename
		}
	}

	return variants
}

func videoPosterFilename(hashStr string) string {
	return hashStr + "_thumb.jpg"
}

// ExtractVideoPoster generates a poster-frame thumbnail for a video using the
// optional ffmpeg binary. It returns the public poster URL on success, or ""
// when ffmpeg is absent, times out, or fails.
func (m *Media) ExtractVideoPoster(videoPath, hashStr, subDir string) string {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ""
	}

	posterFilename := videoPosterFilename(hashStr)
	posterPath := filepath.Join(m.cfg.MediaPath, subDir, posterFilename)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", videoPath,
		"-ss", m.cfg.PosterCapture, "-vframes", "1", "-vf", m.cfg.PosterScale, posterPath)
	if err := cmd.Run(); err != nil {
		return ""
	}
	if _, err := os.Stat(posterPath); err != nil {
		return ""
	}
	return "/media/" + subDir + "/" + posterFilename
}

// BuildReverseGeocodeURL constructs the Nominatim reverse-geocode request URL.
func (m *Media) BuildReverseGeocodeURL(lat, lng float64) string {
	return fmt.Sprintf("%s?format=jsonv2&lat=%.6f&lon=%.6f&zoom=16", m.cfg.NominatimURL, lat, lng)
}

// ReverseGeocode resolves GPS coordinates to a human-readable place name via
// Nominatim.
func (m *Media) ReverseGeocode(lat, lng float64) string {
	req, err := http.NewRequest(http.MethodGet, m.BuildReverseGeocodeURL(lat, lng), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "traces/"+models.CurrentVersion)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var result struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	return result.DisplayName
}
