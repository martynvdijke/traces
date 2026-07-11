package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Config holds database configuration paths.
type Config struct {
	DBPath    string
	MediaPath string
}

// Init opens or creates the SQLite database and runs initialization.
func Init(cfg Config) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

// Migrate runs schema initialization and migrations.
func Migrate(db *sql.DB) {
	var schemaVersion int
	err := db.QueryRow("SELECT COALESCE(MAX(version), -1) FROM schema_version").Scan(&schemaVersion)
	if err != nil {
		schemaVersion = -1
	}
	if schemaVersion == -1 {
		_, _ = db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)")
	}

	if schemaVersion < 0 {
		createTables(db)
		CreateFTS5Table(db)
		seedDefaults(db)
		schemaVersion = 0
	}

	for v := schemaVersion; v < 19; v++ {
		RunMigration(db, v)
		_, _ = db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", v+1)
	}

	CreateFTS5Table(db)
}

func createTables(db *sql.DB) {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS timeline_events (
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
		)`,
		`CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			password TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS persons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			avatar_url TEXT DEFAULT '',
			bio TEXT DEFAULT '',
			birth_date TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS share_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE,
			event_ids TEXT,
			year TEXT,
			expires_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS gotify_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			token TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS memories_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER DEFAULT 1,
			days_window INTEGER DEFAULT 3,
			email_enabled INTEGER DEFAULT 0,
			last_sent_date TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS email_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			smtp_host TEXT DEFAULT '',
			smtp_port INTEGER DEFAULT 587,
			smtp_user TEXT DEFAULT '',
			smtp_pass TEXT DEFAULT '',
			from_addr TEXT DEFAULT '',
			to_addr TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			email TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			avatar_url TEXT DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS ollama_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT 'http://localhost:11434',
			model TEXT DEFAULT 'llama3.2',
			enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS immich_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			api_key TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS umami_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			site_id TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS backup_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			retention_days INTEGER DEFAULT 7,
			auto_prune INTEGER DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS otel_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			endpoint TEXT DEFAULT '',
			traces_enabled INTEGER DEFAULT 0,
			metrics_enabled INTEGER DEFAULT 0,
			logs_enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS event_templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			person_id INTEGER DEFAULT 0,
			user_id INTEGER DEFAULT 0,
			location TEXT DEFAULT '',
			media_type TEXT DEFAULT 'image',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS collection_events (
			collection_id INTEGER NOT NULL,
			event_id INTEGER NOT NULL,
			added_at TEXT DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (collection_id, event_id)
		)`,
		`CREATE TABLE IF NOT EXISTS app_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			severity TEXT NOT NULL DEFAULT 'info',
			source TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			metadata TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS log_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			min_severity TEXT NOT NULL DEFAULT 'warn'
		)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			log.Fatalf("[DB] Create table failed: %v", err)
		}
	}
}

func seedDefaults(db *sql.DB) {
	var count int

	db.QueryRow("SELECT COUNT(*) FROM gotify_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO gotify_settings (id, url, token, enabled) VALUES (1, '', '', 0)")
	}

	db.QueryRow("SELECT COUNT(*) FROM memories_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)")
	}

	db.QueryRow("SELECT COUNT(*) FROM email_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)")
	}

	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO users (id, username, display_name, email, color) VALUES (1, 'default', 'Default User', '', '#7c3aed')")
	}

	db.QueryRow("SELECT COUNT(*) FROM ollama_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)")
	}

	db.QueryRow("SELECT COUNT(*) FROM immich_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)")
	}

	db.QueryRow("SELECT COUNT(*) FROM umami_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO umami_settings (id, url, site_id, enabled) VALUES (1, '', '', 0)")
	}

	db.QueryRow("SELECT COUNT(*) FROM backup_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")
	}

	db.QueryRow("SELECT COUNT(*) FROM otel_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO otel_settings (id, endpoint, traces_enabled, metrics_enabled, logs_enabled) VALUES (1, '', 0, 0, 0)")
	}
}

// CreateFTS5Table sets up the FTS5 virtual table for full-text search.
func CreateFTS5Table(db *sql.DB) {
	_, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
		title, description, location, tags,
		content='timeline_events',
		content_rowid='id'
	)`)
	if err != nil {
		log.Printf("[DB] Warning: FTS5 not available, full-text search disabled: %v", err)
		return
	}
	db.Exec(`CREATE TRIGGER IF NOT EXISTS events_fts_ai AFTER INSERT ON timeline_events BEGIN
		INSERT INTO events_fts(rowid, title, description, location, tags)
		VALUES (new.id, new.title, new.description, new.location, COALESCE(new.tags, ''));
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS events_fts_ad AFTER DELETE ON timeline_events BEGIN
		INSERT INTO events_fts(events_fts, rowid, title, description, location, tags)
		VALUES('delete', old.id, old.title, old.description, old.location, COALESCE(old.tags, ''));
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS events_fts_au AFTER UPDATE ON timeline_events BEGIN
		INSERT INTO events_fts(events_fts, rowid, title, description, location, tags)
		VALUES('delete', old.id, old.title, old.description, old.location, COALESCE(old.tags, ''));
		INSERT INTO events_fts(rowid, title, description, location, tags)
		VALUES (new.id, new.title, new.description, new.location, COALESCE(new.tags, ''));
	END`)

	var ftsCount int
	db.QueryRow("SELECT COUNT(*) FROM events_fts").Scan(&ftsCount)
	if ftsCount == 0 {
		db.Exec(`INSERT INTO events_fts(rowid, title, description, location, tags)
			SELECT id, title, description, location, COALESCE(tags, '') FROM timeline_events`)
		log.Printf("[DB] Indexed events for full-text search")
	}
}

// RunMigration applies a single schema migration step.
func RunMigration(db *sql.DB, fromVersion int) {
	switch fromVersion {
	case 0:
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
	case 1:
		runMigrateAddColumns(db, "media_caption", "tags", "sort_order", "is_public")
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS share_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE,
			event_ids TEXT,
			year TEXT,
			expires_at TEXT
		)`)
	case 2:
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
		_, _ = db.Exec(`INSERT OR IGNORE INTO gotify_settings (id, url, token, enabled) VALUES (1, '', '', 0)`)
		runMigrateAddColumns(db, "person_id", "latitude", "longitude")
	case 3:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER DEFAULT 1,
			days_window INTEGER DEFAULT 3,
			email_enabled INTEGER DEFAULT 0,
			last_sent_date TEXT DEFAULT ''
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			smtp_host TEXT DEFAULT '',
			smtp_port INTEGER DEFAULT 587,
			smtp_user TEXT DEFAULT '',
			smtp_pass TEXT DEFAULT '',
			from_addr TEXT DEFAULT '',
			to_addr TEXT DEFAULT ''
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)`)
	case 4:
		for _, col := range []string{"person_id", "latitude", "longitude", "media_caption", "tags", "sort_order", "is_public"} {
			_, err := db.Exec(fmt.Sprintf("ALTER TABLE timeline_events ADD COLUMN %s", col))
			if err == nil {
				log.Printf("[DB] Added missing column: %s", col)
			}
		}
	case 5:
		runMigrateAddColumns(db, "recurring", "weather_data", "user_id")
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			avatar_url TEXT DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO users (id, username, display_name, color) VALUES (1, 'default', 'Default User', '#7c3aed')`)
	case 6:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS ollama_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT 'http://localhost:11434',
			model TEXT DEFAULT 'llama3.2',
			enabled INTEGER DEFAULT 0
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)`)
	case 7:
		CreateFTS5Table(db)
	case 8:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN is_favorite INTEGER DEFAULT 0`)
	case 9:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS event_templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			person_id INTEGER DEFAULT 0,
			user_id INTEGER DEFAULT 0,
			location TEXT DEFAULT '',
			media_type TEXT DEFAULT 'image',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
	case 10:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS collection_events (
			collection_id INTEGER NOT NULL,
			event_id INTEGER NOT NULL,
			added_at TEXT DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (collection_id, event_id)
		)`)
	case 11:
	case 12:
	case 13:
	case 14:
	case 15:
		runMigrateAddColumns(db, "event_start_time", "event_end_time")
	case 16:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN deleted_at TEXT DEFAULT ''`)
	case 17:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS umami_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			site_id TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO umami_settings (id, url, site_id, enabled) VALUES (1, '', '', 0)`)
	case 18:
		_, err := db.Exec(`ALTER TABLE users ADD COLUMN email TEXT DEFAULT ''`)
		if err == nil {
			log.Printf("[DB] Added column: email to users")
		}
	}
}

func runMigrateAddColumns(db *sql.DB, cols ...string) {
	for _, col := range cols {
		_, err := db.Exec(fmt.Sprintf("ALTER TABLE timeline_events ADD COLUMN %s", col))
		if err == nil {
			log.Printf("[DB] Added column: %s", col)
		}
	}
}

// SeedEvents populates the database with sample events if empty.
func SeedEvents(db *sql.DB, basePath string) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
	if count > 0 {
		return
	}

	type event struct {
		Title       string
		Description string
		Date        string
		Location    string
		MediaType   string
		MediaURL    string
		Latitude    *float64
		Longitude   *float64
	}

	nyLat, nyLng := 40.7580, -73.9855
	cpLat, cpLng := 40.7829, -73.9654
	events := []event{
		{Title: "New Year Celebration", Description: "Welcome to the new year with fireworks and festivities!", Date: "2026-01-01", Location: "Times Square, NYC", MediaType: "image", MediaURL: "/media/newyear.jpg", Latitude: &nyLat, Longitude: &nyLng},
		{Title: "Summer Music Festival", Description: "Amazing performances under the stars", Date: "2026-07-15", Location: "Central Park", MediaType: "video", MediaURL: "/media/festival.mp4", Latitude: &cpLat, Longitude: &cpLng},
	}

	for _, e := range events {
		db.Exec("INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, latitude, longitude, is_public) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)",
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, "", e.Latitude, e.Longitude)

		parts := strings.Split(e.MediaURL, "/")
		if len(parts) > 2 {
			filePath := filepath.Join(basePath, parts[1], parts[2])
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				if f, err := os.Create(filePath); err == nil {
					f.Close()
				}
			}
		}
	}
}
