package database

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func usersColumns(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		t.Fatalf("table_info(users): %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	return cols
}

func TestMigrateFreshDBAddsPasswordHash(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	Migrate(db)

	cols := usersColumns(t, db)
	if !cols["password_hash"] {
		t.Error("fresh DB: users.password_hash column missing")
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count == 0 {
		t.Error("fresh DB: default user not seeded")
	}
}

func TestMigrateUpgradedDBAddsPasswordHash(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Simulate a pre-19 database: schema_version at 18 and a users table
	// without the password_hash column.
	if _, err := db.Exec(`CREATE TABLE schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (18)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		email TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		avatar_url TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'default', 'Default User')`); err != nil {
		t.Fatal(err)
	}

	Migrate(db)

	cols := usersColumns(t, db)
	if !cols["password_hash"] {
		t.Error("upgraded DB: users.password_hash column missing after migration")
	}

	// Existing data survives the migration.
	var name string
	if err := db.QueryRow("SELECT display_name FROM users WHERE id = 1").Scan(&name); err != nil || name != "Default User" {
		t.Errorf("existing user lost in migration: %q err=%v", name, err)
	}

	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil || version < 19 {
		t.Errorf("schema version not advanced: got %d err=%v", version, err)
	}
}

func timelineEventsColumns(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(timeline_events)`)
	if err != nil {
		t.Fatalf("table_info(timeline_events): %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	return cols
}

func hasIndex(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name)
	if err != nil {
		t.Fatalf("sqlite_master query: %v", err)
	}
	defer rows.Close()
	return rows.Next()
}

func TestMigrateFreshDBHasBGGColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	Migrate(db)

	cols := timelineEventsColumns(t, db)
	if !cols["source"] {
		t.Error("fresh DB: timeline_events.source column missing")
	}
	if !cols["source_ref"] {
		t.Error("fresh DB: timeline_events.source_ref column missing")
	}

	// bgg_settings exists with row id=1
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM bgg_settings WHERE id=1`).Scan(&count); err != nil {
		t.Fatalf("bgg_settings query: %v", err)
	}
	if count != 1 {
		t.Errorf("fresh DB: bgg_settings row id=1 missing, count=%d", count)
	}

	if !hasIndex(t, db, "idx_timeline_events_bgg_ref") {
		t.Error("fresh DB: index idx_timeline_events_bgg_ref missing")
	}
}

func TestMigrateUpgradedDBAddsBGGColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Simulate old DB at version 20 with timeline_events without bgg cols
	if _, err := db.Exec(`CREATE TABLE schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (20)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE timeline_events (
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
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Old Event','2024-06-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		email TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		avatar_url TEXT DEFAULT '',
		password_hash TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}

	Migrate(db)

	cols := timelineEventsColumns(t, db)
	if !cols["source"] {
		t.Error("upgraded DB: timeline_events.source column missing after migration")
	}
	if !cols["source_ref"] {
		t.Error("upgraded DB: timeline_events.source_ref column missing after migration")
	}

	if !hasIndex(t, db, "idx_timeline_events_bgg_ref") {
		t.Error("upgraded DB: index idx_timeline_events_bgg_ref missing after migration")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM bgg_settings WHERE id=1`).Scan(&count); err != nil {
		t.Fatalf("bgg_settings query: %v", err)
	}
	if count != 1 {
		t.Errorf("upgraded DB: bgg_settings row missing, count=%d", count)
	}

	// Existing data survives
	var title string
	if err := db.QueryRow(`SELECT title FROM timeline_events WHERE id=1`).Scan(&title); err != nil || title != "Old Event" {
		t.Errorf("existing event lost in migration: %q err=%v", title, err)
	}

	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil || version < 21 {
		t.Errorf("schema version not advanced: got %d err=%v", version, err)
	}
}
