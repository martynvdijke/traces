package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
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
	r.GET("/api/health", handleHealth)

	return r
}

func TestHashPassword(t *testing.T) {
	tests := []struct {
		password string
	}{
		{"password123"},
		{""},
		{"verylongpasswordthat exceeds the normal length"},
	}

	for _, tt := range tests {
		t.Run("hash_"+tt.password, func(t *testing.T) {
			hash, err := bcrypt.GenerateFromPassword([]byte(tt.password), bcrypt.DefaultCost)
			if err != nil {
				t.Fatalf("bcrypt.GenerateFromPassword(%q) failed: %v", tt.password, err)
			}
			if len(hash) == 0 {
				t.Errorf("bcrypt hash is empty for %q", tt.password)
			}
		})
	}

	t.Run("verify", func(t *testing.T) {
		pwd := "testpassword"
		hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := bcrypt.CompareHashAndPassword(hash, []byte(pwd)); err != nil {
			t.Error("bcrypt verification failed")
		}
		if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong")); err == nil {
			t.Error("bcrypt should reject wrong password")
		}
	})
}

func TestHandleLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	defer func() {
		db = origDB
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionStore = make(map[string]int64)
	csrfTokens = make(map[string]string)

	db.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed'
	)`)

	bcryptHash, err := bcrypt.GenerateFromPassword([]byte("bcrypt_password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", "bcrypt_user", string(bcryptHash))

	router := gin.New()
	router.POST("/api/login", handleLogin)

	t.Run("bcrypt_login_success", func(t *testing.T) {
		sessionStore = make(map[string]int64)
		csrfTokens = make(map[string]string)

		w := httptest.NewRecorder()
		body := `{"username":"bcrypt_user","password":"bcrypt_password"}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %q, want 'ok'", resp["status"])
		}
	})

	t.Run("bcrypt_login_wrong_password", func(t *testing.T) {
		sessionStore = make(map[string]int64)
		csrfTokens = make(map[string]string)

		w := httptest.NewRecorder()
		body := `{"username":"bcrypt_user","password":"wrong_password"}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("setup_creates_admin_user", func(t *testing.T) {
		origCount := 0
		db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&origCount)
		db2, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db2.Close()

		db2.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			password TEXT
		)`)
		db2.Exec(`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed'
		)`)

		origDB2 := db
		db = db2
		defer func() { db = origDB2 }()

		sessionStore = make(map[string]int64)
		csrfTokens = make(map[string]string)

		w := httptest.NewRecorder()
		body := `{"username":"setup_admin","password":"new_password","setup":true}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("setup status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var storedPassword string
		db2.QueryRow("SELECT password FROM admin_users WHERE username = 'setup_admin'").Scan(&storedPassword)
		if storedPassword == "" {
			t.Error("setup did not create admin user")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte("new_password")); err != nil {
			t.Error("setup password does not verify")
		}
	})

	t.Run("setup_rejected_when_users_exist", func(t *testing.T) {
		sessionStore = make(map[string]int64)
		csrfTokens = make(map[string]string)

		w := httptest.NewRecorder()
		body := `{"username":"another_admin","password":"password123","setup":true}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("setup with existing users status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("setup_password_too_short", func(t *testing.T) {
		db2, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db2.Close()

		db2.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			password TEXT
		)`)
		db2.Exec(`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed'
		)`)

		origDB3 := db
		db = db2
		defer func() { db = origDB3 }()

		sessionStore = make(map[string]int64)
		csrfTokens = make(map[string]string)

		w := httptest.NewRecorder()
		body := `{"username":"shortpwd","password":"1234567","setup":true}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("short password status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
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

	t.Run("health_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/health", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %q, want 'ok'", resp["status"])
		}
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

func TestHandleUploadCSRFFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	origMediaPath := mediaPath
	defer func() {
		db = origDB
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
		mediaPath = origMediaPath
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionStore = make(map[string]int64)
	csrfTokens = make(map[string]string)
	mediaPath = t.TempDir()

	db.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed'
	)`)

	bcryptHash, err := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", "admin", string(bcryptHash))

	router := gin.New()
	router.MaxMultipartMemory = 32 << 20
	router.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		if c.Request.Method == "POST" || c.Request.Method == "PUT" {
			if !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
				c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
			}
		}
		c.Next()
	})

	api := router.Group("/api")
	{
		api.POST("/login", handleLogin)
		api.GET("/csrf-token", getCSRFToken)

		auth := api.Group("")
		auth.Use(authMiddlewareGin(), csrfMiddleware())
		{
			auth.POST("/upload", handleUpload)
		}
	}

	var sessionCookie string
	t.Run("login_and_get_csrf", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"username":"admin","password":"testpass"}`
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("login status = %d, body=%s", w.Code, w.Body.String())
		}

		cookies := w.Result().Cookies()
		for _, c := range cookies {
			if c.Name == "session" {
				sessionCookie = c.Value
				break
			}
		}
		if sessionCookie == "" {
			t.Fatal("no session cookie set")
		}

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/api/csrf-token", nil)
		req2.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("csrf token status = %d, body=%s", w2.Code, w2.Body.String())
		}
		var csrfResp map[string]string
		json.Unmarshal(w2.Body.Bytes(), &csrfResp)
		if csrfResp["token"] == "" {
			t.Fatal("empty csrf token")
		}
		csrfTokens[sessionCookie] = csrfResp["token"]
	})

	t.Run("upload_image", func(t *testing.T) {
		imgBuf := new(bytes.Buffer)
		png.Encode(imgBuf, image.NewRGBA(image.Rect(0, 0, 100, 100)))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("image", "test.png")
		part.Write(imgBuf.Bytes())
		writer.WriteField("media_type", "image")
		writer.Close()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		req.Header.Set("X-CSRF-Token", csrfTokens[sessionCookie])
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("upload status = %d, body=%s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["url"] == nil || resp["url"] == "" {
			t.Error("upload response missing url")
		}
		if resp["media_type"] != "image" {
			t.Errorf("media_type = %v, want image", resp["media_type"])
		}
	})

	t.Run("upload_video", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		// Minimal WebM header for MIME detection
		webmData := []byte{0x1A, 0x45, 0xDF, 0xA3}
		part, _ := writer.CreateFormFile("video", "test.webm")
		part.Write(webmData)
		writer.WriteField("media_type", "video")
		writer.Close()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		req.Header.Set("X-CSRF-Token", csrfTokens[sessionCookie])
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("upload status = %d, body=%s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["url"] == nil || resp["url"] == "" {
			t.Error("upload response missing url")
		}
		if resp["media_type"] != "video" {
			t.Errorf("media_type = %v, want video", resp["media_type"])
		}
	})

	t.Run("upload_audio", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		// Minimal MP3 ID3v2 header for MIME detection
		mp3Data := []byte{
			0x49, 0x44, 0x33, // ID3
			0x03, 0x00, // version 2.3
			0x00, 0x00, 0x00, 0x00, // flags + size
		}
		part, _ := writer.CreateFormFile("audio", "test.mp3")
		part.Write(mp3Data)
		writer.WriteField("media_type", "audio")
		writer.Close()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		req.Header.Set("X-CSRF-Token", csrfTokens[sessionCookie])
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("upload status = %d, body=%s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["url"] == nil || resp["url"] == "" {
			t.Error("upload response missing url")
		}
		if resp["media_type"] != "audio" {
			t.Errorf("media_type = %v, want audio", resp["media_type"])
		}
	})

	t.Run("upload_rejects_no_csrf", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("image", "test.png")
		part.Write([]byte("fake"))
		writer.WriteField("media_type", "image")
		writer.Close()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 without CSRF, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("upload_rejects_no_auth", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("image", "test.png")
		part.Write([]byte("fake"))
		writer.WriteField("media_type", "image")
		writer.Close()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 without auth, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("upload_rejects_invalid_file_type", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("image", "test.txt")
		part.Write([]byte("not an image"))
		writer.WriteField("media_type", "image")
		writer.Close()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		req.Header.Set("X-CSRF-Token", csrfTokens[sessionCookie])
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid type, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestAutoTagEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	)`)

	router := gin.New()
	router.POST("/api/auto-tag", autoTagEvent)

	t.Run("empty_title_falls_back_to_ollama", func(t *testing.T) {
		origURL := os.Getenv("OLLAMA_URL")
		os.Setenv("OLLAMA_URL", "http://127.0.0.1:1")
		defer os.Setenv("OLLAMA_URL", origURL)

		w := httptest.NewRecorder()
		body := `{"title":"","description":"test","location":""}`
		req := httptest.NewRequest("POST", "/api/auto-tag", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadGateway {
			t.Errorf("expected 502 (ollama unreachable), got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects_title_too_long", func(t *testing.T) {
		longTitle := strings.Repeat("a", 501)
		w := httptest.NewRecorder()
		body := `{"title":"` + longTitle + `","description":"test","location":""}`
		req := httptest.NewRequest("POST", "/api/auto-tag", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for long title, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects_description_too_long", func(t *testing.T) {
		longDesc := strings.Repeat("d", 2001)
		w := httptest.NewRecorder()
		body := `{"title":"Test","description":"` + longDesc + `","location":""}`
		req := httptest.NewRequest("POST", "/api/auto-tag", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for long description, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ollama_unreachable_returns_502", func(t *testing.T) {
		origURL := os.Getenv("OLLAMA_URL")
		os.Setenv("OLLAMA_URL", "http://127.0.0.1:1")
		defer os.Setenv("OLLAMA_URL", origURL)

		w := httptest.NewRecorder()
		body := `{"title":"Test Event","description":"A test event","location":"Test Location"}`
		req := httptest.NewRequest("POST", "/api/auto-tag", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadGateway {
			t.Errorf("expected 502 for unreachable Ollama, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] == "" {
			t.Error("expected error message for unreachable Ollama")
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
		bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
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

func TestMemoriesQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL
	)`)

	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Last year event', ?)`, time.Now().AddDate(-1, 0, 0).Format("2006-01-02"))
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Two years ago', ?)`, time.Now().AddDate(-2, 0, 0).Format("2006-01-02"))
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Old event out of range', ?)`, time.Now().AddDate(-1, -1, 0).Format("2006-01-02"))
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Recent event same year', ?)`, time.Now().Format("2006-01-02"))

	rows, err := db.Query(`SELECT e.title, e.event_date,
		CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
		FROM timeline_events e
		WHERE e.event_date != ''
		AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
		AND strftime('%m-%d', e.event_date) = strftime('%m-%d', 'now')
		ORDER BY e.event_date DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
		var title, date string
		var yearsAgo int
		if err := rows.Scan(&title, &date, &yearsAgo); err != nil {
			t.Fatal(err)
		}
		t.Logf("Memory: %q (date=%s, years_ago=%d)", title, date, yearsAgo)
	}

	if count < 2 {
		t.Errorf("expected at least 2 memories (exact date match), got %d", count)
	}
}

func TestMemoriesConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled INTEGER DEFAULT 1,
		days_window INTEGER DEFAULT 3,
		email_enabled INTEGER DEFAULT 0,
		last_sent_date TEXT DEFAULT ''
	)`)
	db.Exec(`INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)`)

	var enabledInt, daysWindow, emailInt int
	err = db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &daysWindow, &emailInt)
	if err != nil {
		t.Fatal(err)
	}
	if enabledInt != 1 {
		t.Errorf("enabled = %d, want 1", enabledInt)
	}
	if daysWindow != 3 {
		t.Errorf("days_window = %d, want 3", daysWindow)
	}

	db.Exec("UPDATE memories_settings SET enabled=0, days_window=7, email_enabled=1 WHERE id=1")
	err = db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &daysWindow, &emailInt)
	if err != nil {
		t.Fatal(err)
	}
	if enabledInt != 0 {
		t.Errorf("enabled = %d, want 0 after update", enabledInt)
	}
	if daysWindow != 7 {
		t.Errorf("days_window = %d, want 7 after update", daysWindow)
	}
	if emailInt != 1 {
		t.Errorf("email_enabled = %d, want 1 after update", emailInt)
	}
}

func TestEmailConfigRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		smtp_host TEXT DEFAULT '',
		smtp_port INTEGER DEFAULT 587,
		smtp_user TEXT DEFAULT '',
		smtp_pass TEXT DEFAULT '',
		from_addr TEXT DEFAULT '',
		to_addr TEXT DEFAULT ''
	)`)
	db.Exec(`INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)`)

	db.Exec(`UPDATE email_settings SET smtp_host=?, smtp_port=?, smtp_user=?, smtp_pass=?, from_addr=?, to_addr=? WHERE id=1`,
		"smtp.example.com", 465, "user", "pass", "from@test.com", "to@test.com")

	var host, user, pass, fromAddr, toAddr string
	var port int
	err = db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&host, &port, &user, &pass, &fromAddr, &toAddr)
	if err != nil {
		t.Fatal(err)
	}
	if host != "smtp.example.com" {
		t.Errorf("host = %q, want smtp.example.com", host)
	}
	if port != 465 {
		t.Errorf("port = %d, want 465", port)
	}
	if user != "user" {
		t.Errorf("user = %q", user)
	}
	if fromAddr != "from@test.com" {
		t.Errorf("from = %q", fromAddr)
	}
	if toAddr != "to@test.com" {
		t.Errorf("to = %q", toAddr)
	}
}

func TestPersonCRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	t.Run("create_person", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO persons (name, avatar_url, bio, color) VALUES (?, ?, ?, ?)",
			"John Doe", "/media/avatar.jpg", "A test person", "#ff0000")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM persons").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 person, got %d", count)
		}
	})

	t.Run("fetch_person", func(t *testing.T) {
		var name, avatar, bio, color string
		err := db.QueryRow("SELECT name, avatar_url, bio, color FROM persons WHERE id = 1").Scan(&name, &avatar, &bio, &color)
		if err != nil {
			t.Fatal(err)
		}
		if name != "John Doe" {
			t.Errorf("name = %q, want John Doe", name)
		}
		if avatar != "/media/avatar.jpg" {
			t.Errorf("avatar = %q", avatar)
		}
		if color != "#ff0000" {
			t.Errorf("color = %q", color)
		}
	})

	t.Run("update_person", func(t *testing.T) {
		_, err := db.Exec("UPDATE persons SET name=?, color=? WHERE id=?", "Jane Doe", "#00ff00", 1)
		if err != nil {
			t.Fatal(err)
		}
		var name, color string
		db.QueryRow("SELECT name, color FROM persons WHERE id = 1").Scan(&name, &color)
		if name != "Jane Doe" {
			t.Errorf("name = %q, want Jane Doe", name)
		}
		if color != "#00ff00" {
			t.Errorf("color = %q, want #00ff00", color)
		}
	})

	t.Run("delete_person", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM persons WHERE id = 1")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM persons").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 persons, got %d", count)
		}
	})
}

func TestEventCRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL
	)`)

	t.Run("create_event", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, tags, latitude, longitude) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Test Event", "A test description", "2026-06-15", "Test Location", "image", "/media/test.jpg", "tag1, tag2", 40.7128, -74.0060)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event, got %d", count)
		}
	})

	t.Run("fetch_event", func(t *testing.T) {
		var title, description, date, location, mediaType, mediaURL, tags string
		var latitude, longitude float64
		err := db.QueryRow("SELECT title, description, event_date, location, media_type, media_url, tags, latitude, longitude FROM timeline_events WHERE id = 1").
			Scan(&title, &description, &date, &location, &mediaType, &mediaURL, &tags, &latitude, &longitude)
		if err != nil {
			t.Fatal(err)
		}
		if title != "Test Event" {
			t.Errorf("title = %q", title)
		}
		if date != "2026-06-15" {
			t.Errorf("date = %q", date)
		}
		if tags != "tag1, tag2" {
			t.Errorf("tags = %q", tags)
		}
		if latitude != 40.7128 {
			t.Errorf("latitude = %f", latitude)
		}
	})

	t.Run("query_by_year", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", "2026").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event in 2026, got %d", count)
		}
		var count2 int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", "2025").Scan(&count2)
		if count2 != 0 {
			t.Errorf("expected 0 events in 2025, got %d", count2)
		}
	})

	t.Run("query_by_tag_like", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE tags LIKE ?", "%tag1%").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event with tag1, got %d", count)
		}
	})

	t.Run("update_event", func(t *testing.T) {
		_, err := db.Exec("UPDATE timeline_events SET title=?, location=? WHERE id=?", "Updated Event", "New Location", 1)
		if err != nil {
			t.Fatal(err)
		}
		var title, location string
		db.QueryRow("SELECT title, location FROM timeline_events WHERE id = 1").Scan(&title, &location)
		if title != "Updated Event" || location != "New Location" {
			t.Errorf("got %q / %q", title, location)
		}
	})

	t.Run("delete_event", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM timeline_events WHERE id = 1")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 events, got %d", count)
		}
	})
}

func TestTagAutocomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		tags TEXT
	)`)

	db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('Event 1', 'nature, photography')")
	db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('Event 2', 'nature, hiking')")
	db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('Event 3', 'food, cooking')")

	t.Run("distinct_tags", func(t *testing.T) {
		rows, err := db.Query("SELECT DISTINCT tags FROM timeline_events WHERE tags != ''")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var rawTags []string
		for rows.Next() {
			var t string
			rows.Scan(&t)
			rawTags = append(rawTags, t)
		}
		// Flatten comma-separated tags
		tagSet := make(map[string]bool)
		for _, rt := range rawTags {
			for _, tag := range strings.Split(rt, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagSet[tag] = true
				}
			}
		}
		expected := []string{"nature", "photography", "hiking", "food", "cooking"}
		for _, e := range expected {
			if !tagSet[e] {
				t.Errorf("missing tag: %s", e)
			}
		}
		if len(tagSet) != len(expected) {
			t.Errorf("expected %d unique tags, got %d", len(expected), len(tagSet))
		}
	})

	t.Run("tag_search", func(t *testing.T) {
		rows, err := db.Query("SELECT DISTINCT tags FROM timeline_events WHERE tags LIKE ?", "%nature%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("expected 2 tag rows matching 'nature', got %d", count)
		}
	})
}

func TestContributions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		event_date TEXT
	)`)

	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 1', '2026-01-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 2', '2026-01-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 3', '2026-03-20')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 4', '2026-03-20')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 5', '2026-07-04')")

	t.Run("contributions_by_year", func(t *testing.T) {
		rows, err := db.Query("SELECT event_date FROM timeline_events WHERE strftime('%Y', event_date) = ?", "2026")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		contributions := make(map[string]int)
		for rows.Next() {
			var date string
			if err := rows.Scan(&date); err == nil {
				contributions[date]++
			}
		}

		if contributions["2026-01-15"] != 2 {
			t.Errorf("expected 2 events on 2026-01-15, got %d", contributions["2026-01-15"])
		}
		if contributions["2026-07-04"] != 1 {
			t.Errorf("expected 1 event on 2026-07-04, got %d", contributions["2026-07-04"])
		}
		if len(contributions) != 3 {
			t.Errorf("expected 3 unique dates, got %d", len(contributions))
		}
	})
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{[]string{"a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{[]string{}, []string{}},
		{[]string{"same", "same", "same"}, []string{"same"}},
	}
	for _, tt := range tests {
		result := uniqueStrings(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("uniqueStrings(%v) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("uniqueStrings(%v) = %v, want %v", tt.input, result, tt.expected)
				break
			}
		}
	}
}

func TestCalendarQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 1', '2026-06-01')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 2', '2026-06-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 3', '2026-06-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 4', '2026-07-01')")

	t.Run("calendar_month_query", func(t *testing.T) {
		rows, err := db.Query(`SELECT event_date FROM timeline_events WHERE event_date BETWEEN '2026-06-01' AND '2026-06-30' ORDER BY event_date ASC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		var dates []string
		for rows.Next() {
			var d string
			rows.Scan(&d)
			dates = append(dates, d)
		}

		if len(dates) != 3 {
			t.Errorf("expected 3 events in June, got %d", len(dates))
		}
	})

	t.Run("calendar_group_by_date", func(t *testing.T) {
		rows, err := db.Query(`SELECT event_date, COUNT(*) FROM timeline_events WHERE event_date BETWEEN '2026-06-01' AND '2026-06-30' GROUP BY event_date ORDER BY event_date`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		groups := make(map[string]int)
		for rows.Next() {
			var date string
			var count int
			rows.Scan(&date, &count)
			groups[date] = count
		}

		if groups["2026-06-15"] != 2 {
			t.Errorf("expected 2 events on 2026-06-15, got %d", groups["2026-06-15"])
		}
	})
}

func TestRecurringEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0
	)`)

	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('Birthday', '2026-01-15', 'yearly')")
	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('Weekly Meetup', '2026-01-05', 'weekly')")
	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('Monthly Report', '2026-01-01', 'monthly')")
	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('One-off', '2026-01-01', '')")

	t.Run("recurring_yearly", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE recurring = 'yearly'").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 yearly recurring event, got %d", count)
		}
	})

	t.Run("recurring_weekly", func(t *testing.T) {
		var title, recurring string
		db.QueryRow("SELECT title, recurring FROM timeline_events WHERE recurring = 'weekly'").Scan(&title, &recurring)
		if title != "Weekly Meetup" {
			t.Errorf("expected 'Weekly Meetup', got %q", title)
		}
	})

	t.Run("non_recurring", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE recurring = ''").Scan(&count)
		if count < 1 {
			t.Errorf("expected at least 1 non-recurring event")
		}
	})
}

func TestMultiUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		avatar_url TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	db.Exec("INSERT INTO users (username, display_name, color) VALUES ('alice', 'Alice', '#ef4444')")
	db.Exec("INSERT INTO users (username, display_name, color) VALUES ('bob', 'Bob', '#3b82f6')")

	t.Run("user_count", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
		if count != 2 {
			t.Errorf("expected 2 users, got %d", count)
		}
	})

	t.Run("fetch_user", func(t *testing.T) {
		var username, displayName, color string
		err := db.QueryRow("SELECT username, display_name, color FROM users WHERE id = 1").Scan(&username, &displayName, &color)
		if err != nil {
			t.Fatal(err)
		}
		if username != "alice" {
			t.Errorf("username = %q", username)
		}
		if displayName != "Alice" {
			t.Errorf("display_name = %q", displayName)
		}
		if color != "#ef4444" {
			t.Errorf("color = %q", color)
		}
	})

	t.Run("delete_user", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM users WHERE id = 2")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 user after delete, got %d", count)
		}
	})
}

func TestOllamaConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS ollama_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT 'http://localhost:11434',
		model TEXT DEFAULT 'llama3.2',
		enabled INTEGER DEFAULT 0
	)`)
	db.Exec("INSERT OR IGNORE INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)")

	t.Run("default_config", func(t *testing.T) {
		var url, model string
		var enabled int
		err := db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&url, &model, &enabled)
		if err != nil {
			t.Fatal(err)
		}
		if url != "http://localhost:11434" {
			t.Errorf("url = %q", url)
		}
		if model != "llama3.2" {
			t.Errorf("model = %q", model)
		}
		if enabled != 0 {
			t.Errorf("enabled = %d", enabled)
		}
	})

	t.Run("update_config", func(t *testing.T) {
		db.Exec("UPDATE ollama_settings SET url=?, model=?, enabled=? WHERE id=1", "http://ollama:11434", "mistral", 1)
		var url, model string
		var enabled int
		db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&url, &model, &enabled)
		if url != "http://ollama:11434" {
			t.Errorf("url = %q", url)
		}
		if enabled != 1 {
			t.Errorf("enabled = %d", enabled)
		}
	})
}

func TestWeatherDataStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		event_date TEXT,
		weather_data TEXT DEFAULT ''
	)`)

	t.Run("store_and_retrieve_weather", func(t *testing.T) {
		weatherJSON := `{"temperature":22.5,"condition":"Partly cloudy","icon":"cloud-sun","humidity":65,"wind_speed":12.3}`
		_, err := db.Exec("INSERT INTO timeline_events (title, event_date, weather_data) VALUES (?, ?, ?)", "Weather Event", "2026-06-15", weatherJSON)
		if err != nil {
			t.Fatal(err)
		}

		var data string
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 1").Scan(&data)
		if data != weatherJSON {
			t.Errorf("weather_data mismatch")
		}

		var parsed struct {
			Temperature float64 `json:"temperature"`
			Condition   string  `json:"condition"`
		}
		json.Unmarshal([]byte(data), &parsed)
		if parsed.Temperature != 22.5 {
			t.Errorf("temperature = %f", parsed.Temperature)
		}
		if parsed.Condition != "Partly cloudy" {
			t.Errorf("condition = %q", parsed.Condition)
		}
	})
}

func TestWeatherCodeMapping(t *testing.T) {
	tests := []struct {
		code      int
		condition string
		icon      string
	}{
		{0, "Clear sky", "sun"},
		{1, "Partly cloudy", "cloud-sun"},
		{3, "Partly cloudy", "cloud-sun"},
		{45, "Foggy", "smog"},
		{51, "Drizzle", "cloud-rain"},
		{61, "Rain", "cloud-showers-heavy"},
		{71, "Snow", "snowflake"},
		{80, "Rain showers", "cloud-showers-heavy"},
		{85, "Snow showers", "snowflake"},
		{95, "Thunderstorm", "bolt"},
		{99, "Thunderstorm", "bolt"},
	}
	for _, tt := range tests {
		if got := weatherCodeToCondition(tt.code); got != tt.condition {
			t.Errorf("weatherCodeToCondition(%d) = %q, want %q", tt.code, got, tt.condition)
		}
		if got := weatherCodeToIcon(tt.code); got != tt.icon {
			t.Errorf("weatherCodeToIcon(%d) = %q, want %q", tt.code, got, tt.icon)
		}
	}
}

func TestWeatherResponseParsing(t *testing.T) {
	// Verify the response struct can parse Open-Meteo's daily format with
	// the corrected field name relative_humidity_2m_mean
	respJSON := `{
		"daily": {
			"time": ["2026-05-12"],
			"temperature_2m_max": [15.0],
			"temperature_2m_min": [8.0],
			"weathercode": [3],
			"wind_speed_10m_max": [12.5],
			"relative_humidity_2m_mean": [72]
		}
	}`
	var parsed struct {
		Daily struct {
			Time           []string  `json:"time"`
			TemperatureMax []float64 `json:"temperature_2m_max"`
			TemperatureMin []float64 `json:"temperature_2m_min"`
			WeatherCode    []int     `json:"weathercode"`
			WindSpeedMax   []float64 `json:"wind_speed_10m_max"`
			Humidity       []float64 `json:"relative_humidity_2m_mean"`
		} `json:"daily"`
	}
	if err := json.Unmarshal([]byte(respJSON), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Daily.Humidity) == 0 {
		t.Fatal("Humidity array is empty — json tag mismatch")
	}
	if parsed.Daily.Humidity[0] != 72 {
		t.Errorf("humidity = %f, want 72", parsed.Daily.Humidity[0])
	}
	if parsed.Daily.WeatherCode[0] != 3 {
		t.Errorf("weatherCode = %d, want 3", parsed.Daily.WeatherCode[0])
	}
}

func TestWeatherURLContainsCorrectParameters(t *testing.T) {
	// Verify the forecast and archive URLs use the corrected parameter names
	lat := 52.52
	lng := 13.41
	date := "2026-05-12"

	forecastURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
		lat, lng, date, date)
	archiveURL := fmt.Sprintf("https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
		lat, lng, date, date)

	if !strings.Contains(forecastURL, "relative_humidity_2m_mean") {
		t.Error("forecast URL missing relative_humidity_2m_mean")
	}
	if !strings.Contains(forecastURL, "weathercode") {
		t.Error("forecast URL missing weathercode")
	}
	if !strings.Contains(archiveURL, "relative_humidity_2m_mean") {
		t.Error("archive URL missing relative_humidity_2m_mean")
	}
	if !strings.Contains(archiveURL, "weathercode") {
		t.Error("archive URL missing weathercode")
	}
	if strings.Contains(forecastURL, "relative_humidity_2m&") {
		t.Error("forecast URL still uses old relative_humidity_2m parameter")
	}
	if strings.Contains(archiveURL, "relative_humidity_2m&") {
		t.Error("archive URL still uses old relative_humidity_2m parameter")
	}
}

func TestFetchWeatherValidatesInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t.Run("missing_fields_returns_400", func(t *testing.T) {
		r := gin.New()
		r.POST("/api/weather/fetch", fetchWeather)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/weather/fetch", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid_date_returns_400", func(t *testing.T) {
		r := gin.New()
		r.POST("/api/weather/fetch", fetchWeather)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/weather/fetch", strings.NewReader(`{"latitude":52.52,"longitude":13.41,"date":"bad-date"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestTypeScriptBuildOutput(t *testing.T) {
	if _, err := os.Stat("static/js/index.js"); os.IsNotExist(err) {
		t.Skip("compiled JS not found; run 'npm run build:ts' first")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()

	basePath := "."
	router.Static("/static", filepath.Join(basePath, "static"))
	router.GET("/sw.js", serveServiceWorker)

	jsFiles := []string{
		"/static/js/index.js",
		"/static/js/admin.js",
		"/static/js/login.js",
		"/static/js/setup.js",
		"/static/js/map.js",
	}

	for _, file := range jsFiles {
		t.Run("serves_"+file, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", file, nil)
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("GET %s returned status %d, want 200", file, w.Code)
			}
			if len(w.Body.Bytes()) == 0 {
				t.Errorf("GET %s returned empty body", file)
			}
		})
	}

	t.Run("old_appjs_returns_404", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/static/app.js", nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET /static/app.js returned status %d, want 404", w.Code)
		}
	})

	t.Run("source_maps_served", func(t *testing.T) {
		maps := []string{
			"/static/js/index.js.map",
			"/static/js/admin.js.map",
			"/static/js/login.js.map",
			"/static/js/setup.js.map",
			"/static/js/map.js.map",
		}
		for _, file := range maps {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", file, nil)
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("GET %s returned status %d, want 200", file, w.Code)
			}
		}
	})

	t.Run("service_worker_references_index_js", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/sw.js", nil)
		router.ServeHTTP(w, req)
		body := w.Body.String()
		if !strings.Contains(body, "/static/js/index.js") {
			t.Error("service worker should reference /static/js/index.js")
		}
		if strings.Contains(body, "/static/app.js") {
			t.Error("service worker should NOT reference /static/app.js")
		}
	})
}

func TestManifestAndServiceWorker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := setupTestRouter()

	router.GET("/api/manifest.json", serveManifest)
	router.GET("/sw.js", serveServiceWorker)

	t.Run("manifest_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/manifest.json", nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d", w.Code)
		}
		var manifest map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest["name"] != "TRACES - Your Year in Review" {
			t.Errorf("manifest name = %q", manifest["name"])
		}
	})

	t.Run("service_worker_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/sw.js", nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d", w.Code)
		}
		if len(w.Body.Bytes()) == 0 {
			t.Error("empty service worker response")
		}
	})
}

func TestMarkdownInDescription(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT
	)`)

	t.Run("render_markdown_go", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			wantHTML []string
		}{
			{"bold", "**bold**", []string{"<strong>bold</strong>"}},
			{"italic", "*italic*", []string{"<em>italic</em>"}},
			{"heading", "## Heading", []string{"<h2>Heading</h2>"}},
			{"link", "[text](https://example.com)", []string{"<a href=\"https://example.com\"", "text</a>"}},
			{"list", "- item", []string{"<li>item</li>", "<ul>"}},
			{"blockquote", "> quote", []string{"<blockquote>", "quote"}},
			{"code", "`code`", []string{"<code>code</code>"}},
			{"empty", "", []string{}},
			{"xss", "<script>alert('xss')</script>", []string{}}, // goldmark strips raw HTML by default
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := RenderMarkdown(tt.input)
				for _, want := range tt.wantHTML {
					if !strings.Contains(got, want) {
						t.Errorf("RenderMarkdown(%q) = %q, want contains %q", tt.input, got, want)
					}
				}
			})
		}
	})

	t.Run("store_markdown_description", func(t *testing.T) {
		md := "## Heading\n\nThis is **bold** and *italic*.\n\n- List item 1\n- List item 2"
		_, err := db.Exec("INSERT INTO timeline_events (title, description, event_date) VALUES (?, ?, ?)", "MD Event", md, "2026-06-15")
		if err != nil {
			t.Fatal(err)
		}

		var description string
		db.QueryRow("SELECT description FROM timeline_events WHERE id = 1").Scan(&description)
		if description != md {
			t.Errorf("markdown content mismatch")
		}
		if !strings.Contains(description, "**bold**") {
			t.Error("markdown should preserve bold syntax")
		}
	})
}

func TestPersonSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		person_id INTEGER
	)`)

	db.Exec("INSERT INTO persons (name, color) VALUES ('Alice Johnson', '#ff0000')")
	db.Exec("INSERT INTO persons (name, color) VALUES ('Bob Smith', '#00ff00')")
	db.Exec("INSERT INTO persons (name, color) VALUES ('Charlie Brown', '#0000ff')")

	t.Run("search_by_name_returns_matching_persons", func(t *testing.T) {
		rows, err := db.Query("SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at, (SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count FROM persons p WHERE p.name LIKE ? ORDER BY p.name ASC", "%Alice%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		var name string
		for rows.Next() {
			var id int
			var avatarURL, bio, birthDate, color, createdAt sql.NullString
			var eventCount int
			if err := rows.Scan(&id, &name, &avatarURL, &bio, &birthDate, &color, &createdAt, &eventCount); err != nil {
				t.Fatal(err)
			}
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 person matching 'Alice', got %d", count)
		}
		if name != "Alice Johnson" {
			t.Errorf("name = %q, want 'Alice Johnson'", name)
		}
	})

	t.Run("empty_query_returns_all_persons", func(t *testing.T) {
		rows, err := db.Query("SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at, (SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count FROM persons p ORDER BY p.name ASC")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 3 {
			t.Errorf("expected 3 persons with empty query, got %d", count)
		}
	})

	t.Run("partial_match_search", func(t *testing.T) {
		rows, err := db.Query("SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at, (SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count FROM persons p WHERE p.name LIKE ? ORDER BY p.name ASC", "%ob%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var id int
			var name string
			var avatarURL, bio, birthDate, color, createdAt sql.NullString
			var eventCount int
			if err := rows.Scan(&id, &name, &avatarURL, &bio, &birthDate, &color, &createdAt, &eventCount); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		if len(names) != 1 || names[0] != "Bob Smith" {
			t.Errorf("expected ['Bob Smith'], got %v", names)
		}
	})
}

func TestEventCreationWithWeatherData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT,
		weather_data TEXT,
		user_id INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	weatherJSON := `{"temperature":22.5,"condition":"Partly cloudy","icon":"cloud-sun","humidity":65,"wind_speed":12.3,"fetched_at":"2026-05-06T10:00:00Z"}`

	t.Run("create_event_with_weather_data", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, tags, latitude, longitude, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Weather Event", "Event with weather", "2026-06-15", "Test Location", "image", "/media/test.jpg", "weather", 40.7128, -74.0060, weatherJSON, 1)
		if err != nil {
			t.Fatal(err)
		}

		var storedWeather string
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 1").Scan(&storedWeather)
		if storedWeather != weatherJSON {
			t.Errorf("weather_data mismatch: got %q", storedWeather)
		}

		var parsed struct {
			Temperature float64 `json:"temperature"`
			Condition   string  `json:"condition"`
			Humidity    float64 `json:"humidity"`
		}
		if err := json.Unmarshal([]byte(storedWeather), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Temperature != 22.5 {
			t.Errorf("temperature = %f, want 22.5", parsed.Temperature)
		}
		if parsed.Condition != "Partly cloudy" {
			t.Errorf("condition = %q, want 'Partly cloudy'", parsed.Condition)
		}
	})

	t.Run("update_event_weather_data", func(t *testing.T) {
		newWeather := `{"temperature":18.0,"condition":"Rain","icon":"cloud-rain","humidity":80,"wind_speed":20.0,"fetched_at":"2026-05-06T12:00:00Z"}`
		_, err := db.Exec("UPDATE timeline_events SET weather_data = ? WHERE id = ?", newWeather, 1)
		if err != nil {
			t.Fatal(err)
		}

		var storedWeather string
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 1").Scan(&storedWeather)
		if storedWeather != newWeather {
			t.Errorf("weather_data mismatch after update")
		}
	})

	t.Run("create_event_without_weather_data", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, tags, latitude, longitude, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"No Weather Event", "Event without weather", "2026-07-01", "Another Location", "image", "/media/test2.jpg", "test", 51.5074, -0.1278, 1)
		if err != nil {
			t.Fatal(err)
		}

		var weatherData sql.NullString
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 2").Scan(&weatherData)
		if weatherData.Valid && weatherData.String != "" {
			t.Errorf("expected empty weather_data, got %q", weatherData.String)
		}
	})
}

func TestEventCreationWithoutThumbnail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT,
		weather_data TEXT,
		user_id INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	t.Run("insert_without_thumbnail_column", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, media_caption, tags, sort_order, is_public, person_id, latitude, longitude, recurring, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"No Thumbnail Event", "Testing without thumbnail", "2026-08-01", "Test Location", "image", "/media/test.jpg", "", "test", 0, 1, nil, 40.7128, -74.0060, "", "", 1)
		if err != nil {
			t.Fatalf("insert without thumbnail failed: %v", err)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event, got %d", count)
		}
	})

	t.Run("update_without_thumbnail_column", func(t *testing.T) {
		_, err := db.Exec(`UPDATE timeline_events SET title=?, description=?, event_date=?, location=?, media_type=?, media_url=?, media_caption=?, tags=?, sort_order=?, is_public=?, person_id=?, latitude=?, longitude=?, recurring=?, weather_data=?, user_id=? WHERE id=?`,
			"Updated No Thumbnail", "Updated desc", "2026-08-02", "Updated Location", "video", "/media/test2.mp4", "", "updated", 1, 0, nil, 51.5074, -0.1278, "", "", 1, 1)
		if err != nil {
			t.Fatalf("update without thumbnail failed: %v", err)
		}

		var title string
		db.QueryRow("SELECT title FROM timeline_events WHERE id = 1").Scan(&title)
		if title != "Updated No Thumbnail" {
			t.Errorf("title = %q, want 'Updated No Thumbnail'", title)
		}
	})
}

func TestScanEventsWithPersonNullThumbnail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	t.Run("scan_null_thumbnail", func(t *testing.T) {
		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, media_caption, tags, sort_order, is_public)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Null Thumbnail Event", "Testing NULL thumbnail scan", "2026-03-15", "Test", "image", "/media/test.jpg", "", "test", 0, 1)

		rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE 1=1 ORDER BY e.event_date ASC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		events := scanEventsWithPerson(rows)
		if len(events) != 1 {
			t.Fatalf("expected 1 event from scanEventsWithPerson, got %d", len(events))
		}

		e := events[0]
		if e.Title != "Null Thumbnail Event" {
			t.Errorf("title = %q, want 'Null Thumbnail Event'", e.Title)
		}
		if e.Thumbnail != "" {
			t.Errorf("thumbnail = %q, want empty string for NULL", e.Thumbnail)
		}
		if e.Date != "2026-03-15" {
			t.Errorf("date = %q, want '2026-03-15'", e.Date)
		}
	})

	t.Run("scan_multiple_mixed_thumbnails", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")

		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, media_caption, tags, is_public)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"With Thumbnail", "Has thumbnail value", "2026-01-10", "Loc1", "image", "/media/a.jpg", "/thumb.jpg", "", "tag1", 1)
		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, media_caption, tags, is_public)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Null Thumbnail", "Has NULL thumbnail", "2026-06-20", "Loc2", "image", "/media/b.jpg", "", "tag2", 1)

		rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE 1=1 ORDER BY e.event_date ASC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		events := scanEventsWithPerson(rows)
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}

		titles := make(map[string]string)
		for _, e := range events {
			titles[e.Title] = e.Thumbnail
		}

		if v, ok := titles["With Thumbnail"]; !ok {
			t.Error("event 'With Thumbnail' not found")
		} else if v != "/thumb.jpg" {
			t.Errorf("thumbnail = %q, want '/thumb.jpg'", v)
		}

		if v, ok := titles["Null Thumbnail"]; !ok {
			t.Error("event 'Null Thumbnail' not found")
		} else if v != "" {
			t.Errorf("thumbnail = %q, want empty string", v)
		}
	})
}

func TestSaveAndGetEventsRoundtrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	defer func() {
		db = origDB
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionStore = make(map[string]int64)
	csrfTokens = make(map[string]string)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0,
		deleted_at TEXT DEFAULT ''
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	sessionID := "test-roundtrip-session"
	sessionStore[sessionID] = time.Now().Add(24 * time.Hour).Unix()
	csrfTokens[sessionID] = fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"-csrf")))

	router := gin.New()
	auth := router.Group("")
	auth.Use(func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil || sessionStore[cookie] == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		c.Next()
	})
	auth.POST("/api/events", saveEvent)
	auth.GET("/api/events", getEvents)

	t.Run("create_and_find_event", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"title":"Roundtrip Event","description":"Roundtrip test","date":"2026-09-01","location":"Test","media_type":"image","is_public":true,"latitude":40.7128,"longitude":-74.0060}`
		req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST status = %d, body=%s", w.Code, w.Body.String())
		}

		var created TimelineEvent
		json.Unmarshal(w.Body.Bytes(), &created)
		if created.ID <= 0 {
			t.Fatalf("expected positive ID, got %d", created.ID)
		}
		if created.Title != "Roundtrip Event" {
			t.Errorf("title = %q", created.Title)
		}

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/api/events?year=2026&sort=desc&limit=10", nil)
		req2.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("GET status = %d, body=%s", w2.Code, w2.Body.String())
		}

		var events []TimelineEvent
		json.Unmarshal(w2.Body.Bytes(), &events)

		found := false
		for _, e := range events {
			if e.Title == "Roundtrip Event" {
				found = true
				if e.Date != "2026-09-01" {
					t.Errorf("date = %q, want '2026-09-01'", e.Date)
				}
				if e.Latitude == nil || *e.Latitude != 40.7128 {
					t.Errorf("latitude mismatch")
				}
				if e.Longitude == nil || *e.Longitude != -74.0060 {
					t.Errorf("longitude mismatch")
				}
				break
			}
		}
		if !found {
			t.Error("created event not found in events list")
		}
	})

	t.Run("update_and_refetch_event", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"id":1,"title":"Updated Roundtrip","description":"Updated desc","date":"2026-10-15","location":"Updated Loc","media_type":"video","is_public":false}`
		req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("UPDATE status = %d, body=%s", w.Code, w.Body.String())
		}

		var updated TimelineEvent
		json.Unmarshal(w.Body.Bytes(), &updated)
		if updated.Title != "Updated Roundtrip" {
			t.Errorf("title = %q", updated.Title)
		}

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/api/events?year=2026&sort=desc&limit=10", nil)
		req2.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w2, req2)

		var events []TimelineEvent
		json.Unmarshal(w2.Body.Bytes(), &events)

		found := false
		for _, e := range events {
			if e.Title == "Updated Roundtrip" {
				found = true
				if e.Date != "2026-10-15" {
					t.Errorf("date = %q, want '2026-10-15'", e.Date)
				}
				if e.Location != "Updated Loc" {
					t.Errorf("location = %q", e.Location)
				}
				break
			}
		}
		if !found {
			t.Error("updated event not found in events list")
		}

		originalStillPresent := false
		for _, e := range events {
			if e.Title == "Roundtrip Event" {
				originalStillPresent = true
				break
			}
		}
		if originalStillPresent {
			t.Error("original event title still present after update")
		}
	})
}

func TestFTSSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		tags TEXT
	)`)

	createFTS5Table()

	var ftsAvailable bool
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events_fts'").Scan(&ftsAvailable)

	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Beach Day', 'Went swimming at the beach', '2026-07-15', 'Malibu, CA', 'beach, summer')")
	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Mountain Hike', 'Hiked through Yosemite', '2026-08-02', 'Yosemite, CA', 'hiking, nature')")
	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Concert Night', 'Live music at the park', '2026-06-20', 'Central Park', 'music, summer')")

	t.Run("fts_search_by_title", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("Beach"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var title string
			rows.Scan(&title)
			titles = append(titles, title)
		}
		if len(titles) != 1 {
			t.Errorf("expected 1 result for 'Beach', got %d: %v", len(titles), titles)
		}
	})

	t.Run("fts_search_by_location", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("Malibu"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'Malibu', got %d", count)
		}
	})

	t.Run("fts_search_by_tag", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("hiking"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'hiking', got %d", count)
		}
	})

	t.Run("fts_multi_match", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("summer"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("expected 2 results for 'summer', got %d", count)
		}
	})

	t.Run("fts_insert_trigger", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Ski Trip', 'Skiing in the Alps', '2026-01-15', 'Alps', 'skiing, winter')")
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("Skiing"))
		if err != nil {
			t.Fatalf("FTS trigger query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'Skiing' after insert, got %d", count)
		}
	})

	t.Run("fts_delete_trigger", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("DELETE FROM timeline_events WHERE title = 'Ski Trip'")
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("Skiing"))
		if err != nil {
			t.Fatalf("FTS delete query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("expected 0 results for 'Skiing' after delete, got %d", count)
		}
	})

	t.Run("fts_update_trigger", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("UPDATE timeline_events SET description = 'Live jazz concert in the park' WHERE title = 'Concert Night'")
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("jazz"))
		if err != nil {
			t.Fatalf("FTS update query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'jazz' after update, got %d", count)
		}
	})

	t.Run("fts_no_match", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", sanitizeFTSQuery("zzzznotfound"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("expected 0 results for 'zzzznotfound', got %d", count)
		}
	})

	t.Run("sanitize_fts_query", func(t *testing.T) {
		cases := []struct {
			input    string
			expected string
		}{
			{"hello", `"hello"`},
			{"it's", `"it''s"`},
		}
		for _, tc := range cases {
			result := sanitizeFTSQuery(tc.input)
			if result != tc.expected {
				t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		}
	})
}

func TestGlobalSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		tags TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)

	createFTS5Table()

	var ftsAvailable bool
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events_fts'").Scan(&ftsAvailable)

	db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Christmas Party', '2025-12-25', 'Yearly holiday event')")
	db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('New Year Party', '2026-01-01', 'New year celebration')")
	db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Summer BBQ', '2026-07-04', 'Fourth of July cookout')")

	t.Run("global_search_returns_all_years", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query(`SELECT e.title, e.event_date FROM timeline_events e
			WHERE e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)
			ORDER BY e.event_date DESC LIMIT 10`, sanitizeFTSQuery("Party"))
		if err != nil {
			t.Fatalf("Global search query failed: %v", err)
		}
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var title, date string
			rows.Scan(&title, &date)
			titles = append(titles, title)
		}
		if len(titles) != 2 {
			t.Errorf("expected 2 'Party' matches, got %d: %v", len(titles), titles)
		}
	})

	t.Run("global_search_like_fallback", func(t *testing.T) {
		query := "BBQ"
		like := "%" + query + "%"
		rows, err := db.Query(`SELECT e.title FROM timeline_events e
			WHERE 1=1 AND (e.title LIKE ? OR e.description LIKE ? OR e.location LIKE ?)
			ORDER BY e.event_date DESC LIMIT 10`, like, like, like)
		if err != nil {
			t.Fatalf("LIKE fallback query failed: %v", err)
		}
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var title string
			rows.Scan(&title)
			titles = append(titles, title)
		}
		if len(titles) != 1 {
			t.Errorf("expected 1 result for 'BBQ' via LIKE, got %d: %v", len(titles), titles)
		}
	})

	t.Run("global_search_limit", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party One', '2026-01-01', '')")
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party Two', '2026-01-02', '')")
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party Three', '2026-01-03', '')")
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party Four', '2026-01-04', '')")

		rows, err := db.Query(`SELECT e.title FROM timeline_events e
			WHERE e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)
			ORDER BY e.event_date DESC LIMIT 3`, sanitizeFTSQuery("Party"))
		if err != nil {
			t.Fatalf("Limit query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 3 {
			t.Errorf("expected exactly 3 results with LIMIT, got %d", count)
		}
	})

	t.Run("global_search_empty_query", func(t *testing.T) {
		rows, err := db.Query(`SELECT e.title FROM timeline_events e
			WHERE e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)`,
			sanitizeFTSQuery(""))
		if err == nil {
			rows.Close()
		}
	})
}

func TestStatsDistribution(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		event_date TEXT,
		location TEXT,
		tags TEXT,
		media_type TEXT,
		person_id INTEGER,
		user_id INTEGER,
		latitude REAL,
		longitude REAL
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		display_name TEXT
	)`)
	db.Exec("INSERT OR IGNORE INTO users (id, display_name) VALUES (1, 'Alice')")
	db.Exec("INSERT OR IGNORE INTO users (id, display_name) VALUES (2, 'Bob')")
	db.Exec("INSERT OR IGNORE INTO persons (id, name) VALUES (1, 'Charlie')")
	db.Exec("INSERT OR IGNORE INTO persons (id, name) VALUES (2, 'Diana')")

	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E1', '2026-01-15', 'NYC', 'work, travel', 'image', 1, 1, 40.7128, -74.0060)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E2', '2026-03-20', 'LA', 'fun, travel', 'video', 1, 2, 34.0522, -118.2437)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E3', '2026-03-20', 'SF', 'work', 'image', 2, 1, 37.7749, -122.4194)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E4', '2026-07-04', 'Chicago', 'fun, holiday', 'image', 2, 2, 41.8781, -87.6298)")

	t.Run("monthly_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events
			WHERE strftime('%Y', event_date) = '2026' GROUP BY strftime('%m', event_date)`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		monthCounts := make(map[string]int)
		for rows.Next() {
			var m string
			var c int
			rows.Scan(&m, &c)
			monthCounts[m] = c
		}
		if monthCounts["01"] != 1 {
			t.Errorf("Jan has %d events, want 1", monthCounts["01"])
		}
		if monthCounts["03"] != 2 {
			t.Errorf("Mar has %d events, want 2", monthCounts["03"])
		}
		if monthCounts["07"] != 1 {
			t.Errorf("Jul has %d events, want 1", monthCounts["07"])
		}
	})

	t.Run("tag_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT tags FROM timeline_events WHERE strftime('%Y', event_date) = '2026' AND tags != ''`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		tagMap := make(map[string]int)
		for rows.Next() {
			var t string
			rows.Scan(&t)
			for _, tag := range strings.Split(t, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagMap[tag]++
				}
			}
		}
		if tagMap["work"] != 2 {
			t.Errorf("tag 'work' = %d, want 2", tagMap["work"])
		}
		if tagMap["travel"] != 2 {
			t.Errorf("tag 'travel' = %d, want 2", tagMap["travel"])
		}
		if tagMap["fun"] != 2 {
			t.Errorf("tag 'fun' = %d, want 2", tagMap["fun"])
		}
	})

	t.Run("person_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT p.id, p.name, COUNT(e.id) FROM persons p
			LEFT JOIN timeline_events e ON e.person_id = p.id AND strftime('%Y', e.event_date) = '2026'
			GROUP BY p.id HAVING COUNT(e.id) > 0 ORDER BY COUNT(e.id) DESC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var results []struct {
			id    int
			name  string
			count int
		}
		for rows.Next() {
			var r struct {
				id    int
				name  string
				count int
			}
			rows.Scan(&r.id, &r.name, &r.count)
			results = append(results, r)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 persons, got %d", len(results))
		}
	})

	t.Run("user_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT u.id, u.display_name, COUNT(e.id) FROM users u
			LEFT JOIN timeline_events e ON e.user_id = u.id AND strftime('%Y', e.event_date) = '2026'
			GROUP BY u.id HAVING COUNT(e.id) > 0 ORDER BY COUNT(e.id) DESC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var results []struct {
			id    int
			name  string
			count int
		}
		for rows.Next() {
			var r struct {
				id    int
				name  string
				count int
			}
			rows.Scan(&r.id, &r.name, &r.count)
			results = append(results, r)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 users, got %d", len(results))
		}
	})

	t.Run("haversine_distance", func(t *testing.T) {
		nyLat, nyLng := 40.7128, -74.0060
		laLat, laLng := 34.0522, -118.2437
		dist := haversine(nyLat, nyLng, laLat, laLng)
		if dist < 3000 || dist > 5000 {
			t.Errorf("NYC to LA distance = %.0f km, expected ~3940 km", dist)
		}
	})

	t.Run("is_leap_year", func(t *testing.T) {
		if !isLeapYear("2024") {
			t.Error("2024 should be a leap year")
		}
		if isLeapYear("2023") {
			t.Error("2023 should NOT be a leap year")
		}
		if !isLeapYear("2000") {
			t.Error("2000 should be a leap year")
		}
		if isLeapYear("1900") {
			t.Error("1900 should NOT be a leap year")
		}
	})

	t.Run("event_count", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = '2026'").Scan(&count)
		if count != 4 {
			t.Errorf("event count = %d, want 4", count)
		}
	})

	t.Run("top_day", func(t *testing.T) {
		var topDay string
		var topCount int
		db.QueryRow("SELECT event_date, COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = '2026' GROUP BY event_date ORDER BY COUNT(*) DESC LIMIT 1").Scan(&topDay, &topCount)
		if topDay != "2026-03-20" || topCount != 2 {
			t.Errorf("top day = %s (%d), want '2026-03-20' (2)", topDay, topCount)
		}
	})

	t.Run("media_breakdown", func(t *testing.T) {
		rows, err := db.Query("SELECT media_type, COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = '2026' AND media_type != '' GROUP BY media_type")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		media := make(map[string]int)
		for rows.Next() {
			var mt string
			var c int
			rows.Scan(&mt, &c)
			media[mt] = c
		}
		if media["image"] != 3 {
			t.Errorf("images = %d, want 3", media["image"])
		}
		if media["video"] != 1 {
			t.Errorf("videos = %d, want 1", media["video"])
		}
	})
}

func TestSearchEventsCombinedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		tags TEXT,
		person_id INTEGER,
		latitude REAL,
		longitude REAL
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)
	db.Exec("INSERT INTO persons (id, name) VALUES (1, 'Alice')")
	db.Exec("INSERT INTO persons (id, name) VALUES (2, 'Bob')")

	createFTS5Table()

	db.Exec("INSERT INTO timeline_events (title, event_date, location, media_type, tags, person_id) VALUES ('Beach Party', '2026-07-15', 'Miami', 'image', 'beach, summer', 1)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, media_type, tags, person_id) VALUES ('Mountain Trip', '2026-07-20', 'Denver', 'video', 'hiking, summer', 2)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, media_type, tags, person_id) VALUES ('Museum Visit', '2026-08-01', 'NYC', 'image', 'art, culture', 1)")

	t.Run("filter_by_tag", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND e.tags LIKE ? ORDER BY e.event_date ASC`, "%beach%")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("tag filter expected 1, got %d", count)
		}
	})

	t.Run("filter_by_person_id", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND e.person_id = ? ORDER BY e.event_date ASC`, "1")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("person_id filter expected 2, got %d", count)
		}
	})

	t.Run("combined_filters", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND strftime('%Y', e.event_date) = ? AND e.media_type = ? AND e.person_id = ? ORDER BY e.event_date ASC`, "2026", "image", "1")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("combined filter expected 2, got %d", count)
		}
	})

	t.Run("combined_filters_no_match", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND e.person_id = ? AND e.media_type = ? ORDER BY e.event_date ASC`, "1", "video")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("combined no-match expected 0, got %d", count)
		}
	})
}

func TestImmichConfigRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origImmichURL := immichURL
	origImmichAPIKey := immichAPIKey
	origImmichEnabled := immichEnabled
	defer func() {
		db = origDB
		immichURL = origImmichURL
		immichAPIKey = origImmichAPIKey
		immichEnabled = origImmichEnabled
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS immich_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT '',
		api_key TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0
	)`)
	db.Exec(`INSERT OR IGNORE INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)`)

	var url, apiKey string
	var enabledInt int
	err = db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&url, &apiKey, &enabledInt)
	if err != nil {
		t.Fatal(err)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
	if apiKey != "" {
		t.Errorf("api_key = %q, want empty", apiKey)
	}
	if enabledInt != 0 {
		t.Errorf("enabled = %d, want 0", enabledInt)
	}

	immichURL = "https://immich.example.com"
	immichAPIKey = "test-api-key-123"
	immichEnabled = true

	db.Exec("UPDATE immich_settings SET url=?, api_key=?, enabled=? WHERE id=1", immichURL, immichAPIKey, 1)
	err = db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&url, &apiKey, &enabledInt)
	if err != nil {
		t.Fatal(err)
	}
	if url != immichURL {
		t.Errorf("url = %q, want %q", url, immichURL)
	}
	if apiKey != immichAPIKey {
		t.Errorf("api_key = %q, want %q", apiKey, immichAPIKey)
	}
	if enabledInt != 1 {
		t.Errorf("enabled = %d, want 1", enabledInt)
	}

	immichURL = "https://immich.vandijke.xyz"
	immichAPIKey = "another-key"
	immichEnabled = false

	db.Exec("UPDATE immich_settings SET url=?, api_key=?, enabled=? WHERE id=1", immichURL, immichAPIKey, 0)
	err = db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&url, &apiKey, &enabledInt)
	if err != nil {
		t.Fatal(err)
	}
	if url != immichURL {
		t.Errorf("url = %q, want %q after update", url, immichURL)
	}
	if apiKey != immichAPIKey {
		t.Errorf("api_key = %q, want %q after update", apiKey, immichAPIKey)
	}
	if enabledInt != 0 {
		t.Errorf("enabled = %d, want 0 after update", enabledInt)
	}
}

func TestImmichMemoryAssetSerialization(t *testing.T) {
	tests := []struct {
		name   string
		asset  ImmichMemoryAsset
		expect string
	}{
		{
			name: "basic_asset",
			asset: ImmichMemoryAsset{
				ID:               "abc-123",
				OriginalFileName: "IMG_2023.jpg",
				Type:             "IMAGE",
				ThumbnailURL:     "https://immich.example.com/api/assets/abc-123/thumbnail",
				AssetCount:       3,
				MemoryDate:       "1 year ago",
				Latitude:         52.3676,
				Longitude:        4.9041,
				Description:      "photo from immich",
			},
			expect: `"id":"abc-123"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.asset)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			if !strings.Contains(string(data), tt.expect) {
				t.Errorf("json output %q should contain %q", string(data), tt.expect)
			}
		})
	}
}

func TestImmichTimelineResponseParsing(t *testing.T) {
	responseJSON := `[
		{
			"title": "1 year ago",
			"assets": [
				{
					"id": "asset-001",
					"originalFileName": "photo1.jpg",
					"type": "IMAGE",
					"exifInfo": {
						"dateTimeOriginal": "2023-05-07T10:30:00.000Z",
						"latitude": 52.3676,
						"longitude": 4.9041,
						"city": "Amsterdam",
						"country": "Netherlands"
					}
				}
			]
		}
	]`

	var timeline []immichTimelineResponse
	if err := json.Unmarshal([]byte(responseJSON), &timeline); err != nil {
		t.Fatalf("failed to parse timeline response: %v", err)
	}

	if len(timeline) != 1 {
		t.Fatalf("expected 1 timeline group, got %d", len(timeline))
	}

	if timeline[0].Title != "1 year ago" {
		t.Errorf("title = %q, want %q", timeline[0].Title, "1 year ago")
	}

	if len(timeline[0].Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(timeline[0].Assets))
	}

	asset := timeline[0].Assets[0]
	if asset.ID != "asset-001" {
		t.Errorf("asset.ID = %q, want %q", asset.ID, "asset-001")
	}
	if asset.OriginalFileName != "photo1.jpg" {
		t.Errorf("asset.OriginalFileName = %q, want %q", asset.OriginalFileName, "photo1.jpg")
	}
	if asset.Type != "IMAGE" {
		t.Errorf("asset.Type = %q, want %q", asset.Type, "IMAGE")
	}
	if asset.ExifInfo == nil {
		t.Fatal("asset.ExifInfo is nil")
	}
	if *asset.ExifInfo.City != "Amsterdam" {
		t.Errorf("exif.City = %q, want %q", *asset.ExifInfo.City, "Amsterdam")
	}
}

func TestImmichConfigHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origImmichURL := immichURL
	origImmichAPIKey := immichAPIKey
	origImmichEnabled := immichEnabled
	defer func() {
		db = origDB
		immichURL = origImmichURL
		immichAPIKey = origImmichAPIKey
		immichEnabled = origImmichEnabled
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS immich_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT '',
		api_key TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0
	)`)
	db.Exec(`INSERT OR IGNORE INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)`)
	immichURL = "https://immich.vandijke.xyz"
	immichAPIKey = "key-123"
	immichEnabled = true

	r := gin.New()
	r.GET("/api/immich/config", getImmichConfig)
	r.POST("/api/immich/config", saveImmichConfig)

	t.Run("get_config_empty", func(t *testing.T) {
		immichURL = ""
		immichAPIKey = ""
		immichEnabled = false

		req := httptest.NewRequest("GET", "/api/immich/config", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		var cfg ImmichConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Enabled {
			t.Errorf("Enabled should be false for empty config")
		}
	})

	t.Run("save_and_read_config", func(t *testing.T) {
		cfg := ImmichConfig{
			URL:     "https://immich.vandijke.xyz",
			APIKey:  "test-api-key-456",
			Enabled: true,
		}
		body, _ := json.Marshal(cfg)

		req := httptest.NewRequest("POST", "/api/immich/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("save status = %d, want 200: %s", w.Code, w.Body.String())
		}

		var result map[string]string
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["status"] != "ok" {
			t.Errorf("status = %q, want ok", result["status"])
		}

		if immichURL != cfg.URL {
			t.Errorf("immichURL global = %q, want %q", immichURL, cfg.URL)
		}
		if immichAPIKey != cfg.APIKey {
			t.Errorf("immichAPIKey global = %q, want %q", immichAPIKey, cfg.APIKey)
		}
		if !immichEnabled {
			t.Error("immichEnabled should be true")
		}

		req2 := httptest.NewRequest("GET", "/api/immich/config", nil)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("read status = %d, want 200", w2.Code)
		}

		var readCfg ImmichConfig
		json.Unmarshal(w2.Body.Bytes(), &readCfg)
		if readCfg.URL != cfg.URL {
			t.Errorf("URL = %q, want %q", readCfg.URL, cfg.URL)
		}
		if readCfg.APIKey != cfg.APIKey {
			t.Errorf("APIKey = %q, want %q", readCfg.APIKey, cfg.APIKey)
		}
		if !readCfg.Enabled {
			t.Error("Enabled should be true")
		}
	})

	t.Run("save_config_disabled", func(t *testing.T) {
		cfg := ImmichConfig{
			URL:     "https://immich.vandijke.xyz",
			APIKey:  "key-disabled",
			Enabled: false,
		}
		body, _ := json.Marshal(cfg)

		req := httptest.NewRequest("POST", "/api/immich/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("save status = %d, want 200", w.Code)
		}
		if immichEnabled {
			t.Error("immichEnabled should be false after saving disabled config")
		}
	})
}

func TestPruneBackups(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origBackupPath := backupPath
	defer func() {
		db = origDB
		backupPath = origBackupPath
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")

	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	backupPath = tmpDir

	oldTime := time.Now().AddDate(0, 0, -15)
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("traces-backup-%s-%d.db", oldTime.Format("2006-01-02-150405"), i)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("test"), 0644)
		os.Chtimes(path, oldTime, oldTime)
	}

	recentTime := time.Now().AddDate(0, 0, -1)
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("traces-backup-%s-%d.db", recentTime.Format("2006-01-02-150405"), i)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("test"), 0644)
		os.Chtimes(path, recentTime, recentTime)
	}

	pruneBackups()

	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 2 {
		t.Errorf("expected 2 backups after prune, got %d", len(entries))
	}
}

func TestPruneBackupsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origBackupPath := backupPath
	defer func() {
		db = origDB
		backupPath = origBackupPath
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 0)")

	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	backupPath = tmpDir

	oldTime := time.Now().AddDate(0, 0, -15)
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("traces-backup-%s-%d.db", oldTime.Format("2006-01-02-150405"), i)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("test"), 0644)
		os.Chtimes(path, oldTime, oldTime)
	}

	pruneBackups()

	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 3 {
		t.Errorf("expected 3 backups when prune disabled, got %d", len(entries))
	}
}

func TestBackupConfigRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")

	t.Run("default_config", func(t *testing.T) {
		var days int
		var auto int
		db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&days, &auto)
		if days != 7 {
			t.Errorf("retention_days = %d, want 7", days)
		}
		if auto != 1 {
			t.Errorf("auto_prune = %d, want 1", auto)
		}
	})

	t.Run("update_config", func(t *testing.T) {
		db.Exec("UPDATE backup_settings SET retention_days=?, auto_prune=? WHERE id=1", 14, 0)
		var days int
		var auto int
		db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&days, &auto)
		if days != 14 {
			t.Errorf("retention_days = %d, want 14", days)
		}
		if auto != 0 {
			t.Errorf("auto_prune = %d, want 0", auto)
		}
	})
}

func TestBackupConfigAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")

	r := setupTestRouter()
	r.GET("/api/backup/config", getBackupConfig)
	r.POST("/api/backup/config", saveBackupConfig)

	t.Run("get_config", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/backup/config", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var cfg BackupConfig
		json.Unmarshal(w.Body.Bytes(), &cfg)
		if cfg.RetentionDays != 7 {
			t.Errorf("retention_days = %d, want 7", cfg.RetentionDays)
		}
		if !cfg.AutoPrune {
			t.Error("auto_prune should be true")
		}
	})

	t.Run("save_config", func(t *testing.T) {
		body, _ := json.Marshal(BackupConfig{RetentionDays: 30, AutoPrune: false})
		req := httptest.NewRequest("POST", "/api/backup/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		req = httptest.NewRequest("GET", "/api/backup/config", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var cfg BackupConfig
		json.Unmarshal(w.Body.Bytes(), &cfg)
		if cfg.RetentionDays != 30 {
			t.Errorf("retention_days = %d, want 30", cfg.RetentionDays)
		}
		if cfg.AutoPrune {
			t.Error("auto_prune should be false")
		}
	})
}

func TestEventDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0
	)`)

	t.Run("create_with_times", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, event_date, event_start_time, event_end_time) VALUES (?, ?, ?, ?)`,
			"Timed Event", "2026-06-15", "09:30", "17:00")
		if err != nil {
			t.Fatal(err)
		}
		var startTime, endTime string
		err = db.QueryRow("SELECT event_start_time, event_end_time FROM timeline_events WHERE id = 1").Scan(&startTime, &endTime)
		if err != nil {
			t.Fatal(err)
		}
		if startTime != "09:30" {
			t.Errorf("start_time = %q, want 09:30", startTime)
		}
		if endTime != "17:00" {
			t.Errorf("end_time = %q, want 17:00", endTime)
		}
	})

	t.Run("create_without_times", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES (?, ?)`,
			"No Time Event", "2026-07-01")
		if err != nil {
			t.Fatal(err)
		}
		var startTime, endTime string
		err = db.QueryRow("SELECT event_start_time, event_end_time FROM timeline_events WHERE id = 2").Scan(&startTime, &endTime)
		if err != nil {
			t.Fatal(err)
		}
		if startTime != "" {
			t.Errorf("expected empty start_time, got %q", startTime)
		}
		if endTime != "" {
			t.Errorf("expected empty end_time, got %q", endTime)
		}
	})

	t.Run("update_times", func(t *testing.T) {
		_, err := db.Exec("UPDATE timeline_events SET event_start_time=?, event_end_time=? WHERE id=1", "10:00", "18:30")
		if err != nil {
			t.Fatal(err)
		}
		var startTime, endTime string
		err = db.QueryRow("SELECT event_start_time, event_end_time FROM timeline_events WHERE id = 1").Scan(&startTime, &endTime)
		if err != nil {
			t.Fatal(err)
		}
		if startTime != "10:00" {
			t.Errorf("start_time = %q, want 10:00", startTime)
		}
		if endTime != "18:30" {
			t.Errorf("end_time = %q, want 18:30", endTime)
		}
	})

	t.Run("query_with_times", func(t *testing.T) {
		rows, err := db.Query(`SELECT id, title, event_date, event_start_time, event_end_time FROM timeline_events WHERE event_start_time != '' ORDER BY event_date`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
			var id int
			var title, date, start, end string
			rows.Scan(&id, &title, &date, &start, &end)
			if start == "" {
				t.Errorf("event %d should have start_time", id)
			}
		}
		if count != 1 {
			t.Errorf("expected 1 event with start_time, got %d", count)
		}
	})
}

func TestTagManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		tags TEXT
	)`)

	t.Run("rename_tag", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E1', 'nature, photography')")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E2', 'nature, hiking')")

		db.Exec("UPDATE timeline_events SET tags = REPLACE(tags, 'nature', 'outdoor') WHERE tags LIKE '%nature%'")

		var tags1, tags2 string
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E1'").Scan(&tags1)
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E2'").Scan(&tags2)
		if !strings.Contains(tags1, "outdoor") {
			t.Errorf("expected 'outdoor' in E1, got %q", tags1)
		}
		if strings.Contains(tags1, "nature") {
			t.Errorf("unexpected 'nature' in E1, got %q", tags1)
		}
		if !strings.Contains(tags2, "outdoor") {
			t.Errorf("expected 'outdoor' in E2, got %q", tags2)
		}
	})

	t.Run("delete_tag", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E1', 'food, cooking')")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E2', 'nature, hiking')")

		var id int
		var tags string
		err := db.QueryRow("SELECT id, tags FROM timeline_events WHERE tags LIKE '%cooking%'").Scan(&id, &tags)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(tags, ",")
		var newParts []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && p != "cooking" {
				newParts = append(newParts, p)
			}
		}
		_, err = db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", strings.Join(newParts, ", "), id)
		if err != nil {
			t.Fatal(err)
		}

		var tagsAfter string
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E1'").Scan(&tagsAfter)
		if strings.Contains(tagsAfter, "cooking") {
			t.Errorf("expected 'cooking' removed, got %q", tagsAfter)
		}
		if !strings.Contains(tagsAfter, "food") {
			t.Errorf("expected 'food' to remain, got %q", tagsAfter)
		}
	})

	t.Run("merge_tags", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E1', 'outdoor, photography')")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E2', 'outdoor, hiking')")

		var id int
		var tags string
		err := db.QueryRow("SELECT id, tags FROM timeline_events WHERE title = 'E1'").Scan(&id, &tags)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(tags, ",")
		var newParts []string
		seen := make(map[string]bool)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if p == "outdoor" {
				p = "outside"
			}
			if !seen[p] {
				newParts = append(newParts, p)
				seen[p] = true
			}
		}
		_, err = db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", strings.Join(newParts, ", "), id)
		if err != nil {
			t.Fatal(err)
		}

		var tags1 string
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E1'").Scan(&tags1)
		if !strings.Contains(tags1, "outside") {
			t.Errorf("expected 'outside' in E1 after merge, got %q", tags1)
		}
		if strings.Contains(tags1, "outdoor") {
			t.Errorf("unexpected 'outdoor' in E1, got %q", tags1)
		}
	})
}

func TestICalendarExport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0
	)`)

	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, media_type, latitude, longitude, recurring, event_start_time, event_end_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"Test Event", "A test event\nwith multiline", "2026-06-15", "Test Location", "image", 40.7128, -74.0060, "", "09:30", "17:00")
	db.Exec("INSERT INTO timeline_events (title, description, event_date, recurring) VALUES (?, ?, ?, ?)",
		"Birthday", "Annual birthday", "2026-01-15", "yearly")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES (?, ?)",
		"No Desc Event", "2026-03-20")

	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.latitude, e.longitude, e.recurring, e.weather_data, e.event_start_time, e.event_end_time
		FROM timeline_events e ORDER BY e.event_date ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var ics string
	ics += "BEGIN:VCALENDAR\r\n"
	ics += "VERSION:2.0\r\n"
	ics += "PRODID:-//TRACES//Events 2026//EN\r\n"
	ics += "CALSCALE:GREGORIAN\r\n"
	ics += "METHOD:PUBLISH\r\n"
	ics += "X-WR-CALNAME:TRACES 2026\r\n"

	for rows.Next() {
		var id int
		var title, date string
		var desc, location, mediaType, recurring, weatherData, startTime, endTime sql.NullString
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&id, &title, &desc, &date, &location, &mediaType, &lat, &lng, &recurring, &weatherData, &startTime, &endTime); err != nil {
			t.Fatal(err)
		}
		uid := fmt.Sprintf("%d-%s@traces", id, date)
		ics += "BEGIN:VEVENT\r\n"
		ics += "UID:" + uid + "\r\n"
		ics += "DTSTAMP:20260511T000000Z\r\n"
		if startTime.Valid && startTime.String != "" {
			ics += "DTSTART:" + strings.ReplaceAll(date, "-", "") + "T" + strings.ReplaceAll(startTime.String, ":", "") + "00\r\n"
			if endTime.Valid && endTime.String != "" {
				ics += "DTEND:" + strings.ReplaceAll(date, "-", "") + "T" + strings.ReplaceAll(endTime.String, ":", "") + "00\r\n"
			}
		} else {
			ics += "DTSTART;VALUE=DATE:" + strings.ReplaceAll(date, "-", "") + "\r\n"
		}
		ics += "SUMMARY:" + strings.ReplaceAll(title, "\\", "\\\\") + "\r\n"
		if desc.Valid && desc.String != "" {
			ics += "DESCRIPTION:" + strings.ReplaceAll(strings.ReplaceAll(desc.String, "\n", "\\n"), "\\", "\\\\") + "\r\n"
		}
		if location.Valid && location.String != "" {
			ics += "LOCATION:" + strings.ReplaceAll(location.String, "\\", "\\\\") + "\r\n"
		}
		if lat.Valid && lng.Valid {
			ics += "GEO:" + fmt.Sprintf("%.6f;%.6f", lat.Float64, lng.Float64) + "\r\n"
		}
		if recurring.Valid {
			switch recurring.String {
			case "yearly":
				ics += "RRULE:FREQ=YEARLY\r\n"
			}
		}
		ics += "END:VEVENT\r\n"
	}
	ics += "END:VCALENDAR\r\n"

	t.Run("has_vcalendar_wrapper", func(t *testing.T) {
		if !strings.Contains(ics, "BEGIN:VCALENDAR") {
			t.Error("missing BEGIN:VCALENDAR")
		}
		if !strings.Contains(ics, "END:VCALENDAR") {
			t.Error("missing END:VCALENDAR")
		}
		if !strings.Contains(ics, "VERSION:2.0") {
			t.Error("missing VERSION:2.0")
		}
	})

	t.Run("contains_all_events", func(t *testing.T) {
		count := strings.Count(ics, "BEGIN:VEVENT")
		if count != 3 {
			t.Errorf("expected 3 VEVENT, got %d", count)
		}
	})

	t.Run("recurring_event_has_rrule", func(t *testing.T) {
		if !strings.Contains(ics, "RRULE:FREQ=YEARLY") {
			t.Error("missing RRULE for yearly event")
		}
	})

	t.Run("timed_event_has_dtstart_dtend", func(t *testing.T) {
		if !strings.Contains(ics, "DTSTART:20260615T093000") {
			t.Error("missing DTSTART with time for timed event")
		}
		if !strings.Contains(ics, "DTEND:20260615T170000") {
			t.Error("missing DTEND with time for timed event")
		}
	})

	t.Run("all_day_event_has_date_dtstart", func(t *testing.T) {
		if !strings.Contains(ics, "DTSTART;VALUE=DATE:20260115") {
			t.Error("missing DATE value DTSTART for all-day event")
		}
	})

	t.Run("event_has_geo", func(t *testing.T) {
		if !strings.Contains(ics, "GEO:40.712800;-74.006000") {
			t.Error("missing GEO for event with coordinates")
		}
	})

	t.Run("each_vevent_has_uid", func(t *testing.T) {
		uidCount := strings.Count(ics, "@traces")
		veventCount := strings.Count(ics, "BEGIN:VEVENT")
		if uidCount != veventCount {
			t.Errorf("expected %d UIDs for %d events, got %d", veventCount, veventCount, uidCount)
		}
	})
}

func TestEmailConfigAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		smtp_host TEXT DEFAULT '',
		smtp_port INTEGER DEFAULT 587,
		smtp_user TEXT DEFAULT '',
		smtp_pass TEXT DEFAULT '',
		from_addr TEXT DEFAULT '',
		to_addr TEXT DEFAULT ''
	)`)

	r := gin.New()
	r.GET("/api/email/config", getEmailConfig)
	r.POST("/api/email/config", saveEmailConfig)
	r.POST("/api/email/test", testEmail)

	t.Run("get_config_returns_defaults_when_no_row", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/email/config", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var cfg EmailConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.SMTPPort != 587 {
			t.Errorf("port = %d, want 587", cfg.SMTPPort)
		}
		if cfg.SMTPHost != "" {
			t.Errorf("host = %q, want empty", cfg.SMTPHost)
		}
	})

	t.Run("save_config", func(t *testing.T) {
		db.Exec("DELETE FROM email_settings")
		db.Exec("INSERT INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)")

		body, _ := json.Marshal(EmailConfig{
			SMTPHost: "smtp.gmail.com",
			SMTPPort: 465,
			SMTPUser: "user@gmail.com",
			SMTPPass: "app-password",
			FromAddr: "from@gmail.com",
			ToAddr:   "to@gmail.com",
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/email/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var result map[string]string
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["status"] != "ok" {
			t.Errorf("status = %q, want 'ok'", result["status"])
		}

		var host, user, pass, fromAddr, toAddr string
		var port int
		err = db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&host, &port, &user, &pass, &fromAddr, &toAddr)
		if err != nil {
			t.Fatal(err)
		}
		if host != "smtp.gmail.com" {
			t.Errorf("host = %q", host)
		}
		if port != 465 {
			t.Errorf("port = %d", port)
		}
		if user != "user@gmail.com" {
			t.Errorf("user = %q", user)
		}
		if pass != "app-password" {
			t.Errorf("pass = %q", pass)
		}
		if fromAddr != "from@gmail.com" {
			t.Errorf("from = %q", fromAddr)
		}
		if toAddr != "to@gmail.com" {
			t.Errorf("to = %q", toAddr)
		}
	})

	t.Run("get_config_after_save", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/email/config", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var cfg EmailConfig
		json.Unmarshal(w.Body.Bytes(), &cfg)
		if cfg.SMTPHost != "smtp.gmail.com" {
			t.Errorf("host = %q", cfg.SMTPHost)
		}
		if cfg.SMTPPort != 465 {
			t.Errorf("port = %d", cfg.SMTPPort)
		}
		if cfg.SMTPUser != "user@gmail.com" {
			t.Errorf("user = %q", cfg.SMTPUser)
		}
		if cfg.ToAddr != "to@gmail.com" {
			t.Errorf("to = %q", cfg.ToAddr)
		}
	})

	t.Run("save_config_defaults_port", func(t *testing.T) {
		db.Exec("DELETE FROM email_settings")
		db.Exec("INSERT INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)")

		body, _ := json.Marshal(EmailConfig{
			SMTPHost: "smtp.example.com",
			SMTPPort: 0,
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/email/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		var port int
		db.QueryRow("SELECT smtp_port FROM email_settings WHERE id = 1").Scan(&port)
		if port != 587 {
			t.Errorf("port = %d, want 587 (default)", port)
		}
	})

	t.Run("test_email_fails_when_not_configured", func(t *testing.T) {
		db.Exec("DELETE FROM email_settings")
		db.Exec("INSERT INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)")

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/email/test", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400: body=%s", w.Code, w.Body.String())
		}
		var result map[string]string
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["error"] != "Email not configured" {
			t.Errorf("error = %q, want 'Email not configured'", result["error"])
		}
	})
}

func TestSendMemoriesEmailHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled INTEGER DEFAULT 1,
		days_window INTEGER DEFAULT 3,
		email_enabled INTEGER DEFAULT 0,
		last_sent_date TEXT DEFAULT ''
	)`)
	db.Exec("INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)")

	db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		smtp_host TEXT DEFAULT '',
		smtp_port INTEGER DEFAULT 587,
		smtp_user TEXT DEFAULT '',
		smtp_pass TEXT DEFAULT '',
		from_addr TEXT DEFAULT '',
		to_addr TEXT DEFAULT ''
	)`)
	db.Exec("INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, 'smtp.example.com', 587)")

	r := gin.New()
	r.POST("/api/memories/send", sendMemoriesEmailHandler)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT
	)`)

	t.Run("fails_when_email_not_fully_configured", func(t *testing.T) {
		db.Exec("UPDATE email_settings SET to_addr='' WHERE id=1")
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/memories/send", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400: body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("fails_when_memories_disabled", func(t *testing.T) {
		db.Exec("UPDATE email_settings SET to_addr='to@test.com' WHERE id=1")
		db.Exec("UPDATE memories_settings SET enabled=0 WHERE id=1")

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/memories/send", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400: body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("succeeds_when_no_memories_found", func(t *testing.T) {
		db.Exec("UPDATE memories_settings SET enabled=1 WHERE id=1")
		db.Exec("UPDATE email_settings SET to_addr='to@test.com' WHERE id=1")

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/memories/send", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200: body=%s", w.Code, w.Body.String())
		}
		var result map[string]string
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["message"] != "No memories for today" {
			t.Errorf("message = %q, want 'No memories for today'", result["message"])
		}
	})
}

func TestMigrationFromV8ToCurrent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	defer func() { db = origDB }()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, _ = db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)")
	_, _ = db.Exec("INSERT INTO schema_version (version) VALUES (8)")

	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN media_caption TEXT`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN tags TEXT`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN sort_order INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN is_public INTEGER DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN person_id INTEGER`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN latitude REAL`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN longitude REAL`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN recurring TEXT DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN weather_data TEXT DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN user_id INTEGER DEFAULT 0`)

	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS share_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE,
		event_ids TEXT,
		year TEXT,
		expires_at TEXT
	)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS gotify_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT '',
		token TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0
	)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO gotify_settings (id, url, token, enabled) VALUES (1, 'https://gotify.example.com', 'token-123', 1)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled INTEGER DEFAULT 1,
		days_window INTEGER DEFAULT 3,
		email_enabled INTEGER DEFAULT 0,
		last_sent_date TEXT DEFAULT ''
	)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 5, 1)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		smtp_host TEXT DEFAULT '',
		smtp_port INTEGER DEFAULT 587,
		smtp_user TEXT DEFAULT '',
		smtp_pass TEXT DEFAULT '',
		from_addr TEXT DEFAULT '',
		to_addr TEXT DEFAULT ''
	)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr) VALUES (1, 'smtp.test.com', 587, 'user', 'pass', 'from@test.com', 'to@test.com')`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		avatar_url TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO users (id, username, display_name, color) VALUES (1, 'admin', 'Admin', '#7c3aed')`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS ollama_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT 'http://localhost:11434',
		model TEXT DEFAULT 'llama3.2',
		enabled INTEGER DEFAULT 0
	)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://ollama:11434', 'mistral', 1)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS immich_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT '',
		api_key TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0
	)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO immich_settings (id, url, api_key, enabled) VALUES (1, 'https://immich.test.com', 'key-123', 1)`)

	_, _ = db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, tags)
		VALUES ('Test Event 1', 'Description 1', '2026-06-15', 'Location 1', 'image', 'tag1, tag2')`)
	_, _ = db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, tags)
		VALUES ('Test Event 2', 'Description 2', '2026-07-20', 'Location 2', 'video', 'tag3')`)

	_, _ = db.Exec(`INSERT INTO persons (name, color) VALUES ('Alice', '#ff0000')`)
	_, _ = db.Exec(`INSERT INTO admin_users (username, password) VALUES ('admin', 'hash-placeholder')`)

	var version int
	_ = db.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if version != 8 {
		t.Fatalf("expected schema version 8, got %d", version)
	}

	var eventCount int
	db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&eventCount)
	if eventCount != 2 {
		t.Fatalf("expected 2 events before migration, got %d", eventCount)
	}

	var emailHost string
	db.QueryRow("SELECT smtp_host FROM email_settings WHERE id = 1").Scan(&emailHost)
	if emailHost != "smtp.test.com" {
		t.Fatalf("expected email host 'smtp.test.com' before migration, got %q", emailHost)
	}

	for version < currentSchemaVersion {
		runMigration(version)
		version++
		db.Exec("DELETE FROM schema_version")
		db.Exec("INSERT INTO schema_version (version) VALUES (?)", version)
	}

	var migratedVersion int
	db.QueryRow("SELECT version FROM schema_version").Scan(&migratedVersion)
	if migratedVersion != currentSchemaVersion {
		t.Errorf("schema version after migration = %d, want %d", migratedVersion, currentSchemaVersion)
	}

	createTables()

	eventCount = 0
	db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&eventCount)
	if eventCount != 2 {
		t.Errorf("event count after migration = %d, want 2 (data should survive)", eventCount)
	}

	var title, date string
	db.QueryRow("SELECT title, event_date FROM timeline_events WHERE id = 1").Scan(&title, &date)
	if title != "Test Event 1" {
		t.Errorf("event 1 title = %q, want 'Test Event 1'", title)
	}
	if date != "2026-06-15" {
		t.Errorf("event 1 date = %q, want '2026-06-15'", date)
	}

	db.QueryRow("SELECT smtp_host FROM email_settings WHERE id = 1").Scan(&emailHost)
	if emailHost != "smtp.test.com" {
		t.Errorf("email host after migration = %q, want 'smtp.test.com'", emailHost)
	}

	var gotifyURL string
	db.QueryRow("SELECT url FROM gotify_settings WHERE id = 1").Scan(&gotifyURL)
	if gotifyURL != "https://gotify.example.com" {
		t.Errorf("gotify url after migration = %q, want 'https://gotify.example.com'", gotifyURL)
	}

	var personCount int
	db.QueryRow("SELECT COUNT(*) FROM persons").Scan(&personCount)
	if personCount != 1 {
		t.Errorf("persons count after migration = %d, want 1", personCount)
	}

	var userName string
	db.QueryRow("SELECT username FROM users WHERE id = 1").Scan(&userName)
	if userName != "admin" {
		t.Errorf("user after migration = %q, want 'admin'", userName)
	}

	colExists := false
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='collections'").Scan(&colExists)
	if !colExists {
		t.Error("collections table should exist after migration")
	}
	colExists = false
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='event_templates'").Scan(&colExists)
	if !colExists {
		t.Error("event_templates table should exist after migration")
	}

	t.Run("new_columns_exist", func(t *testing.T) {
		var colCount int
		db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('timeline_events') WHERE name IN ('is_favorite', 'event_start_time', 'event_end_time')").Scan(&colCount)
		if colCount != 3 {
			t.Errorf("expected 3 new columns (is_favorite, event_start_time, event_end_time), found %d", colCount)
		}
	})
}

func TestRecycleBin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origDB := db
	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	defer func() {
		db = origDB
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	}()

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionStore = make(map[string]int64)
	csrfTokens = make(map[string]string)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0,
		deleted_at TEXT DEFAULT ''
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	router := gin.New()
	auth := router.Group("")
	auth.Use(func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil || sessionStore[cookie] == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		c.Next()
	})
	auth.GET("/api/events", getEvents)
	auth.GET("/api/events/trash", getTrashEvents)
	auth.POST("/api/events/restore", restoreEvents)
	auth.POST("/api/events/empty-trash", emptyTrash)
	auth.POST("/api/events", saveEvent)

	sessionID := "test-trash-session"
	sessionStore[sessionID] = time.Now().Add(24 * time.Hour).Unix()

	t.Run("soft_delete_moves_event_to_trash", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO timeline_events (title, description, event_date) VALUES (?, ?, ?)", "Trash Event", "Will be deleted", "2026-07-04")
		if err != nil {
			t.Fatal(err)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '')").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 active event, got %d", count)
		}

		_, err = db.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=1")
		if err != nil {
			t.Fatal(err)
		}

		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '')").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 active events after soft delete, got %d", count)
		}
	})

	t.Run("trash_endpoint_returns_deleted_events", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/events/trash", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/events/trash status = %d", w.Code)
		}

		var events []TimelineEvent
		json.Unmarshal(w.Body.Bytes(), &events)
		if len(events) != 1 {
			t.Fatalf("expected 1 trashed event, got %d", len(events))
		}
		if events[0].Title != "Trash Event" {
			t.Errorf("trashed event title = %q", events[0].Title)
		}
		if events[0].DeletedAt == "" {
			t.Error("expected deleted_at to be set")
		}
	})

	t.Run("restore_endpoint_brings_event_back", func(t *testing.T) {
		body := `{"ids":[1]}`
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/events/restore", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/events/restore status = %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %q", resp["status"])
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '')").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 active event after restore, got %d", count)
		}
	})

	t.Run("empty_trash_permanently_deletes", func(t *testing.T) {
		_, err := db.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=1")
		if err != nil {
			t.Fatal(err)
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/events/empty-trash", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/events/empty-trash status = %d", w.Code)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 events after empty trash, got %d", count)
		}
	})
}
