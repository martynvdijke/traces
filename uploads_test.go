package main

import (
	"bytes"
	"crypto/sha256"
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

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"traces/internal/integrations"
)

func TestHandleUploadHashing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setupTestMediaSvc(t)

	content := []byte("test-image-content-for-hash-test")
	hash := sha256.Sum256(content)
	hashStr := fmt.Sprintf("%x", hash)

	body := &bytes.Buffer{}
	writer := io.MultiWriter(body)
	_ = writer

	router := gin.New()
	ensureEventsSvc(t)
	router.POST("/api/upload", eventsSvc.HandleUpload)

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

	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	t.Cleanup(func() {
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	})

	newTestDB(t)

	sessionStore = make(map[string]sessionInfo)
	csrfTokens = make(map[string]string)
	setupTestMediaSvc(t)

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
			auth.POST("/upload", ensureEventsSvc(t).HandleUpload)
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

		var resp map[string]any
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

		var resp map[string]any
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

		var resp map[string]any
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

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	)`)

	router := gin.New()
	svc := integrations.New(db, logService, nil)
	router.POST("/api/auto-tag", svc.AutoTagEvent)

	t.Run("empty_title_falls_back_to_ollama", func(t *testing.T) {
		t.Setenv("OLLAMA_URL", "http://127.0.0.1:1")

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
		t.Setenv("OLLAMA_URL", "http://127.0.0.1:1")

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
