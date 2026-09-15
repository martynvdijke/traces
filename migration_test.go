package main

import (
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/database"
	"traces/internal/models"
)

func TestMigrationFromV8ToCurrent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

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

	for version < models.CurrentSchemaVersion {
		database.RunMigration(db, version)
		version++
		db.Exec("DELETE FROM schema_version")
		db.Exec("INSERT INTO schema_version (version) VALUES (?)", version)
	}

	var migratedVersion int
	db.QueryRow("SELECT version FROM schema_version").Scan(&migratedVersion)
	if migratedVersion != models.CurrentSchemaVersion {
		t.Errorf("schema version after migration = %d, want %d", migratedVersion, models.CurrentSchemaVersion)
	}

	database.CreateTables(db)

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
