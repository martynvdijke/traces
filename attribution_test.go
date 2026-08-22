package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// setupAttributionTest prepares an isolated DB with the full timeline_events
// schema used by saveEvent.
func setupAttributionTest(t *testing.T) *sql.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origDB := db
	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	t.Cleanup(func() {
		db = origDB
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	})

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	sessionStore = make(map[string]sessionInfo)
	csrfTokens = make(map[string]string)

	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		email TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		avatar_url TEXT DEFAULT '',
		password_hash TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT DEFAULT '',
		tags TEXT DEFAULT '',
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0,
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		deleted_at TEXT DEFAULT ''
	)`)

	return db
}

func TestSaveEventAttribution(t *testing.T) {
	database := setupAttributionTest(t)

	res, err := database.Exec("INSERT INTO users (username, display_name, color) VALUES ('alice', 'Alice', '#ef4444')")
	if err != nil {
		t.Fatal(err)
	}
	aliceID, _ := res.LastInsertId()

	router := gin.New()
	router.Use(authMiddlewareGin())
	router.POST("/api/events", saveEvent)

	familyCookie := "family-cookie"
	sessionStore[familyCookie] = sessionInfo{userID: aliceID, expiresAt: time.Now().Add(time.Hour).Unix()}
	adminCookie := "admin-cookie"
	sessionStore[adminCookie] = sessionInfo{userID: 0, expiresAt: time.Now().Add(time.Hour).Unix()}

	t.Run("family_member_event_stamped", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/events", `{"title":"Beach day","date":"2026-07-01"}`, "session="+familyCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var saved struct {
			ID     int `json:"id"`
			UserID int `json:"user_id"`
		}
		json.Unmarshal(w.Body.Bytes(), &saved)
		if int64(saved.UserID) != aliceID {
			t.Errorf("event user_id = %d, want %d", saved.UserID, aliceID)
		}
		var dbUserID int
		database.QueryRow("SELECT user_id FROM timeline_events WHERE id = ?", saved.ID).Scan(&dbUserID)
		if int64(dbUserID) != aliceID {
			t.Errorf("db user_id = %d, want %d", dbUserID, aliceID)
		}
	})

	t.Run("family_member_cannot_spoof_user_id", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/events", `{"title":"Spoofed","date":"2026-07-02","user_id":99}`, "session="+familyCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var saved struct {
			ID     int `json:"id"`
			UserID int `json:"user_id"`
		}
		json.Unmarshal(w.Body.Bytes(), &saved)
		if int64(saved.UserID) != aliceID {
			t.Errorf("spoofed user_id not overwritten: got %d, want %d", saved.UserID, aliceID)
		}
	})

	t.Run("admin_payload_honored_unchanged", func(t *testing.T) {
		// Admin behaviour is exactly as before: payload user_id passes through.
		w := doJSON(router, "POST", "/api/events", `{"title":"Manual attribution","date":"2026-07-03","user_id":5}`, "session="+adminCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var saved struct {
			ID     int `json:"id"`
			UserID int `json:"user_id"`
		}
		json.Unmarshal(w.Body.Bytes(), &saved)
		if saved.UserID != 5 {
			t.Errorf("admin event user_id = %d, want 5 (unchanged payload behaviour)", saved.UserID)
		}
	})

	t.Run("admin_default_zero", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/events", `{"title":"Admin default","date":"2026-07-04"}`, "session="+adminCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var saved struct {
			UserID int `json:"user_id"`
		}
		json.Unmarshal(w.Body.Bytes(), &saved)
		if saved.UserID != 0 {
			t.Errorf("admin default user_id = %d, want 0", saved.UserID)
		}
	})
}
