package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func newAccountTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			email TEXT DEFAULT '',
			color TEXT DEFAULT '#B5452B',
			avatar_url TEXT DEFAULT '',
			password_hash TEXT DEFAULT '',
			created_at TEXT DEFAULT '',
			oidc_sub TEXT DEFAULT '',
			auth_method TEXT DEFAULT '',
			is_admin INTEGER DEFAULT 0,
			analytics_opt_out INTEGER DEFAULT 0
		)`,
		`CREATE TABLE timeline_events (
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
			created_at TEXT DEFAULT '',
			person_id INTEGER,
			latitude REAL,
			longitude REAL,
			recurring TEXT DEFAULT '',
			weather_data TEXT DEFAULT '',
			user_id INTEGER DEFAULT 0,
			event_start_time TEXT DEFAULT '',
			event_end_time TEXT DEFAULT '',
			deleted_at TEXT DEFAULT '',
			reveal_at TEXT DEFAULT '',
			revealed_at TEXT DEFAULT '',
			source TEXT DEFAULT '',
			source_ref TEXT DEFAULT ''
		)`,
		`CREATE TABLE persons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			avatar_url TEXT DEFAULT '',
			bio TEXT DEFAULT '',
			birth_date TEXT DEFAULT '',
			color TEXT DEFAULT '#B5452B',
			created_at TEXT DEFAULT ''
		)`,
		`CREATE TABLE event_people (
			event_id INTEGER NOT NULL,
			person_id INTEGER NOT NULL,
			PRIMARY KEY (event_id, person_id)
		)`,
		`CREATE TABLE collection_events (
			collection_id INTEGER NOT NULL,
			event_id INTEGER NOT NULL,
			added_at TEXT DEFAULT '',
			PRIMARY KEY (collection_id, event_id)
		)`,
		`CREATE TABLE event_templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			person_id INTEGER DEFAULT 0,
			user_id INTEGER DEFAULT 0,
			location TEXT DEFAULT '',
			media_type TEXT DEFAULT '',
			created_at TEXT DEFAULT ''
		)`,
		`CREATE TABLE password_reset_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT UNIQUE NOT NULL,
			expires_at INTEGER NOT NULL,
			used_at INTEGER,
			created_at INTEGER DEFAULT 0
		)`,
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL,
			csrf TEXT DEFAULT ''
		)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return db
}

func insertAccountUser(t *testing.T, db *sql.DB, id int64, username string, isAdmin int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users
		(id, username, display_name, email, color, password_hash, auth_method, is_admin)
		VALUES (?, ?, ?, ?, '#B5452B', 'secret-hash', 'password', ?)`,
		id, username, username, username+"@example.test", isAdmin); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAccountRemovesOwnedDataOnly(t *testing.T) {
	db := newAccountTestDB(t)
	insertAccountUser(t, db, 10, "alice", 0)
	insertAccountUser(t, db, 11, "bob", 0)

	if _, err := db.Exec(`INSERT INTO persons (id, name, color) VALUES (1, 'Shared Person', '#B5452B')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO timeline_events (id, title, event_date, user_id, person_id) VALUES
		(100, 'Alice event', '2024-01-01', 10, 1),
		(101, 'Bob event', '2024-02-02', 11, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collection_events (collection_id, event_id) VALUES (1, 100), (1, 101)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO event_templates (title, user_id) VALUES ('Alice template', 10), ('Bob template', 11)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES (10, 'a', 1), (11, 'b', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, user_id, expires_at) VALUES ('sess-alice', 10, 9999999999), ('sess-bob', 11, 9999999999)`); err != nil {
		t.Fatal(err)
	}

	svc := New(Deps{DB: db})
	deleted, err := svc.DeleteAccount(10)
	if err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted events = %d, want 1", deleted)
	}

	checks := []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM users WHERE id = 10`, 0},
		{`SELECT COUNT(*) FROM users WHERE id = 11`, 1},
		{`SELECT COUNT(*) FROM timeline_events WHERE user_id = 10`, 0},
		{`SELECT COUNT(*) FROM timeline_events WHERE user_id = 11`, 1},
		{`SELECT COUNT(*) FROM collection_events WHERE event_id = 100`, 0},
		{`SELECT COUNT(*) FROM collection_events WHERE event_id = 101`, 1},
		{`SELECT COUNT(*) FROM event_templates WHERE user_id = 10`, 0},
		{`SELECT COUNT(*) FROM event_templates WHERE user_id = 11`, 1},
		{`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = 10`, 0},
		{`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = 11`, 1},
		{`SELECT COUNT(*) FROM sessions WHERE user_id = 10`, 0},
		{`SELECT COUNT(*) FROM sessions WHERE user_id = 11`, 1},
		// Persons are shared records and must survive account deletion.
		{`SELECT COUNT(*) FROM persons WHERE id = 1`, 1},
	}
	for _, check := range checks {
		var got int
		if err := db.QueryRow(check.query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", check.query, err)
		}
		if got != check.want {
			t.Errorf("%s = %d, want %d", check.query, got, check.want)
		}
	}
}

func TestDeleteAccountRefusesLegacyAdmin(t *testing.T) {
	db := newAccountTestDB(t)
	svc := New(Deps{DB: db})
	if _, err := svc.DeleteAccount(0); !errors.Is(err, ErrLegacyAdmin) {
		t.Fatalf("DeleteAccount(0) = %v, want ErrLegacyAdmin", err)
	}
}

func TestDeleteAccountRefusesLastAdmin(t *testing.T) {
	db := newAccountTestDB(t)
	insertAccountUser(t, db, 20, "onlyadmin", 1)
	svc := New(Deps{DB: db})
	if _, err := svc.DeleteAccount(20); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("DeleteAccount(last admin) = %v, want ErrLastAdmin", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = 20`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("last admin was deleted")
	}
}

func TestExportOwnAccountOnlyIncludesCallerData(t *testing.T) {
	db := newAccountTestDB(t)
	insertAccountUser(t, db, 10, "alice", 0)
	insertAccountUser(t, db, 11, "bob", 0)
	if _, err := db.Exec(`INSERT INTO persons (id, name, color) VALUES (1, 'Shared Person', '#B5452B')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO timeline_events (title, event_date, user_id, person_id) VALUES
		('Alice event', '2024-01-01', 10, 1),
		('Bob event', '2024-02-02', 11, 1)`); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	svc := New(Deps{DB: db})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/me/export", nil)
	c.Set(CtxKeyUserID, int64(10))
	c.Set(CtxKeyUserName, "alice")
	c.Set(CtxKeyUserIsAdmin, false)

	svc.ExportOwnAccount(c)
	if w.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", w.Code, w.Body.String())
	}

	var export AccountExport
	if err := json.Unmarshal(w.Body.Bytes(), &export); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if len(export.Events) != 1 || export.Events[0].Title != "Alice event" {
		t.Errorf("export events = %+v, want only Alice's event", export.Events)
	}
	if export.Account.ID != 10 || export.Account.Username != "alice" {
		t.Errorf("export account = %+v", export.Account)
	}
	if optOut, ok := export.Preferences["analytics_opt_out"]; !ok || optOut != false {
		t.Errorf("preferences = %+v, want analytics_opt_out=false", export.Preferences)
	}

	body := w.Body.String()
	for _, forbidden := range []string{`"password":`, `"password_hash"`, `"session"`, `"csrf"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("export contains forbidden field %s: %s", forbidden, body)
		}
	}
}

func TestPrivacySettingsRoundTrip(t *testing.T) {
	db := newAccountTestDB(t)
	insertAccountUser(t, db, 10, "alice", 0)

	gin.SetMode(gin.TestMode)
	svc := New(Deps{DB: db})

	get := func() PrivacySettings {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/me/privacy", nil)
		c.Set(CtxKeyUserID, int64(10))
		svc.GetPrivacySettings(c)
		if w.Code != http.StatusOK {
			t.Fatalf("get privacy status = %d, body = %s", w.Code, w.Body.String())
		}
		var settings PrivacySettings
		if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
			t.Fatal(err)
		}
		return settings
	}

	if settings := get(); settings.AnalyticsOptOut {
		t.Fatal("analytics_opt_out should default to false")
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/me/privacy", strings.NewReader(`{"analytics_opt_out":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(CtxKeyUserID, int64(10))
	svc.UpdatePrivacySettings(c)
	if w.Code != http.StatusOK {
		t.Fatalf("update privacy status = %d, body = %s", w.Code, w.Body.String())
	}

	if settings := get(); !settings.AnalyticsOptOut {
		t.Fatal("analytics_opt_out was not persisted")
	}
}
