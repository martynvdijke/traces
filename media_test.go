package main

import (
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return img
}

func TestResizeImageLanczos(t *testing.T) {
	img := makeTestImage(800, 400)

	t.Run("downscales_to_max_dim", func(t *testing.T) {
		resized := resizeImage(img, 300)
		b := resized.Bounds()
		if b.Dx() != 300 || b.Dy() != 150 {
			t.Errorf("expected 300x150, got %dx%d", b.Dx(), b.Dy())
		}
	})

	t.Run("no_upscale_when_smaller", func(t *testing.T) {
		small := makeTestImage(100, 50)
		resized := resizeImage(small, 300)
		b := resized.Bounds()
		if b.Dx() != 100 || b.Dy() != 50 {
			t.Errorf("expected original 100x50, got %dx%d", b.Dx(), b.Dy())
		}
	})
}

func TestWriteImageVariants(t *testing.T) {
	mediaBase := t.TempDir()
	subDir := "2026/08"
	// In production handleUpload creates the subdirectory before writing
	// variants; mirror that here.
	if err := os.MkdirAll(filepath.Join(mediaBase, subDir), 0o755); err != nil {
		t.Fatal(err)
	}
	img := makeTestImage(800, 600)

	t.Run("jpeg_source_writes_all_variants", func(t *testing.T) {
		variants := writeImageVariants(mediaBase, subDir, "hash123", ".jpg", "jpeg", img)

		wantThumb := "/media/" + subDir + "/hash123_thumb.jpg"
		wantSm := "/media/" + subDir + "/hash123_sm.webp"
		wantMd := "/media/" + subDir + "/hash123_md.webp"

		if variants["thumb"] != wantThumb {
			t.Errorf("thumb = %q, want %q", variants["thumb"], wantThumb)
		}
		if variants["sm"] != wantSm {
			t.Errorf("sm = %q, want %q", variants["sm"], wantSm)
		}
		if variants["md"] != wantMd {
			t.Errorf("md = %q, want %q", variants["md"], wantMd)
		}

		for _, f := range []string{"hash123_thumb.jpg", "hash123_sm.webp", "hash123_md.webp"} {
			info, err := os.Stat(filepath.Join(mediaBase, subDir, f))
			if err != nil {
				t.Errorf("variant %s missing: %v", f, err)
				continue
			}
			if info.Size() == 0 {
				t.Errorf("variant %s is empty", f)
			}
		}
	})

	t.Run("gif_source_skips_webp", func(t *testing.T) {
		variants := writeImageVariants(mediaBase, subDir, "gif1", ".gif", "gif", img)

		if variants["thumb"] == "" {
			t.Error("expected thumb variant for gif source")
		}
		if variants["sm"] != "" || variants["md"] != "" {
			t.Errorf("unexpected webp variants for gif source: %v", variants)
		}
		if _, err := os.Stat(filepath.Join(mediaBase, subDir, "gif1_sm.webp")); !os.IsNotExist(err) {
			t.Error("gif source should not produce _sm.webp")
		}
	})
}

func TestVideoPosterFilename(t *testing.T) {
	if got := videoPosterFilename("abc123"); got != "abc123_thumb.jpg" {
		t.Errorf("videoPosterFilename = %q, want %q", got, "abc123_thumb.jpg")
	}
}

func TestExtractVideoPosterFallback(t *testing.T) {
	// A nonexistent source video must yield "" regardless of whether ffmpeg is
	// installed: without ffmpeg the helper bails early, with it the command
	// fails on the missing input file.
	got := extractVideoPoster(filepath.Join(t.TempDir(), "missing.mp4"), "h1", "test")
	if got != "" {
		t.Errorf("extractVideoPoster = %q, want empty string on failure", got)
	}
}

func TestBuildReverseGeocodeURL(t *testing.T) {
	orig := nominatimURL
	nominatimURL = "https://nominatim.example.org/reverse"
	defer func() { nominatimURL = orig }()

	want := "https://nominatim.example.org/reverse?format=jsonv2&lat=40.712800&lon=-74.006000&zoom=16"
	if got := buildReverseGeocodeURL(40.7128, -74.006); got != want {
		t.Errorf("buildReverseGeocodeURL = %q, want %q", got, want)
	}
}

func TestReverseGeocode(t *testing.T) {
	t.Run("returns_display_name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") == "" {
				t.Error("expected User-Agent header per OSM usage policy")
			}
			if !strings.Contains(r.URL.RawQuery, "zoom=16") {
				t.Errorf("expected zoom=16 in query, got %q", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"display_name":"Brooklyn, New York"}`))
		}))
		defer server.Close()

		orig := nominatimURL
		nominatimURL = server.URL
		defer func() { nominatimURL = orig }()

		if got := reverseGeocode(40.6782, -73.9442); got != "Brooklyn, New York" {
			t.Errorf("reverseGeocode = %q, want %q", got, "Brooklyn, New York")
		}
	})

	t.Run("silent_failure_on_error_status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		}))
		defer server.Close()

		orig := nominatimURL
		nominatimURL = server.URL
		defer func() { nominatimURL = orig }()

		if got := reverseGeocode(40.6782, -73.9442); got != "" {
			t.Errorf("reverseGeocode = %q, want empty string on non-200", got)
		}
	})
}
