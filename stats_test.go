package main

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestContributions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		event_date TEXT
	)`)

	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 1', '2026-01-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 2', '2026-01-15')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 3', '2026-03-20')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 4', '2026-03-20')")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES ('Event 5', '2026-07-04')")

	t.Run("contributions_by_year", func(t *testing.T) {
		rows, err := db.Query("SELECT event_date FROM timeline_events WHERE strftime('%Y', event_date) = ?", "2026")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		contributions := make(map[string]int)
		for rows.Next() {
			var date string
			if err := rows.Scan(&date); err == nil {
				contributions[date]++
			}
		}

		if contributions["2026-01-15"] != 2 {
			t.Errorf("expected 2 events on 2026-01-15, got %d", contributions["2026-01-15"])
		}
		if contributions["2026-07-04"] != 1 {
			t.Errorf("expected 1 event on 2026-07-04, got %d", contributions["2026-07-04"])
		}
		if len(contributions) != 3 {
			t.Errorf("expected 3 unique dates, got %d", len(contributions))
		}
	})
}

func TestStatsDistribution(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		event_date TEXT,
		location TEXT,
		tags TEXT,
		media_type TEXT,
		person_id INTEGER,
		user_id INTEGER,
		latitude REAL,
		longitude REAL
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		display_name TEXT
	)`)
	db.Exec("INSERT OR IGNORE INTO users (id, display_name) VALUES (1, 'Alice')")
	db.Exec("INSERT OR IGNORE INTO users (id, display_name) VALUES (2, 'Bob')")
	db.Exec("INSERT OR IGNORE INTO persons (id, name) VALUES (1, 'Charlie')")
	db.Exec("INSERT OR IGNORE INTO persons (id, name) VALUES (2, 'Diana')")

	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E1', '2026-01-15', 'NYC', 'work, travel', 'image', 1, 1, 40.7128, -74.0060)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E2', '2026-03-20', 'LA', 'fun, travel', 'video', 1, 2, 34.0522, -118.2437)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E3', '2026-03-20', 'SF', 'work', 'image', 2, 1, 37.7749, -122.4194)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, tags, media_type, person_id, user_id, latitude, longitude) VALUES ('E4', '2026-07-04', 'Chicago', 'fun, holiday', 'image', 2, 2, 41.8781, -87.6298)")

	t.Run("monthly_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events
			WHERE strftime('%Y', event_date) = '2026' GROUP BY strftime('%m', event_date)`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		monthCounts := make(map[string]int)
		for rows.Next() {
			var m string
			var c int
			rows.Scan(&m, &c)
			monthCounts[m] = c
		}
		if monthCounts["01"] != 1 {
			t.Errorf("Jan has %d events, want 1", monthCounts["01"])
		}
		if monthCounts["03"] != 2 {
			t.Errorf("Mar has %d events, want 2", monthCounts["03"])
		}
		if monthCounts["07"] != 1 {
			t.Errorf("Jul has %d events, want 1", monthCounts["07"])
		}
	})

	t.Run("tag_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT tags FROM timeline_events WHERE strftime('%Y', event_date) = '2026' AND tags != ''`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		tagMap := make(map[string]int)
		for rows.Next() {
			var t string
			rows.Scan(&t)
			for tag := range strings.SplitSeq(t, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagMap[tag]++
				}
			}
		}
		if tagMap["work"] != 2 {
			t.Errorf("tag 'work' = %d, want 2", tagMap["work"])
		}
		if tagMap["travel"] != 2 {
			t.Errorf("tag 'travel' = %d, want 2", tagMap["travel"])
		}
		if tagMap["fun"] != 2 {
			t.Errorf("tag 'fun' = %d, want 2", tagMap["fun"])
		}
	})

	t.Run("person_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT p.id, p.name, COUNT(e.id) FROM persons p
			LEFT JOIN timeline_events e ON e.person_id = p.id AND strftime('%Y', e.event_date) = '2026'
			GROUP BY p.id HAVING COUNT(e.id) > 0 ORDER BY COUNT(e.id) DESC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var results []struct {
			id    int
			name  string
			count int
		}
		for rows.Next() {
			var r struct {
				id    int
				name  string
				count int
			}
			rows.Scan(&r.id, &r.name, &r.count)
			results = append(results, r)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 persons, got %d", len(results))
		}
	})

	t.Run("user_distribution", func(t *testing.T) {
		rows, err := db.Query(`SELECT u.id, u.display_name, COUNT(e.id) FROM users u
			LEFT JOIN timeline_events e ON e.user_id = u.id AND strftime('%Y', e.event_date) = '2026'
			GROUP BY u.id HAVING COUNT(e.id) > 0 ORDER BY COUNT(e.id) DESC`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var results []struct {
			id    int
			name  string
			count int
		}
		for rows.Next() {
			var r struct {
				id    int
				name  string
				count int
			}
			rows.Scan(&r.id, &r.name, &r.count)
			results = append(results, r)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 users, got %d", len(results))
		}
	})

	t.Run("haversine_distance", func(t *testing.T) {
		nyLat, nyLng := 40.7128, -74.0060
		laLat, laLng := 34.0522, -118.2437
		dist := haversine(nyLat, nyLng, laLat, laLng)
		if dist < 3000 || dist > 5000 {
			t.Errorf("NYC to LA distance = %.0f km, expected ~3940 km", dist)
		}
	})

	t.Run("is_leap_year", func(t *testing.T) {
		if !isLeapYear("2024") {
			t.Error("2024 should be a leap year")
		}
		if isLeapYear("2023") {
			t.Error("2023 should NOT be a leap year")
		}
		if !isLeapYear("2000") {
			t.Error("2000 should be a leap year")
		}
		if isLeapYear("1900") {
			t.Error("1900 should NOT be a leap year")
		}
	})

	t.Run("event_count", func(t *testing.T) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = '2026'").Scan(&count)
		if count != 4 {
			t.Errorf("event count = %d, want 4", count)
		}
	})

	t.Run("top_day", func(t *testing.T) {
		var topDay string
		var topCount int
		db.QueryRow("SELECT event_date, COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = '2026' GROUP BY event_date ORDER BY COUNT(*) DESC LIMIT 1").Scan(&topDay, &topCount)
		if topDay != "2026-03-20" || topCount != 2 {
			t.Errorf("top day = %s (%d), want '2026-03-20' (2)", topDay, topCount)
		}
	})

	t.Run("media_breakdown", func(t *testing.T) {
		rows, err := db.Query("SELECT media_type, COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = '2026' AND media_type != '' GROUP BY media_type")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		media := make(map[string]int)
		for rows.Next() {
			var mt string
			var c int
			rows.Scan(&mt, &c)
			media[mt] = c
		}
		if media["image"] != 3 {
			t.Errorf("images = %d, want 3", media["image"])
		}
		if media["video"] != 1 {
			t.Errorf("videos = %d, want 1", media["video"])
		}
	})
}
