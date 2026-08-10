package main

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
)

// Media variant dimensions (max edge in px). The full-size variant is capped
// at fullMaxDim and keeps the original format; _sm/_md are WebP re-encodes.
const (
	thumbMaxDim = 300
	smMaxDim    = 640
	mdMaxDim    = 1280
	fullMaxDim  = 1920

	webpQuality = 80
)

var (
	jpegQuality   = 82
	nominatimURL  = "https://nominatim.openstreetmap.org/reverse"
	posterCapture = "00:00:01"
	posterScale   = "scale=640:-2"
)

func init() {
	if q := os.Getenv("TRACES_JPEG_QUALITY"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= 100 {
			jpegQuality = n
		}
	}
	if u := os.Getenv("TRACES_NOMINATIM_URL"); u != "" {
		nominatimURL = u
	}
}

// resizeImage downscales img to fit within maxDim x maxDim using Lanczos3
// resampling. Images already within the limit are returned unchanged
// (no upscaling).
func resizeImage(img image.Image, maxDim int) image.Image {
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

// saveImage encodes img to path in the given format. JPEG quality is
// configurable via TRACES_JPEG_QUALITY (default 82). Variants are re-encoded
// from the decoded image, which strips EXIF/GPS metadata.
func saveImage(path string, img image.Image, format string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	switch format {
	case "png":
		return png.Encode(f, img)
	default:
		return jpeg.Encode(f, img, &jpeg.Options{Quality: jpegQuality})
	}
}

// saveWebP encodes img to path as lossy WebP at the given quality.
func saveWebP(path string, img image.Image, quality float32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return webp.Encode(f, img, &webp.Options{Quality: quality})
}

// writeImageVariants emits the responsive variant set for an uploaded image
// into mediaBase/subDir using the content-hash base name:
//
//	<base>_thumb.<ext>  — 300px, original format (backward-compatible)
//	<base>_sm.webp      — 640px, WebP (jpeg/png sources only)
//	<base>_md.webp      — 1280px, WebP (jpeg/png sources only)
//
// It returns a map of variant name -> public URL, empty when nothing was
// written. Non-photographic formats (gif/svg/tiff) are handled by callers
// before reaching this function.
func writeImageVariants(mediaBase, subDir, hashStr, ext, format string, img image.Image) map[string]string {
	variants := map[string]string{}
	base := filepath.Join(mediaBase, subDir)

	thumb := resizeImage(img, thumbMaxDim)
	thumbFilename := hashStr + "_thumb" + ext
	if err := saveImage(filepath.Join(base, thumbFilename), thumb, format); err == nil {
		variants["thumb"] = "/media/" + subDir + "/" + thumbFilename
	}

	// WebP variants only for photographic sources; GIF/SVG/TIFF keep the
	// original-format thumbnail as their only variant.
	if format == "jpeg" || format == "png" {
		sm := resizeImage(img, smMaxDim)
		smFilename := hashStr + "_sm.webp"
		if err := saveWebP(filepath.Join(base, smFilename), sm, webpQuality); err == nil {
			variants["sm"] = "/media/" + subDir + "/" + smFilename
		}

		md := resizeImage(img, mdMaxDim)
		mdFilename := hashStr + "_md.webp"
		if err := saveWebP(filepath.Join(base, mdFilename), md, webpQuality); err == nil {
			variants["md"] = "/media/" + subDir + "/" + mdFilename
		}
	}

	return variants
}

// videoPosterFilename returns the poster file name for a video upload.
func videoPosterFilename(hashStr string) string {
	return hashStr + "_thumb.jpg"
}

// extractVideoPoster generates a poster-frame thumbnail for a video using the
// optional ffmpeg binary. It returns the public poster URL on success, or ""
// when ffmpeg is absent, times out, or fails (callers fall back to the generic
// video placeholder).
func extractVideoPoster(videoPath, hashStr, subDir string) string {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ""
	}

	posterFilename := videoPosterFilename(hashStr)
	posterPath := filepath.Join(mediaPath, subDir, posterFilename)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", videoPath,
		"-ss", posterCapture, "-vframes", "1", "-vf", posterScale, posterPath)
	if err := cmd.Run(); err != nil {
		return ""
	}
	if _, err := os.Stat(posterPath); err != nil {
		return ""
	}
	return "/media/" + subDir + "/" + posterFilename
}

// buildReverseGeocodeURL constructs the Nominatim reverse-geocode request URL.
func buildReverseGeocodeURL(lat, lng float64) string {
	return fmt.Sprintf("%s?format=jsonv2&lat=%.6f&lon=%.6f&zoom=16", nominatimURL, lat, lng)
}

// reverseGeocode resolves GPS coordinates to a human-readable place name via
// Nominatim. It is best-effort and always silent on failure: any error returns
// "". The endpoint is configurable via TRACES_NOMINATIM_URL and requests carry
// a polite User-Agent per the OSM usage policy.
func reverseGeocode(lat, lng float64) string {
	req, err := http.NewRequest(http.MethodGet, buildReverseGeocodeURL(lat, lng), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "traces/"+currentVersion)

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
