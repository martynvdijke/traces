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
