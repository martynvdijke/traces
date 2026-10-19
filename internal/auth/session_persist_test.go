package auth

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newSessionDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL DEFAULT 0,
		expires_at INTEGER NOT NULL,
		csrf TEXT DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSessionStorePersistsAcrossRestart(t *testing.T) {
	db := newSessionDB(t)

	first := NewSessionStore()
	first.SetDB(db)
	first.Set("sess-1", SessionInfo{UserID: 42, ExpiresAt: 9999999999})
	first.SetCSRF("sess-1", "csrf-token")

	// A new store simulates a process restart: the in-memory cache is empty,
	// so the session and CSRF token must be read back from the database.
	second := NewSessionStore()
	second.SetDB(db)

	info, ok := second.Get("sess-1")
	if !ok {
		t.Fatal("session not restored after restart")
	}
	if info.UserID != 42 || info.ExpiresAt != 9999999999 {
		t.Errorf("restored session = %+v", info)
	}
	if token, ok := second.GetCSRF("sess-1"); !ok || token != "csrf-token" {
		t.Errorf("restored csrf = %q ok=%v", token, ok)
	}

	second.Delete("sess-1")
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = 'sess-1'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("deleted session row still persisted")
	}
}

func TestSessionStoreCleanupRemovesPersistedRows(t *testing.T) {
	db := newSessionDB(t)
	store := NewSessionStore()
	store.SetDB(db)
	store.Set("expired", SessionInfo{UserID: 1, ExpiresAt: 100})
	store.CleanupExpired(200)

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = 'expired'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expired session row count = %d, want 0", count)
	}
}
