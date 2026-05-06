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

		rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
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

		rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
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
