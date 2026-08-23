package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// setupTRMNLTestDB replaces the global db with an in-memory database holding the
// timeline_events and persons tables, and returns a router with the TRMNL summary
// endpoint registered. Callers must restore the original db and publicMode globals.
func setupTRMNLTestDB(t *testing.T) (*gin.Engine, *sql.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origDB := db
	origPublicMode := publicMode
	t.Cleanup(func() {
		db = origDB
		publicMode = origPublicMode
	})

	testDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testDB.Close() })
	db = testDB

	if _, err := db.Exec(`CREATE TABLE timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0,
		deleted_at TEXT DEFAULT '',
		source TEXT DEFAULT '',
		source_ref TEXT DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/api/trmnl/summary", getTRMNLSummary)
	return router, testDB
}

// currentMonthDate builds a date string inside the current calendar month for the given year.
func currentMonthDate(year int, day int) string {
	return fmt.Sprintf("%d-%02d-%02d", year, int(time.Now().Month()), day)
}

// otherMonthDate builds a date string in a month different from the current one.
func otherMonthDate(year int, day int) string {
	month := int(time.Now().Month())%12 + 1
	return fmt.Sprintf("%d-%02d-%02d", year, month, day)
}

func insertTestEvent(t *testing.T, title, date string, isPublic, isFavorite int, mediaType, tags string, personID *int) {
	t.Helper()
	var err error
	if personID != nil {
		_, err = db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags, person_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			title, date, isPublic, isFavorite, mediaType, tags, *personID)
	} else {
		_, err = db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, ?, ?, ?, ?)`,
			title, date, isPublic, isFavorite, mediaType, tags)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func requestTRMNLSummary(t *testing.T, router *gin.Engine) trmnlSummary {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/trmnl/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var summary trmnlSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	return summary
}

func TestTRMNLSummaryEmpty(t *testing.T) {
	router, _ := setupTRMNLTestDB(t)
	publicMode = false

	summary := requestTRMNLSummary(t, router)

	if summary.Month != time.Now().Month().String() {
		t.Errorf("expected month %q, got %q", time.Now().Month().String(), summary.Month)
	}
	if summary.Events == nil {
		t.Error("expected events to be a non-nil empty array")
	}
	if len(summary.Events) != 0 {
		t.Errorf("expected no events, got %d", len(summary.Events))
	}
	if summary.Stats.EventCount != 0 {
		t.Errorf("expected event_count 0, got %d", summary.Stats.EventCount)
	}
	if summary.Stats.Media == nil || summary.Stats.TopTags == nil || summary.Stats.TopPersons == nil {
		t.Error("expected stats arrays to be non-nil (empty) arrays")
	}
}

func TestTRMNLSummaryPublicOnly(t *testing.T) {
	router, _ := setupTRMNLTestDB(t)
	publicMode = false

	insertTestEvent(t, "public event", currentMonthDate(2024, 15), 1, 0, "photo", "holiday", nil)
	insertTestEvent(t, "private event", currentMonthDate(2023, 10), 0, 1, "video", "secret", nil)

	summary := requestTRMNLSummary(t, router)

	if len(summary.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(summary.Events))
	}
	if summary.Events[0].Title != "public event" {
		t.Errorf("expected only the public event, got %q", summary.Events[0].Title)
	}
	if summary.Stats.EventCount != 1 {
		t.Errorf("expected event_count 1, got %d", summary.Stats.EventCount)
	}
}

func TestTRMNLSummaryPublicModeServesAll(t *testing.T) {
	router, _ := setupTRMNLTestDB(t)
	publicMode = true

	insertTestEvent(t, "public event", currentMonthDate(2024, 15), 1, 0, "photo", "holiday", nil)
	insertTestEvent(t, "private event", currentMonthDate(2023, 10), 0, 1, "video", "secret", nil)

	summary := requestTRMNLSummary(t, router)

	if len(summary.Events) != 2 {
		t.Errorf("expected both events in public mode, got %d", len(summary.Events))
	}
	if summary.Stats.EventCount != 2 {
		t.Errorf("expected event_count 2, got %d", summary.Stats.EventCount)
	}
}

func TestTRMNLSummaryMonthFilter(t *testing.T) {
	router, _ := setupTRMNLTestDB(t)
	publicMode = false

	insertTestEvent(t, "this month", currentMonthDate(2022, 5), 1, 0, "photo", "a", nil)
	insertTestEvent(t, "another month", otherMonthDate(2021, 20), 1, 0, "photo", "b", nil)

	summary := requestTRMNLSummary(t, router)

	if len(summary.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(summary.Events))
	}
	if summary.Events[0].Title != "this month" {
		t.Errorf("expected only current-month event, got %q", summary.Events[0].Title)
	}
	if summary.Stats.EventCount != 1 {
		t.Errorf("expected event_count 1, got %d", summary.Stats.EventCount)
	}
}

func TestTRMNLSummaryFavoritesFirstAndLimit(t *testing.T) {
	router, _ := setupTRMNLTestDB(t)
	publicMode = false

	// 10 public events in the current month; 3 are favorites (ids 8, 9, 10 by insertion).
	for i := 1; i <= 10; i++ {
		insertTestEvent(t, fmt.Sprintf("event %d", i), currentMonthDate(2024, i), 1, 0, "photo", "t", nil)
	}
	for _, day := range []int{28, 29, 30} {
		insertTestEvent(t, fmt.Sprintf("fav %d", day), currentMonthDate(2024, day), 1, 1, "photo", "t", nil)
	}

	summary := requestTRMNLSummary(t, router)

	if len(summary.Events) != 8 {
		t.Fatalf("expected 8 events (limit), got %d", len(summary.Events))
	}
	// The three favorites must come first, newest of them first.
	if summary.Events[0].Title != "fav 30" || summary.Events[1].Title != "fav 29" || summary.Events[2].Title != "fav 28" {
		t.Errorf("expected favorites first in date order, got %v", summary.Events[0:3])
	}
	for _, ev := range summary.Events[:3] {
		if !ev.IsFavorite {
			t.Errorf("expected event %q to be marked favorite", ev.Title)
		}
	}
	if summary.Stats.EventCount != 13 {
		t.Errorf("expected event_count 13 in stats, got %d", summary.Stats.EventCount)
	}
	if summary.Stats.FavoriteCount != 3 {
		t.Errorf("expected favorite_count 3, got %d", summary.Stats.FavoriteCount)
	}
}

func TestTRMNLSummaryStatsAggregation(t *testing.T) {
	router, _ := setupTRMNLTestDB(t)
	publicMode = false

	// Person linked to the first two events.
	personRes, err := db.Exec(`INSERT INTO persons (name) VALUES ('Alice')`)
	if err != nil {
		t.Fatal(err)
	}
	personID, _ := personRes.LastInsertId()
	pid := int(personID)

	insertTestEvent(t, "e1", currentMonthDate(2024, 1), 1, 1, "photo", "holiday,sea", &pid)
	insertTestEvent(t, "e2", currentMonthDate(2023, 2), 1, 0, "photo", "holiday", &pid)
	insertTestEvent(t, "e3", currentMonthDate(2022, 3), 1, 0, "video", "", nil)
	insertTestEvent(t, "e4", currentMonthDate(2021, 4), 1, 0, "", "", nil)

	summary := requestTRMNLSummary(t, router)

	if summary.Stats.EventCount != 4 {
		t.Errorf("expected event_count 4, got %d", summary.Stats.EventCount)
	}
	if summary.Stats.FavoriteCount != 1 {
		t.Errorf("expected favorite_count 1, got %d", summary.Stats.FavoriteCount)
	}

	media := map[string]int{}
	for _, m := range summary.Stats.Media {
		media[m.Type] = m.Count
	}
	if media["photo"] != 2 || media["video"] != 1 || media["text"] != 1 {
		t.Errorf("unexpected media breakdown: %v", summary.Stats.Media)
	}

	if len(summary.Stats.TopTags) != 2 {
		t.Fatalf("expected 2 top tags, got %v", summary.Stats.TopTags)
	}
	if summary.Stats.TopTags[0].Tag != "holiday" || summary.Stats.TopTags[0].Count != 2 {
		t.Errorf("expected holiday to top the tags, got %v", summary.Stats.TopTags)
	}

	if len(summary.Stats.TopPersons) != 1 || summary.Stats.TopPersons[0].Name != "Alice" || summary.Stats.TopPersons[0].Count != 2 {
		t.Errorf("expected Alice with count 2, got %v", summary.Stats.TopPersons)
	}

	// e4 has no person: verify event fields carry person name and year.
	for _, ev := range summary.Events {
		if ev.ID == 1 && ev.PersonName != "Alice" {
			t.Errorf("expected e1 person_name Alice, got %q", ev.PersonName)
		}
		if ev.ID == 1 && ev.Year != 2024 {
			t.Errorf("expected e1 year 2024, got %d", ev.Year)
		}
	}
}
