package database

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"traces/internal/models"
)

func TestMigrateAddsTimeCapsuleColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	cols, err := tableColumns(db, "timeline_events")
	if err != nil {
		t.Fatalf("tableColumns: %v", err)
	}
	for _, want := range []string{"reveal_at", "revealed_at"} {
		if !cols[want] {
			t.Fatalf("timeline_events is missing column %q (have %v)", want, cols)
		}
	}

	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != models.CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, models.CurrentSchemaVersion)
	}

	var indexName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_timeline_events_reveal_at'").Scan(&indexName); err != nil {
		t.Fatalf("partial reveal index missing: %v", err)
	}
}

func TestMigrateKeepsExistingEventsUnsealed(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO timeline_events (title, event_date, user_id) VALUES ('Existing', '2026-01-01', 1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Re-running migrations must be a no-op and must not seal old rows.
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var revealAt, revealedAt string
	if err := db.QueryRow(`SELECT COALESCE(reveal_at,''), COALESCE(revealed_at,'') FROM timeline_events WHERE title = 'Existing'`).Scan(&revealAt, &revealedAt); err != nil {
		t.Fatalf("select: %v", err)
	}
	if revealAt != "" || revealedAt != "" {
		t.Fatalf("pre-existing event was sealed: reveal_at=%q revealed_at=%q", revealAt, revealedAt)
	}
}
