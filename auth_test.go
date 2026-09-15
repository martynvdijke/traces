package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"

	"traces/internal/models"
)

func TestHandleLogin(t *testing.T) {
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
		sessionStore = make(map[string]sessionInfo)
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
		sessionStore = make(map[string]sessionInfo)
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
		t.Cleanup(func() { db = origDB2 })

		sessionStore = make(map[string]sessionInfo)
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
		sessionStore = make(map[string]sessionInfo)
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
		t.Cleanup(func() { db = origDB3 })

		sessionStore = make(map[string]sessionInfo)
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
		if resp["version"] != models.CurrentVersion {
			t.Errorf("version = %q, want %q", resp["version"], models.CurrentVersion)
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
		if resp["version"] != models.CurrentVersion {
			t.Errorf("version = %q, want %q", resp["version"], models.CurrentVersion)
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

func TestMultiUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

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
