package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// setupBGGTestDB swaps global db with an in-memory DB containing required tables.
func setupBGGTestDB(t *testing.T) *sql.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origDB := db
	t.Cleanup(func() { db = origDB })

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
	if _, err := db.Exec(`CREATE TABLE bgg_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		username TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0,
		last_sync TEXT DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO bgg_settings (id, username, enabled, last_sync) VALUES (1, '', 0, '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_timeline_events_bgg_ref ON timeline_events(source_ref) WHERE source='bgg'`); err != nil {
		t.Fatal(err)
	}

	return testDB
}

func sampleBGGPlay(id, date, game string) bggPlayXML {
	return bggPlayXML{
		ID:       id,
		Date:     date,
		Location: "Home",
		Item:     bggItemXML{Name: game, ObjectID: "123", ObjectType: "thing"},
		Comments: "Great game",
	}
}

func TestImportBGGPlayDedupe(t *testing.T) {
	database := setupBGGTestDB(t)

	play := sampleBGGPlay("12345", "2026-05-01", "Catan")

	ok, err := importBGGPlay(database, play)
	if err != nil || !ok {
		t.Fatalf("first import failed: ok=%v err=%v", ok, err)
	}

	ok2, err := importBGGPlay(database, play)
	if err != nil {
		t.Fatalf("second import err: %v", err)
	}
	if ok2 {
		t.Error("second import should be no-op (dedupe), got imported=true")
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE source='bgg' AND source_ref='bgg-play-12345'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected count 1 after dedupe, got %d", count)
	}
}

func TestImportBGGPlayMapsCorrectly(t *testing.T) {
	database := setupBGGTestDB(t)

	play := bggPlayXML{
		ID:       "123",
		Date:     "2026-06-15",
		Location: "Kitchen table",
		Item:     bggItemXML{Name: "Ticket to Ride", ObjectID: "999"},
		Players: &bggPlayersXML{
			Players: []bggPlayerXML{{Name: "Alice", Score: "42", Win: "1"}},
		},
		Comments: "Fun",
		Length:   "60",
	}

	ok, err := importBGGPlay(database, play)
	if err != nil || !ok {
		t.Fatalf("import failed: %v ok=%v", err, ok)
	}

	var title, tags, source, sourceRef, eventDate string
	if err := database.QueryRow(`SELECT title, tags, source, source_ref, event_date FROM timeline_events WHERE source_ref='bgg-play-123'`).Scan(&title, &tags, &source, &sourceRef, &eventDate); err != nil {
		t.Fatal(err)
	}
	if title != "Played Ticket to Ride" {
		t.Errorf("title = %q, want %q", title, "Played Ticket to Ride")
	}
	if !strings.Contains(tags, "boardgame") {
		t.Errorf("tags = %q, want to contain boardgame", tags)
	}
	if source != "bgg" {
		t.Errorf("source = %q, want bgg", source)
	}
	if sourceRef != "bgg-play-123" {
		t.Errorf("source_ref = %q, want bgg-play-123", sourceRef)
	}
	if eventDate != "2026-06-15" {
		t.Errorf("event_date = %q, want 2026-06-15", eventDate)
	}
}

func TestBGGPlaysExcludedFromPublicFeed(t *testing.T) {
	database := setupBGGTestDB(t)

	// Normal event
	if _, err := database.Exec(`INSERT INTO timeline_events (title, event_date, source, source_ref, tags) VALUES ('Normal Event','2026-06-01','','','')`); err != nil {
		t.Fatal(err)
	}
	// BGG event via import
	play := sampleBGGPlay("12345", "2026-06-02", "Catan")
	if _, err := importBGGPlay(database, play); err != nil {
		t.Fatal(err)
	}

	// BuildEventQuery should exclude BGG events
	q, args := BuildEventQuery(EventFilters{})
	if !strings.Contains(q, "source") {
		t.Fatalf("BuildEventQuery should contain BGG exclusion, got %q", q)
	}
	rows, err := database.Query(q, args...)
	if err != nil {
		t.Fatalf("BuildEventQuery query failed: %v", err)
	}
	defer rows.Close()
	var count int
	var titles []string
	for rows.Next() {
		var id int
		var title, desc, date, loc, mt sql.NullString
		var mu, thumb, cap, tags sql.NullString
		var so sql.NullInt64
		var isPub, isFav sql.NullBool
		var created sql.NullString
		var pid sql.NullInt64
		var lat, lng sql.NullFloat64
		var rec, wd sql.NullString
		var uid sql.NullInt64
		var st, et sql.NullString
		var pID sql.NullInt64
		var pName, pAv, pBio, pBirth, pColor, pCreated sql.NullString
		if err := rows.Scan(&id, &title, &desc, &date, &loc, &mt, &mu, &thumb, &cap, &tags, &so, &isPub, &isFav, &created, &pid, &lat, &lng, &rec, &wd, &uid, &st, &et, &pID, &pName, &pAv, &pBio, &pBirth, &pColor, &pCreated); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		count++
		titles = append(titles, title.String)
	}
	if count != 1 {
		t.Fatalf("expected 1 event from BuildEventQuery (BGG excluded), got %d titles=%v", count, titles)
	}
	if titles[0] != "Normal Event" {
		t.Errorf("got title %q, want Normal Event", titles[0])
	}

	// Corner query: direct BGG query should return the BGG event
	rows2, err := database.Query(`SELECT id FROM timeline_events WHERE source='bgg' AND (deleted_at IS NULL OR deleted_at='')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	var bggCount int
	for rows2.Next() {
		bggCount++
	}
	if bggCount != 1 {
		t.Errorf("expected 1 BGG event via corner query, got %d", bggCount)
	}

	// Also verify getBGGEvents endpoint returns BGG event
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/bgg/events", getBGGEvents)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/bgg/events", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("getBGGEvents status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Events []struct {
			Title string `json:"title"`
		} `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 1 || resp.Events[0].Title != "Played Catan" {
		t.Errorf("getBGGEvents returned %v, want 1x Played Catan", resp.Events)
	}
}

func TestBGGExcludedFromStats(t *testing.T) {
	database := setupBGGTestDB(t)

	// Normal event in 2026
	if _, err := database.Exec(`INSERT INTO timeline_events (title, event_date, source, source_ref) VALUES ('Normal','2026-07-01','','')`); err != nil {
		t.Fatal(err)
	}
	// BGG event in same year
	play := sampleBGGPlay("999", "2026-07-02", "Azul")
	if _, err := importBGGPlay(database, play); err != nil {
		t.Fatal(err)
	}

	stats := QueryYearStats(database, "2026")
	if stats.Total != 1 {
		t.Errorf("QueryYearStats Total = %d, want 1 (BGG excluded)", stats.Total)
	}
}
