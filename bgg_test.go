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

	newTestDB(t)

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

	return db
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

	var title, tags, source, sourceRef, eventDate, mediaType string
	if err := database.QueryRow(`SELECT title, tags, source, source_ref, event_date, media_type FROM timeline_events WHERE source_ref='bgg-play-123'`).Scan(&title, &tags, &source, &sourceRef, &eventDate, &mediaType); err != nil {
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
	if mediaType != "boardgame" {
		t.Errorf("media_type = %q, want boardgame", mediaType)
	}
}

func TestBGGPlaysIncludedInPublicFeed(t *testing.T) {
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

	// BuildEventQuery should now include BGG events
	q, args := BuildEventQuery(EventFilters{})
	if strings.Contains(q, "source = ''") {
		t.Fatalf("BuildEventQuery should not contain BGG exclusion, got %q", q)
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
	if count != 2 {
		t.Fatalf("expected 2 events from BuildEventQuery (BGG included), got %d titles=%v", count, titles)
	}
	foundNormal, foundBGG := false, false
	for _, tt := range titles {
		if tt == "Normal Event" {
			foundNormal = true
		}
		if tt == "Played Catan" {
			foundBGG = true
		}
	}
	if !foundNormal || !foundBGG {
		t.Errorf("expected both Normal Event and Played Catan in feed, got %v", titles)
	}

	// Verify BGG row has media_type=boardgame
	var mediaType string
	if err := database.QueryRow(`SELECT media_type FROM timeline_events WHERE source='bgg'`).Scan(&mediaType); err != nil {
		t.Fatal(err)
	}
	if mediaType != "boardgame" {
		t.Errorf("BGG media_type = %q, want boardgame", mediaType)
	}

	// Verify via HTTP: /api/events and /api/events/full now include BGG play
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/events", func(c *gin.Context) {
		// mimic real handler: use BuildEventQuery then ScanEvents
		q2, args2 := BuildEventQuery(EventFilters{})
		rows2, err := database.Query(q2, args2...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows2.Close()
		events := ScanEvents(rows2)
		c.JSON(http.StatusOK, events)
	})
	router.GET("/api/events/full", func(c *gin.Context) {
		q2, args2 := BuildEventQuery(EventFilters{})
		rows2, err := database.Query(q2, args2...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows2.Close()
		events := ScanEvents(rows2)
		c.JSON(http.StatusOK, events)
	})
	for _, path := range []string{"/api/events", "/api/events/full"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 body=%s", path, w.Code, w.Body.String())
		}
		var resp []struct {
			Title     string `json:"title"`
			MediaType string `json:"media_type"`
			Source    string `json:"source"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ev := range resp {
			if ev.Title == "Played Catan" {
				found = true
				if ev.MediaType != "boardgame" {
					t.Errorf("%s: Played Catan media_type = %q, want boardgame", path, ev.MediaType)
				}
			}
		}
		if !found {
			t.Errorf("%s should contain Played Catan, got %v", path, resp)
		}
	}
}

func TestBGGIncludedInStats(t *testing.T) {
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
	if stats.Total != 2 {
		t.Errorf("QueryYearStats Total = %d, want 2 (BGG included)", stats.Total)
	}
	if stats.ByMedia["boardgame"] < 1 {
		t.Errorf("ByMedia[boardgame] = %d, want >=1 stats=%v", stats.ByMedia["boardgame"], stats.ByMedia)
	}
}
