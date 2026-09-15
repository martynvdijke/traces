package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/models"
)

func TestEventCRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
		longitude REAL
	)`)

	t.Run("create_event", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, tags, latitude, longitude) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Test Event", "A test description", "2026-06-15", "Test Location", "image", "/media/test.jpg", "tag1, tag2", 40.7128, -74.0060)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event, got %d", count)
		}
	})

	t.Run("fetch_event", func(t *testing.T) {
		var title, description, date, location, mediaType, mediaURL, tags string
		var latitude, longitude float64
		err := db.QueryRow("SELECT title, description, event_date, location, media_type, media_url, tags, latitude, longitude FROM timeline_events WHERE id = 1").
			Scan(&title, &description, &date, &location, &mediaType, &mediaURL, &tags, &latitude, &longitude)
		if err != nil {
			t.Fatal(err)
		}
		if title != "Test Event" {
			t.Errorf("title = %q", title)
		}
		if date != "2026-06-15" {
			t.Errorf("date = %q", date)
		}
		if tags != "tag1, tag2" {
			t.Errorf("tags = %q", tags)
		}
		if latitude != 40.7128 {
			t.Errorf("latitude = %f", latitude)
		}
	})

	t.Run("query_by_year", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", "2026").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event in 2026, got %d", count)
		}
		var count2 int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", "2025").Scan(&count2)
		if count2 != 0 {
			t.Errorf("expected 0 events in 2025, got %d", count2)
		}
	})

	t.Run("query_by_tag_like", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE tags LIKE ?", "%tag1%").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event with tag1, got %d", count)
		}
	})

	t.Run("update_event", func(t *testing.T) {
		_, err := db.Exec("UPDATE timeline_events SET title=?, location=? WHERE id=?", "Updated Event", "New Location", 1)
		if err != nil {
			t.Fatal(err)
		}
		var title, location string
		db.QueryRow("SELECT title, location FROM timeline_events WHERE id = 1").Scan(&title, &location)
		if title != "Updated Event" || location != "New Location" {
			t.Errorf("got %q / %q", title, location)
		}
	})

	t.Run("delete_event", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM timeline_events WHERE id = 1")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 events, got %d", count)
		}
	})
}

func TestCalendarQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
		user_id INTEGER DEFAULT 0
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 1', '2026-06-01')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 2', '2026-06-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 3', '2026-06-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 4', '2026-07-01')")

	t.Run("calendar_month_query", func(t *testing.T) {
		rows, err := db.Query(`SELECT event_date FROM timeline_events WHERE event_date BETWEEN '2026-06-01' AND '2026-06-30' ORDER BY event_date ASC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		var dates []string
		for rows.Next() {
			var d string
			rows.Scan(&d)
			dates = append(dates, d)
		}

		if len(dates) != 3 {
			t.Errorf("expected 3 events in June, got %d", len(dates))
		}
	})

	t.Run("calendar_group_by_date", func(t *testing.T) {
		rows, err := db.Query(`SELECT event_date, COUNT(*) FROM timeline_events WHERE event_date BETWEEN '2026-06-01' AND '2026-06-30' GROUP BY event_date ORDER BY event_date`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		groups := make(map[string]int)
		for rows.Next() {
			var date string
			var count int
			rows.Scan(&date, &count)
			groups[date] = count
		}

		if groups["2026-06-15"] != 2 {
			t.Errorf("expected 2 events on 2026-06-15, got %d", groups["2026-06-15"])
		}
	})
}

func TestRecurringEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
		user_id INTEGER DEFAULT 0
	)`)

	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('Birthday', '2026-01-15', 'yearly')")
	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('Weekly Meetup', '2026-01-05', 'weekly')")
	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('Monthly Report', '2026-01-01', 'monthly')")
	db.Exec("INSERT INTO timeline_events (title, event_date, recurring) VALUES ('One-off', '2026-01-01', '')")

	t.Run("recurring_yearly", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE recurring = 'yearly'").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 yearly recurring event, got %d", count)
		}
	})

	t.Run("recurring_weekly", func(t *testing.T) {
		var title, recurring string
		db.QueryRow("SELECT title, recurring FROM timeline_events WHERE recurring = 'weekly'").Scan(&title, &recurring)
		if title != "Weekly Meetup" {
			t.Errorf("expected 'Weekly Meetup', got %q", title)
		}
	})

	t.Run("non_recurring", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE recurring = ''").Scan(&count)
		if count < 1 {
			t.Errorf("expected at least 1 non-recurring event")
		}
	})
}

func TestMarkdownInDescription(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT
	)`)

	t.Run("render_markdown_go", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			wantHTML []string
		}{
			{"bold", "**bold**", []string{"<strong>bold</strong>"}},
			{"italic", "*italic*", []string{"<em>italic</em>"}},
			{"heading", "## Heading", []string{"<h2>Heading</h2>"}},
			{"link", "[text](https://example.com)", []string{"<a href=\"https://example.com\"", "text</a>"}},
			{"list", "- item", []string{"<li>item</li>", "<ul>"}},
			{"blockquote", "> quote", []string{"<blockquote>", "quote"}},
			{"code", "`code`", []string{"<code>code</code>"}},
			{"empty", "", []string{}},
			{"xss", "<script>alert('xss')</script>", []string{}}, // goldmark strips raw HTML by default
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := models.RenderMarkdown(tt.input)
				for _, want := range tt.wantHTML {
					if !strings.Contains(got, want) {
						t.Errorf("models.RenderMarkdown(%q) = %q, want contains %q", tt.input, got, want)
					}
				}
			})
		}
	})

	t.Run("store_markdown_description", func(t *testing.T) {
		md := "## Heading\n\nThis is **bold** and *italic*.\n\n- List item 1\n- List item 2"
		_, err := db.Exec("INSERT INTO timeline_events (title, description, event_date) VALUES (?, ?, ?)", "MD Event", md, "2026-06-15")
		if err != nil {
			t.Fatal(err)
		}

		var description string
		db.QueryRow("SELECT description FROM timeline_events WHERE id = 1").Scan(&description)
		if description != md {
			t.Errorf("markdown content mismatch")
		}
		if !strings.Contains(description, "**bold**") {
			t.Error("markdown should preserve bold syntax")
		}
	})
}

func TestEventCreationWithWeatherData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT,
		weather_data TEXT,
		user_id INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	weatherJSON := `{"temperature":22.5,"condition":"Partly cloudy","icon":"cloud-sun","humidity":65,"wind_speed":12.3,"fetched_at":"2026-05-06T10:00:00Z"}`

	t.Run("create_event_with_weather_data", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, tags, latitude, longitude, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Weather Event", "Event with weather", "2026-06-15", "Test Location", "image", "/media/test.jpg", "weather", 40.7128, -74.0060, weatherJSON, 1)
		if err != nil {
			t.Fatal(err)
		}

		var storedWeather string
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 1").Scan(&storedWeather)
		if storedWeather != weatherJSON {
			t.Errorf("weather_data mismatch: got %q", storedWeather)
		}

		var parsed struct {
			Temperature float64 `json:"temperature"`
			Condition   string  `json:"condition"`
			Humidity    float64 `json:"humidity"`
		}
		if err := json.Unmarshal([]byte(storedWeather), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Temperature != 22.5 {
			t.Errorf("temperature = %f, want 22.5", parsed.Temperature)
		}
		if parsed.Condition != "Partly cloudy" {
			t.Errorf("condition = %q, want 'Partly cloudy'", parsed.Condition)
		}
	})

	t.Run("update_event_weather_data", func(t *testing.T) {
		newWeather := `{"temperature":18.0,"condition":"Rain","icon":"cloud-rain","humidity":80,"wind_speed":20.0,"fetched_at":"2026-05-06T12:00:00Z"}`
		_, err := db.Exec("UPDATE timeline_events SET weather_data = ? WHERE id = ?", newWeather, 1)
		if err != nil {
			t.Fatal(err)
		}

		var storedWeather string
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 1").Scan(&storedWeather)
		if storedWeather != newWeather {
			t.Errorf("weather_data mismatch after update")
		}
	})

	t.Run("create_event_without_weather_data", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, tags, latitude, longitude, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"No Weather Event", "Event without weather", "2026-07-01", "Another Location", "image", "/media/test2.jpg", "test", 51.5074, -0.1278, 1)
		if err != nil {
			t.Fatal(err)
		}

		var weatherData sql.NullString
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 2").Scan(&weatherData)
		if weatherData.Valid && weatherData.String != "" {
			t.Errorf("expected empty weather_data, got %q", weatherData.String)
		}
	})
}

func TestEventCreationWithoutThumbnail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT,
		weather_data TEXT,
		user_id INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	t.Run("insert_without_thumbnail_column", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, media_caption, tags, sort_order, is_public, person_id, latitude, longitude, recurring, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"No Thumbnail Event", "Testing without thumbnail", "2026-08-01", "Test Location", "image", "/media/test.jpg", "", "test", 0, 1, nil, 40.7128, -74.0060, "", "", 1)
		if err != nil {
			t.Fatalf("insert without thumbnail failed: %v", err)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 event, got %d", count)
		}
	})

	t.Run("update_without_thumbnail_column", func(t *testing.T) {
		_, err := db.Exec(`UPDATE timeline_events SET title=?, description=?, event_date=?, location=?, media_type=?, media_url=?, media_caption=?, tags=?, sort_order=?, is_public=?, person_id=?, latitude=?, longitude=?, recurring=?, weather_data=?, user_id=? WHERE id=?`,
			"Updated No Thumbnail", "Updated desc", "2026-08-02", "Updated Location", "video", "/media/test2.mp4", "", "updated", 1, 0, nil, 51.5074, -0.1278, "", "", 1, 1)
		if err != nil {
			t.Fatalf("update without thumbnail failed: %v", err)
		}

		var title string
		db.QueryRow("SELECT title FROM timeline_events WHERE id = 1").Scan(&title)
		if title != "Updated No Thumbnail" {
			t.Errorf("title = %q, want 'Updated No Thumbnail'", title)
		}
	})
}

func TestScanEventsWithPersonNullThumbnail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
		user_id INTEGER DEFAULT 0
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	t.Run("scan_null_thumbnail", func(t *testing.T) {
		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, media_caption, tags, sort_order, is_public)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Null Thumbnail Event", "Testing NULL thumbnail scan", "2026-03-15", "Test", "image", "/media/test.jpg", "", "test", 0, 1)

		rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE 1=1 ORDER BY e.event_date ASC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		events := scanEventsWithPerson(rows)
		if len(events) != 1 {
			t.Fatalf("expected 1 event from scanEventsWithPerson, got %d", len(events))
		}

		e := events[0]
		if e.Title != "Null Thumbnail Event" {
			t.Errorf("title = %q, want 'Null Thumbnail Event'", e.Title)
		}
		if e.Thumbnail != "" {
			t.Errorf("thumbnail = %q, want empty string for NULL", e.Thumbnail)
		}
		if e.Date != "2026-03-15" {
			t.Errorf("date = %q, want '2026-03-15'", e.Date)
		}
	})

	t.Run("scan_multiple_mixed_thumbnails", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")

		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, media_caption, tags, is_public)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"With Thumbnail", "Has thumbnail value", "2026-01-10", "Loc1", "image", "/media/a.jpg", "/thumb.jpg", "", "tag1", 1)
		db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, media_caption, tags, is_public)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"Null Thumbnail", "Has NULL thumbnail", "2026-06-20", "Loc2", "image", "/media/b.jpg", "", "tag2", 1)

		rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE 1=1 ORDER BY e.event_date ASC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		events := scanEventsWithPerson(rows)
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}

		titles := make(map[string]string)
		for _, e := range events {
			titles[e.Title] = e.Thumbnail
		}

		if v, ok := titles["With Thumbnail"]; !ok {
			t.Error("event 'With Thumbnail' not found")
		} else if v != "/thumb.jpg" {
			t.Errorf("thumbnail = %q, want '/thumb.jpg'", v)
		}

		if v, ok := titles["Null Thumbnail"]; !ok {
			t.Error("event 'Null Thumbnail' not found")
		} else if v != "" {
			t.Errorf("thumbnail = %q, want empty string", v)
		}
	})
}

func TestSaveAndGetEventsRoundtrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	t.Cleanup(func() {
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	})

	newTestDB(t)

	sessionStore = make(map[string]sessionInfo)
	csrfTokens = make(map[string]string)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	sessionID := "test-roundtrip-session"
	sessionStore[sessionID] = sessionInfo{userID: 0, expiresAt: time.Now().Add(24 * time.Hour).Unix()}
	csrfTokens[sessionID] = fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"-csrf")))

	router := gin.New()
	auth := router.Group("")
	auth.Use(func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil || sessionStore[cookie].expiresAt == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		c.Next()
	})
	auth.POST("/api/events", saveEvent)
	auth.GET("/api/events", getEvents)

	t.Run("create_and_find_event", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"title":"Roundtrip Event","description":"Roundtrip test","date":"2026-09-01","location":"Test","media_type":"image","is_public":true,"latitude":40.7128,"longitude":-74.0060}`
		req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST status = %d, body=%s", w.Code, w.Body.String())
		}

		var created models.TimelineEvent
		json.Unmarshal(w.Body.Bytes(), &created)
		if created.ID <= 0 {
			t.Fatalf("expected positive ID, got %d", created.ID)
		}
		if created.Title != "Roundtrip Event" {
			t.Errorf("title = %q", created.Title)
		}

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/api/events?year=2026&sort=desc&limit=10", nil)
		req2.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("GET status = %d, body=%s", w2.Code, w2.Body.String())
		}

		var events []models.TimelineEvent
		json.Unmarshal(w2.Body.Bytes(), &events)

		found := false
		for _, e := range events {
			if e.Title == "Roundtrip Event" {
				found = true
				if e.Date != "2026-09-01" {
					t.Errorf("date = %q, want '2026-09-01'", e.Date)
				}
				if e.Latitude == nil || *e.Latitude != 40.7128 {
					t.Errorf("latitude mismatch")
				}
				if e.Longitude == nil || *e.Longitude != -74.0060 {
					t.Errorf("longitude mismatch")
				}
				break
			}
		}
		if !found {
			t.Error("created event not found in events list")
		}
	})

	t.Run("update_and_refetch_event", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"id":1,"title":"Updated Roundtrip","description":"Updated desc","date":"2026-10-15","location":"Updated Loc","media_type":"video","is_public":false}`
		req := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("UPDATE status = %d, body=%s", w.Code, w.Body.String())
		}

		var updated models.TimelineEvent
		json.Unmarshal(w.Body.Bytes(), &updated)
		if updated.Title != "Updated Roundtrip" {
			t.Errorf("title = %q", updated.Title)
		}

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/api/events?year=2026&sort=desc&limit=10", nil)
		req2.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w2, req2)

		var events []models.TimelineEvent
		json.Unmarshal(w2.Body.Bytes(), &events)

		found := false
		for _, e := range events {
			if e.Title == "Updated Roundtrip" {
				found = true
				if e.Date != "2026-10-15" {
					t.Errorf("date = %q, want '2026-10-15'", e.Date)
				}
				if e.Location != "Updated Loc" {
					t.Errorf("location = %q", e.Location)
				}
				break
			}
		}
		if !found {
			t.Error("updated event not found in events list")
		}

		originalStillPresent := false
		for _, e := range events {
			if e.Title == "Roundtrip Event" {
				originalStillPresent = true
				break
			}
		}
		if originalStillPresent {
			t.Error("original event title still present after update")
		}
	})
}

func TestGetPublicEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origPublicMode := publicMode
	t.Cleanup(func() { publicMode = origPublicMode })

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, is_public) VALUES ('Public Jan Event', 'Jan description', '2026-01-15', 'Amsterdam', 'image', 1)`)
	db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, is_public) VALUES ('Public Jun Event', 'Jun description', '2026-06-10', 'London', 'video', 1)`)
	db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, is_public) VALUES ('Private Event', 'Private desc', '2026-03-20', 'Paris', 'image', 0)`)

	t.Run("default_year_filter", func(t *testing.T) {
		router := gin.New()
		router.GET("/api/public", getPublicEvents)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/public?year=2026", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}

		var events []models.TimelineEvent
		if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
			t.Fatalf("json unmarshal: %v, body=%s", err, w.Body.String())
		}

		// Should only return is_public = 1 events when publicMode is false
		if len(events) != 2 {
			t.Fatalf("expected 2 public events (non-publicMode), got %d: %+v", len(events), events)
		}
	})

	t.Run("month_filter", func(t *testing.T) {
		router := gin.New()
		router.GET("/api/public", getPublicEvents)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/public?year=2026&month=01", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}

		var events []models.TimelineEvent
		if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
			t.Fatalf("json unmarshal: %v, body=%s", err, w.Body.String())
		}

		if len(events) != 1 {
			t.Fatalf("expected 1 event in January, got %d", len(events))
		}
		if events[0].Title != "Public Jan Event" {
			t.Errorf("title = %q, want 'Public Jan Event'", events[0].Title)
		}
	})

	t.Run("public_mode_returns_all", func(t *testing.T) {
		publicMode = true
		defer func() { publicMode = false }()

		router := gin.New()
		router.GET("/api/public", getPublicEvents)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/public?year=2026", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}

		var events []models.TimelineEvent
		if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
			t.Fatalf("json unmarshal: %v, body=%s", err, w.Body.String())
		}

		if len(events) != 3 {
			t.Fatalf("expected 3 events in publicMode, got %d", len(events))
		}
	})
}

func TestEventDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
		user_id INTEGER DEFAULT 0
	)`)

	t.Run("create_with_times", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, event_date, event_start_time, event_end_time) VALUES (?, ?, ?, ?)`,
			"Timed Event", "2026-06-15", "09:30", "17:00")
		if err != nil {
			t.Fatal(err)
		}
		var startTime, endTime string
		err = db.QueryRow("SELECT event_start_time, event_end_time FROM timeline_events WHERE id = 1").Scan(&startTime, &endTime)
		if err != nil {
			t.Fatal(err)
		}
		if startTime != "09:30" {
			t.Errorf("start_time = %q, want 09:30", startTime)
		}
		if endTime != "17:00" {
			t.Errorf("end_time = %q, want 17:00", endTime)
		}
	})

	t.Run("create_without_times", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES (?, ?)`,
			"No Time Event", "2026-07-01")
		if err != nil {
			t.Fatal(err)
		}
		var startTime, endTime string
		err = db.QueryRow("SELECT event_start_time, event_end_time FROM timeline_events WHERE id = 2").Scan(&startTime, &endTime)
		if err != nil {
			t.Fatal(err)
		}
		if startTime != "" {
			t.Errorf("expected empty start_time, got %q", startTime)
		}
		if endTime != "" {
			t.Errorf("expected empty end_time, got %q", endTime)
		}
	})

	t.Run("update_times", func(t *testing.T) {
		_, err := db.Exec("UPDATE timeline_events SET event_start_time=?, event_end_time=? WHERE id=1", "10:00", "18:30")
		if err != nil {
			t.Fatal(err)
		}
		var startTime, endTime string
		err = db.QueryRow("SELECT event_start_time, event_end_time FROM timeline_events WHERE id = 1").Scan(&startTime, &endTime)
		if err != nil {
			t.Fatal(err)
		}
		if startTime != "10:00" {
			t.Errorf("start_time = %q, want 10:00", startTime)
		}
		if endTime != "18:30" {
			t.Errorf("end_time = %q, want 18:30", endTime)
		}
	})

	t.Run("query_with_times", func(t *testing.T) {
		rows, err := db.Query(`SELECT id, title, event_date, event_start_time, event_end_time FROM timeline_events WHERE event_start_time != '' ORDER BY event_date`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
			var id int
			var title, date, start, end string
			rows.Scan(&id, &title, &date, &start, &end)
			if start == "" {
				t.Errorf("event %d should have start_time", id)
			}
		}
		if count != 1 {
			t.Errorf("expected 1 event with start_time, got %d", count)
		}
	})
}
