package database

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMigrateRepairsSchemaDrift simulates a database whose schema_version was
// advanced to the current version without the corresponding columns having
// been added (the production failure mode behind "no such column:
// e.is_favorite").
func TestMigrateRepairsSchemaDrift(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (26)`); err != nil {
		t.Fatal(err)
	}

	// Minimal timeline_events without is_favorite (and without any of the other
	// drift columns), but with the fields CreateFTS5Table needs.
	if _, err := db.Exec(`CREATE TABLE timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		tags TEXT DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}

	// Minimal users without analytics_opt_out.
	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE
	)`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	eventCols := timelineEventsColumns(t, db)
	for _, col := range []string{
		"is_favorite", "event_start_time", "event_end_time", "deleted_at",
		"source", "source_ref", "person_id", "latitude", "longitude",
	} {
		if !eventCols[col] {
			t.Errorf("timeline_events.%s missing after drift repair", col)
		}
	}

	userCols := usersColumns(t, db)
	for _, col := range []string{"analytics_opt_out", "oidc_sub", "auth_method", "is_admin"} {
		if !userCols[col] {
			t.Errorf("users.%s missing after drift repair", col)
		}
	}

	if !hasIndex(t, db, "idx_event_people_person") {
		t.Error("idx_event_people_person missing after drift repair")
	}

	var name string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='event_people'").Scan(&name); err != nil || name != "event_people" {
		t.Errorf("event_people table missing after drift repair: %q err=%v", name, err)
	}
}
