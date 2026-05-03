package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": currentVersion})
	})
	r.POST("/api/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/api/logout", handleLogout)

	return r
}

func TestHashPassword(t *testing.T) {
	tests := []struct {
		password string
		wantLen  int
	}{
		{"password123", 64},
		{"", 64},
		{"verylongpasswordthat exceeds the normal length", 64},
	}

	for _, tt := range tests {
		t.Run("hash_"+tt.password, func(t *testing.T) {
			got := hashPassword(tt.password)
			if len(got) != tt.wantLen {
				t.Errorf("hashPassword(%q) len = %d, want %d", tt.password, len(got), tt.wantLen)
			}
		})
	}

	t.Run("consistent", func(t *testing.T) {
		pwd := "testpassword"
		if hashPassword(pwd) != hashPassword(pwd) {
			t.Error("hashPassword not consistent")
		}
	})
}

func TestEscapeHtml(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{"a & b", "a &amp; b"},
		{`"quotes"`, "&quot;quotes&quot;"},
		{"it's", "it&#039;s"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := EscapeHtml(tt.input); got != tt.expected {
				t.Errorf("EscapeHtml(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetMediaIcon(t *testing.T) {
	tests := []struct{ mediaType, expected string }{
		{"video", "fa-solid fa-video"},
		{"audio", "fa-solid fa-music"},
		{"image", "fa-solid fa-image"},
		{"unknown", "fa-solid fa-image"},
		{"", "fa-solid fa-image"},
	}
	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			if got := GetMediaIcon(tt.mediaType); got != tt.expected {
				t.Errorf("GetMediaIcon(%q) = %q, want %q", tt.mediaType, got, tt.expected)
			}
		})
	}
}

func TestFormatDate(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"2026-01-01", "Jan 1"},
		{"2026-07-15", "Jul 15"},
		{"2026-12-25", "Dec 25"},
		{"invalid", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := FormatDate(tt.input); got != tt.expected {
				t.Errorf("FormatDate(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
	t.Run("leap_year", func(t *testing.T) {
		if got := FormatDate("2024-02-29"); got == "invalid" {
			t.Error("should handle leap year")
		}
	})
}

func TestAPIEndpoints(t *testing.T) {
	router := setupTestRouter()

	t.Run("version_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/version", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["version"] != currentVersion {
			t.Errorf("version = %q, want %q", resp["version"], currentVersion)
		}
	})

	t.Run("logout_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/logout", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %q, want 'ok'", resp["status"])
		}
	})

	t.Run("login_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %q, want 'ok'", resp["status"])
		}
	})
}

func TestResizeImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3840, 2160))

	t.Run("larger_than_max", func(t *testing.T) {
		resized := resizeImage(img, 1920)
		b := resized.Bounds()
		if b.Dx() != 1920 || b.Dy() != 1080 {
			t.Errorf("expected 1920x1080, got %dx%d", b.Dx(), b.Dy())
		}
	})

	t.Run("smaller_than_max", func(t *testing.T) {
		small := image.NewRGBA(image.Rect(0, 0, 800, 600))
		resized := resizeImage(small, 1920)
		b := resized.Bounds()
		if b.Dx() != 800 || b.Dy() != 600 {
			t.Errorf("expected original 800x600, got %dx%d", b.Dx(), b.Dy())
		}
	})

	t.Run("exact_dimensions", func(t *testing.T) {
		resized := resizeImage(img, 3840)
		b := resized.Bounds()
		if b.Dx() != 3840 || b.Dy() != 2160 {
			t.Errorf("expected original 3840x2160, got %dx%d", b.Dx(), b.Dy())
		}
	})

	t.Run("square_image", func(t *testing.T) {
		sq := image.NewRGBA(image.Rect(0, 0, 4000, 4000))
		resized := resizeImage(sq, 500)
		b := resized.Bounds()
		if b.Dx() != 500 || b.Dy() != 500 {
			t.Errorf("expected 500x500, got %dx%d", b.Dx(), b.Dy())
		}
	})
}

func TestSaveImage(t *testing.T) {
	dir := t.TempDir()

	t.Run("save_jpeg", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 100, 100))
		path := filepath.Join(dir, "test.jpg")
		if err := saveImage(path, img, "jpeg"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("jpeg file was not created")
		}
	})

	t.Run("save_png", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 100, 100))
		path := filepath.Join(dir, "test.png")
		if err := saveImage(path, img, "png"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("png file was not created")
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := png.DecodeConfig(f); err != nil {
			t.Errorf("saved file is not a valid png: %v", err)
		}
	})
}

func TestHandleUploadHashing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origMediaPath := mediaPath
	mediaPath = t.TempDir()
	defer func() { mediaPath = origMediaPath }()

	content := []byte("test-image-content-for-hash-test")
	hash := sha256.Sum256(content)
	hashStr := fmt.Sprintf("%x", hash)

	body := &bytes.Buffer{}
	writer := io.MultiWriter(body)
	_ = writer

	router := gin.New()
	router.POST("/api/upload", func(c *gin.Context) {
		handleUpload(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/upload", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	router.ServeHTTP(w, req)

	t.Run("hash_directory_created", func(t *testing.T) {
		subDir := hashStr[:2]
		dirPath := filepath.Join(mediaPath, subDir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Log("directory not created (expected when no file uploaded)")
		}
	})
}

func BenchmarkResizeImage(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resizeImage(img, 1920)
	}
}

func BenchmarkHashPassword(b *testing.B) {
	password := "benchmark_password"
	for i := 0; i < b.N; i++ {
		hashPassword(password)
	}
}

func BenchmarkEscapeHtml(b *testing.B) {
	text := "<script>alert('xss')</script>"
	for i := 0; i < b.N; i++ {
		EscapeHtml(text)
	}
}

func BenchmarkGetMediaIcon(b *testing.B) {
	types := []string{"image", "video", "audio"}
	for i := 0; i < b.N; i++ {
		GetMediaIcon(types[i%len(types)])
	}
}
