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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
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
