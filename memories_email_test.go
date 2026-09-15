package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/models"
)

func TestMemoriesQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

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

	// Use UTC to match SQLite's strftime('now') (UTC); local time can be a
	// different calendar day, which made this assertion flaky near midnight.
	now := time.Now().UTC()
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Last year event', ?)`, now.AddDate(-1, 0, 0).Format("2006-01-02"))
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Two years ago', ?)`, now.AddDate(-2, 0, 0).Format("2006-01-02"))
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Old event out of range', ?)`, now.AddDate(-1, -1, 0).Format("2006-01-02"))
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Recent event same year', ?)`, now.Format("2006-01-02"))

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

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled INTEGER DEFAULT 1,
		days_window INTEGER DEFAULT 3,
		email_enabled INTEGER DEFAULT 0,
		last_sent_date TEXT DEFAULT ''
	)`)
	db.Exec(`INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)`)

	var enabledInt, daysWindow, emailInt int
	var err error
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

	newTestDB(t)

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
	err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&host, &port, &user, &pass, &fromAddr, &toAddr)
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

func TestEmailConfigAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

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
		var cfg models.EmailConfig
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

		body, _ := json.Marshal(models.EmailConfig{
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
		err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&host, &port, &user, &pass, &fromAddr, &toAddr)
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
		var cfg models.EmailConfig
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

		body, _ := json.Marshal(models.EmailConfig{
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

	newTestDB(t)

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
		event_date TEXT,
		deleted_at TEXT DEFAULT '',
		source TEXT DEFAULT '',
		source_ref TEXT DEFAULT ''
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
