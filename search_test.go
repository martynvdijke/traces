package main

import (
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestFTSSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		tags TEXT
	)`)

	createFTS5Table()

	var ftsAvailable bool
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events_fts'").Scan(&ftsAvailable)

	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Beach Day', 'Went swimming at the beach', '2026-07-15', 'Malibu, CA', 'beach, summer')")
	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Mountain Hike', 'Hiked through Yosemite', '2026-08-02', 'Yosemite, CA', 'hiking, nature')")
	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Concert Night', 'Live music at the park', '2026-06-20', 'Central Park', 'music, summer')")

	t.Run("fts_search_by_title", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("Beach"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var title string
			rows.Scan(&title)
			titles = append(titles, title)
		}
		if len(titles) != 1 {
			t.Errorf("expected 1 result for 'Beach', got %d: %v", len(titles), titles)
		}
	})

	t.Run("fts_search_by_location", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("Malibu"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'Malibu', got %d", count)
		}
	})

	t.Run("fts_search_by_tag", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("hiking"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'hiking', got %d", count)
		}
	})

	t.Run("fts_multi_match", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("summer"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("expected 2 results for 'summer', got %d", count)
		}
	})

	t.Run("fts_insert_trigger", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("INSERT INTO timeline_events (title, description, event_date, location, tags) VALUES ('Ski Trip', 'Skiing in the Alps', '2026-01-15', 'Alps', 'skiing, winter')")
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("Skiing"))
		if err != nil {
			t.Fatalf("FTS trigger query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'Skiing' after insert, got %d", count)
		}
	})

	t.Run("fts_delete_trigger", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("DELETE FROM timeline_events WHERE title = 'Ski Trip'")
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("Skiing"))
		if err != nil {
			t.Fatalf("FTS delete query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("expected 0 results for 'Skiing' after delete, got %d", count)
		}
	})

	t.Run("fts_update_trigger", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("UPDATE timeline_events SET description = 'Live jazz concert in the park' WHERE title = 'Concert Night'")
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("jazz"))
		if err != nil {
			t.Fatalf("FTS update query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 result for 'jazz' after update, got %d", count)
		}
	})

	t.Run("fts_no_match", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query("SELECT title FROM timeline_events WHERE id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", SanitizeFTSQuery("zzzznotfound"))
		if err != nil {
			t.Fatalf("FTS query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("expected 0 results for 'zzzznotfound', got %d", count)
		}
	})

	t.Run("sanitize_fts_query", func(t *testing.T) {
		cases := []struct {
			input    string
			expected string
		}{
			{"hello", `"hello"`},
			{"it's", `"it''s"`},
		}
		for _, tc := range cases {
			result := SanitizeFTSQuery(tc.input)
			if result != tc.expected {
				t.Errorf("SanitizeFTSQuery(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		}
	})
}

func TestGlobalSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		tags TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)

	createFTS5Table()

	var ftsAvailable bool
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events_fts'").Scan(&ftsAvailable)

	db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Christmas Party', '2025-12-25', 'Yearly holiday event')")
	db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('New Year Party', '2026-01-01', 'New year celebration')")
	db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Summer BBQ', '2026-07-04', 'Fourth of July cookout')")

	t.Run("global_search_returns_all_years", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		rows, err := db.Query(`SELECT e.title, e.event_date FROM timeline_events e
			WHERE e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)
			ORDER BY e.event_date DESC LIMIT 10`, SanitizeFTSQuery("Party"))
		if err != nil {
			t.Fatalf("Global search query failed: %v", err)
		}
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var title, date string
			rows.Scan(&title, &date)
			titles = append(titles, title)
		}
		if len(titles) != 2 {
			t.Errorf("expected 2 'Party' matches, got %d: %v", len(titles), titles)
		}
	})

	t.Run("global_search_like_fallback", func(t *testing.T) {
		query := "BBQ"
		like := "%" + query + "%"
		rows, err := db.Query(`SELECT e.title FROM timeline_events e
			WHERE 1=1 AND (e.title LIKE ? OR e.description LIKE ? OR e.location LIKE ?)
			ORDER BY e.event_date DESC LIMIT 10`, like, like, like)
		if err != nil {
			t.Fatalf("LIKE fallback query failed: %v", err)
		}
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var title string
			rows.Scan(&title)
			titles = append(titles, title)
		}
		if len(titles) != 1 {
			t.Errorf("expected 1 result for 'BBQ' via LIKE, got %d: %v", len(titles), titles)
		}
	})

	t.Run("global_search_limit", func(t *testing.T) {
		if !ftsAvailable {
			t.Skip("FTS5 not available")
		}
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party One', '2026-01-01', '')")
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party Two', '2026-01-02', '')")
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party Three', '2026-01-03', '')")
		db.Exec("INSERT INTO timeline_events (title, event_date, description) VALUES ('Party Four', '2026-01-04', '')")

		rows, err := db.Query(`SELECT e.title FROM timeline_events e
			WHERE e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)
			ORDER BY e.event_date DESC LIMIT 3`, SanitizeFTSQuery("Party"))
		if err != nil {
			t.Fatalf("Limit query failed: %v", err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 3 {
			t.Errorf("expected exactly 3 results with LIMIT, got %d", count)
		}
	})

	t.Run("global_search_empty_query", func(t *testing.T) {
		rows, err := db.Query(`SELECT e.title FROM timeline_events e
			WHERE e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)`,
			SanitizeFTSQuery(""))
		if err == nil {
			rows.Close()
		}
	})
}

func TestSearchEventsCombinedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		tags TEXT,
		person_id INTEGER,
		latitude REAL,
		longitude REAL
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)
	db.Exec("INSERT INTO persons (id, name) VALUES (1, 'Alice')")
	db.Exec("INSERT INTO persons (id, name) VALUES (2, 'Bob')")

	createFTS5Table()

	db.Exec("INSERT INTO timeline_events (title, event_date, location, media_type, tags, person_id) VALUES ('Beach Party', '2026-07-15', 'Miami', 'image', 'beach, summer', 1)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, media_type, tags, person_id) VALUES ('Mountain Trip', '2026-07-20', 'Denver', 'video', 'hiking, summer', 2)")
	db.Exec("INSERT INTO timeline_events (title, event_date, location, media_type, tags, person_id) VALUES ('Museum Visit', '2026-08-01', 'NYC', 'image', 'art, culture', 1)")

	t.Run("filter_by_tag", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND e.tags LIKE ? ORDER BY e.event_date ASC`, "%beach%")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 1 {
			t.Errorf("tag filter expected 1, got %d", count)
		}
	})

	t.Run("filter_by_person_id", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND e.person_id = ? ORDER BY e.event_date ASC`, "1")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("person_id filter expected 2, got %d", count)
		}
	})

	t.Run("combined_filters", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND strftime('%Y', e.event_date) = ? AND e.media_type = ? AND e.person_id = ? ORDER BY e.event_date ASC`, "2026", "image", "1")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("combined filter expected 2, got %d", count)
		}
	})

	t.Run("combined_filters_no_match", func(t *testing.T) {
		rows, _ := db.Query(`SELECT title FROM timeline_events e WHERE 1=1 AND e.person_id = ? AND e.media_type = ? ORDER BY e.event_date ASC`, "1", "video")
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("combined no-match expected 0, got %d", count)
		}
	})
}
