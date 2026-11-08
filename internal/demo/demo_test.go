package demo

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func newDemoTestDB(t *testing.T) *sql.DB {
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
			is_admin INTEGER DEFAULT 0
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
			source TEXT DEFAULT '',
			source_ref TEXT DEFAULT ''
		)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return db
}

func testConfig() Config {
	return Config{
		Enabled:     true,
		Username:    "demo",
		Password:    "demo-pass",
		Email:       "demo@traces.local",
		DisplayName: "Demo User",
		SeedContent: true,
	}
}

func TestEnsureIsIdempotentAndNeverPromotes(t *testing.T) {
	db := newDemoTestDB(t)
	cfg := testConfig()

	if err := Ensure(db, cfg, nil); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}

	var (
		id           int64
		isAdmin      int
		displayName  string
		authMethod   string
		passwordHash string
	)
	if err := db.QueryRow(`SELECT id, COALESCE(is_admin,0), COALESCE(display_name,''),
		COALESCE(auth_method,''), COALESCE(password_hash,'') FROM users WHERE username = 'demo'`).
		Scan(&id, &isAdmin, &displayName, &authMethod, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if isAdmin != 0 {
		t.Error("demo account was created as an admin")
	}
	if displayName != cfg.DisplayName || authMethod != "password" {
		t.Errorf("demo account = display_name %q auth_method %q", displayName, authMethod)
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(cfg.Password)) != nil {
		t.Error("demo password hash does not match configured password")
	}

	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE user_id = ?`, id).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 4 {
		t.Fatalf("seeded events = %d, want 4", events)
	}

	// A second startup must not duplicate the account or the seeded content.
	if err := Ensure(db, cfg, nil); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	var users int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'demo'`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Errorf("demo users = %d, want 1", users)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE user_id = ?`, id).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 4 {
		t.Errorf("seeded events after second Ensure = %d, want 4", events)
	}

	// Tamper with the account, then confirm reconciliation: the password and
	// display name are repaired, but is_admin is never touched.
	otherHash, err := bcrypt.GenerateFromPassword([]byte("different"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET password_hash = ?, display_name = 'Hacked', is_admin = 1 WHERE id = ?`,
		string(otherHash), id); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(db, cfg, nil); err != nil {
		t.Fatalf("reconcile Ensure: %v", err)
	}
	if err := db.QueryRow(`SELECT COALESCE(is_admin,0), COALESCE(display_name,''), COALESCE(password_hash,'')
		FROM users WHERE id = ?`, id).Scan(&isAdmin, &displayName, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if isAdmin != 1 {
		t.Error("Ensure changed is_admin on an existing account")
	}
	if displayName != cfg.DisplayName {
		t.Errorf("display_name = %q, want %q", displayName, cfg.DisplayName)
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(cfg.Password)) != nil {
		t.Error("Ensure did not reset the demo password")
	}
}

func TestEnsureSkipsAccountOwnedByDifferentEmail(t *testing.T) {
	db := newDemoTestDB(t)
	existingHash, err := bcrypt.GenerateFromPassword([]byte("owner-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, display_name, email, password_hash, is_admin)
		VALUES ('demo', 'Real Owner', 'owner@example.test', ?, 1)`, string(existingHash)); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	if err := Ensure(db, cfg, nil); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	var displayName, email, passwordHash string
	if err := db.QueryRow(`SELECT display_name, email, password_hash FROM users WHERE username = 'demo'`).
		Scan(&displayName, &email, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if displayName != "Real Owner" || email != "owner@example.test" {
		t.Errorf("account was taken over: display_name %q email %q", displayName, email)
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("owner-password")) != nil {
		t.Error("Ensure changed a password it does not own")
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM timeline_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Errorf("seeded %d events into an account owned by someone else", events)
	}
}
