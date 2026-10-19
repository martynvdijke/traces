package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"traces/internal/models"
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

// Migrate runs schema initialization and migrations. A migration version is
// recorded only after every statement in that step succeeds, so a failure
// leaves the database at the last good version instead of silently skipping
// half-applied changes.
func Migrate(db *sql.DB) error {
	var schemaVersion int
	err := db.QueryRow("SELECT COALESCE(MAX(version), -1) FROM schema_version").Scan(&schemaVersion)
	if err != nil {
		schemaVersion = -1
	}
	if schemaVersion == -1 {
		if _, err := db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)"); err != nil {
			return fmt.Errorf("create schema_version: %w", err)
		}
	}

	if schemaVersion < 0 {
		CreateTables(db)
		CreateFTS5Table(db)
		seedDefaults(db)
		schemaVersion = 0
	}

	for v := schemaVersion; v < models.CurrentSchemaVersion; v++ {
		if err := RunMigration(db, v); err != nil {
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", v+1); err != nil {
			return fmt.Errorf("record migration %d: %w", v+1, err)
		}
	}

	CreateFTS5Table(db)
	return nil
}

func CreateTables(db *sql.DB) {
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
			deleted_at TEXT DEFAULT '',
			source TEXT DEFAULT '',
			source_ref TEXT DEFAULT ''
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
			color TEXT DEFAULT '#B5452B',
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
			color TEXT DEFAULT '#B5452B',
			avatar_url TEXT DEFAULT '',
			password_hash TEXT DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			oidc_sub TEXT DEFAULT '',
			auth_method TEXT DEFAULT '',
			is_admin INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL,
			csrf TEXT DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
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
		`CREATE TABLE IF NOT EXISTS bgg_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0,
			last_sync TEXT DEFAULT ''
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
			color TEXT DEFAULT '#B5452B',
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
		`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT UNIQUE NOT NULL,
			expires_at INTEGER NOT NULL,
			used_at INTEGER,
			created_at INTEGER DEFAULT (unixepoch())
		)`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user_id ON password_reset_tokens(user_id)`,
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
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_timeline_events_bgg_ref ON timeline_events(source_ref) WHERE source='bgg'`)
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
		db.Exec("INSERT INTO users (id, username, display_name, email, color) VALUES (1, 'default', 'Default User', '', '#B5452B')")
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

	db.QueryRow("SELECT COUNT(*) FROM bgg_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO bgg_settings (id, username, enabled, last_sync) VALUES (1, '', 0, '')")
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
func RunMigration(db *sql.DB, fromVersion int) error {
	var migErr error
	migExec := func(query string, args ...any) {
		if migErr != nil {
			return
		}
		if _, err := db.Exec(query, args...); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			migErr = err
			return
		}
	}

	switch fromVersion {
	case 0:
		migExec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
		migExecAddColumns(migExec, "media_caption", "tags", "sort_order", "is_public")
		migExec(`CREATE TABLE IF NOT EXISTS share_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE,
			event_ids TEXT,
			year TEXT,
			expires_at TEXT
		)`)
	case 2:
		migExec(`CREATE TABLE IF NOT EXISTS persons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			avatar_url TEXT DEFAULT '',
			bio TEXT DEFAULT '',
			birth_date TEXT DEFAULT '',
			color TEXT DEFAULT '#B5452B',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		migExec(`CREATE TABLE IF NOT EXISTS gotify_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			token TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`)
		migExec(`INSERT OR IGNORE INTO gotify_settings (id, url, token, enabled) VALUES (1, '', '', 0)`)
		migExecAddColumns(migExec, "person_id", "latitude", "longitude")
	case 3:
		migExec(`CREATE TABLE IF NOT EXISTS memories_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER DEFAULT 1,
			days_window INTEGER DEFAULT 3,
			email_enabled INTEGER DEFAULT 0,
			last_sent_date TEXT DEFAULT ''
		)`)
		migExec(`INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)`)
		migExec(`CREATE TABLE IF NOT EXISTS email_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			smtp_host TEXT DEFAULT '',
			smtp_port INTEGER DEFAULT 587,
			smtp_user TEXT DEFAULT '',
			smtp_pass TEXT DEFAULT '',
			from_addr TEXT DEFAULT '',
			to_addr TEXT DEFAULT ''
		)`)
		migExec(`INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)`)
	case 4:
		for _, col := range []string{"person_id", "latitude", "longitude", "media_caption", "tags", "sort_order", "is_public"} {
			migExec(fmt.Sprintf("ALTER TABLE timeline_events ADD COLUMN %s", col))
		}
	case 5:
		migExecAddColumns(migExec, "recurring", "weather_data", "user_id")
		migExec(`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			color TEXT DEFAULT '#B5452B',
			avatar_url TEXT DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		migExec(`INSERT OR IGNORE INTO users (id, username, display_name, color) VALUES (1, 'default', 'Default User', '#B5452B')`)
	case 6:
		migExec(`CREATE TABLE IF NOT EXISTS ollama_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT 'http://localhost:11434',
			model TEXT DEFAULT 'llama3.2',
			enabled INTEGER DEFAULT 0
		)`)
		migExec(`INSERT OR IGNORE INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)`)
	case 7:
		CreateFTS5Table(db)
	case 8:
		migExec(`ALTER TABLE timeline_events ADD COLUMN is_favorite INTEGER DEFAULT 0`)
	case 9:
		migExec(`CREATE TABLE IF NOT EXISTS event_templates (
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
		migExec(`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			color TEXT DEFAULT '#B5452B',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		migExec(`CREATE TABLE IF NOT EXISTS collection_events (
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
		migExecAddColumns(migExec, "event_start_time", "event_end_time")
	case 16:
		migExec(`ALTER TABLE timeline_events ADD COLUMN deleted_at TEXT DEFAULT ''`)
	case 17:
		migExec(`CREATE TABLE IF NOT EXISTS umami_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			site_id TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`)
		migExec(`INSERT OR IGNORE INTO umami_settings (id, url, site_id, enabled) VALUES (1, '', '', 0)`)
	case 18:
		migExec(`ALTER TABLE users ADD COLUMN email TEXT DEFAULT ''`)
	case 19:
		migExec(`ALTER TABLE users ADD COLUMN password_hash TEXT DEFAULT ''`)
	case 20:
		migExec(`ALTER TABLE timeline_events ADD COLUMN source TEXT DEFAULT ''`)
		migExec(`ALTER TABLE timeline_events ADD COLUMN source_ref TEXT DEFAULT ''`)
		migExec(`CREATE TABLE IF NOT EXISTS bgg_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0,
			last_sync TEXT DEFAULT ''
		)`)
		migExec(`INSERT OR IGNORE INTO bgg_settings (id, username, enabled, last_sync) VALUES (1, '', 0, '')`)
		migExec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_timeline_events_bgg_ref ON timeline_events(source_ref) WHERE source='bgg'`)
		migExec(`UPDATE timeline_events SET media_type='boardgame' WHERE source='bgg' AND (media_type IS NULL OR media_type='')`)
	case 21:
		migExec(`ALTER TABLE users ADD COLUMN oidc_sub TEXT DEFAULT ''`)
		migExec(`ALTER TABLE users ADD COLUMN auth_method TEXT DEFAULT ''`)
		migExec(`ALTER TABLE users ADD COLUMN is_admin INTEGER DEFAULT 0`)
		migExec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_sub ON users(oidc_sub) WHERE oidc_sub <> ''`)
		migExec(`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`)
	case 22:
		migExec(`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT UNIQUE NOT NULL,
			expires_at INTEGER NOT NULL,
			used_at INTEGER,
			created_at INTEGER DEFAULT (unixepoch())
		)`)
		migExec(`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user_id ON password_reset_tokens(user_id)`)
	case 23:
		migExec(`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL,
			csrf TEXT DEFAULT ''
		)`)
		migExec(`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`)
	}
	return migErr
}

func migExecAddColumns(exec func(string, ...any), cols ...string) {
	for _, col := range cols {
		exec(fmt.Sprintf("ALTER TABLE timeline_events ADD COLUMN %s", col))
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
