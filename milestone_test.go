package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func setupMilestoneTestDB(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origDB := db
	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
	)`)

	return func() {
		db.Close()
		db = origDB
	}
}

func TestGetPersonEventsMilestones(t *testing.T) {
	cleanup := setupMilestoneTestDB(t)
	defer cleanup()

	r := gin.New()
	r.GET("/api/persons/:id/events", getPersonEvents)

	t.Run("person_not_found", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/persons/999/events", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
	})

	t.Run("invalid_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/persons/abc/events", nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})

	t.Run("with_birth_date_age_and_life_groups", func(t *testing.T) {
		db.Exec(`INSERT INTO persons (id, name, birth_date) VALUES (1, 'Mila', '2022-03-15')`)
		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, tags, person_id) VALUES
			('First steps', '', '2023-05-20', '', 'image', '', 1),
			('First birthday', '', '2023-03-15', '', 'image', '', 1),
			('Baby shower', '', '2022-01-10', '', 'image', '', 1)`)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/persons/1/events", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}

		var resp PersonEventsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Person.Name != "Mila" || resp.Person.BirthDate != "2022-03-15" {
			t.Errorf("person = %+v, want Mila born 2022-03-15", resp.Person)
		}
		if len(resp.Events) != 3 {
			t.Fatalf("got %d events, want 3", len(resp.Events))
		}

		byTitle := map[string]PersonMilestone{}
		for _, m := range resp.Events {
			byTitle[m.Title] = m
		}

		steps := byTitle["First steps"]
		if steps.AgeYears == nil || steps.AgeMonths == nil {
			t.Fatalf("First steps should have age fields: %+v", steps)
		}
		if *steps.AgeYears != 1 || *steps.AgeMonths != 2 {
			t.Errorf("First steps age = %dy%dm, want 1y2m", *steps.AgeYears, *steps.AgeMonths)
		}
		if steps.Group != "Year 2" {
			t.Errorf("First steps group = %q, want Year 2", steps.Group)
		}

		bday := byTitle["First birthday"]
		if bday.AgeYears == nil || *bday.AgeYears != 1 || *bday.AgeMonths != 0 {
			t.Errorf("First birthday age = %v, want 1y0m", bday.AgeYears)
		}
		if bday.Group != "Year 2" {
			t.Errorf("First birthday group = %q, want Year 2", bday.Group)
		}

		shower := byTitle["Baby shower"]
		if shower.AgeYears != nil || shower.AgeMonths != nil {
			t.Errorf("Baby shower should have no age fields (before birth)")
		}
		if shower.Group != "Before" {
			t.Errorf("Baby shower group = %q, want Before", shower.Group)
		}
	})

	t.Run("without_birth_date_calendar_groups", func(t *testing.T) {
		db.Exec(`INSERT INTO persons (id, name, birth_date) VALUES (2, 'Anna', '')`)
		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, tags, person_id) VALUES
			('Trip', '', '2019-07-04', '', 'image', '', 2),
			('Mystery', '', 'not-a-date', '', 'image', '', 2)`)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/persons/2/events", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		var resp PersonEventsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Events) != 2 {
			t.Fatalf("got %d events, want 2", len(resp.Events))
		}

		byTitle := map[string]PersonMilestone{}
		for _, m := range resp.Events {
			byTitle[m.Title] = m
		}

		trip := byTitle["Trip"]
		if trip.AgeYears != nil || trip.AgeMonths != nil {
			t.Errorf("Trip should have no age fields without birth date")
		}
		if trip.Group != "2019" {
			t.Errorf("Trip group = %q, want 2019", trip.Group)
		}

		mystery := byTitle["Mystery"]
		if mystery.Group != "Undated" {
			t.Errorf("Mystery group = %q, want Undated", mystery.Group)
		}
	})

	t.Run("empty_events", func(t *testing.T) {
		db.Exec(`INSERT INTO persons (id, name, birth_date) VALUES (3, 'Nobody', '2020-01-01')`)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/persons/3/events", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		var resp PersonEventsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Events) != 0 {
			t.Errorf("got %d events, want 0", len(resp.Events))
		}
	})
}
