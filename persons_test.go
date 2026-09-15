package main

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestPersonCRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	t.Run("create_person", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO persons (name, avatar_url, bio, color) VALUES (?, ?, ?, ?)",
			"John Doe", "/media/avatar.jpg", "A test person", "#ff0000")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM persons").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 person, got %d", count)
		}
	})

	t.Run("fetch_person", func(t *testing.T) {
		var name, avatar, bio, color string
		err := db.QueryRow("SELECT name, avatar_url, bio, color FROM persons WHERE id = 1").Scan(&name, &avatar, &bio, &color)
		if err != nil {
			t.Fatal(err)
		}
		if name != "John Doe" {
			t.Errorf("name = %q, want John Doe", name)
		}
		if avatar != "/media/avatar.jpg" {
			t.Errorf("avatar = %q", avatar)
		}
		if color != "#ff0000" {
			t.Errorf("color = %q", color)
		}
	})

	t.Run("update_person", func(t *testing.T) {
		_, err := db.Exec("UPDATE persons SET name=?, color=? WHERE id=?", "Jane Doe", "#00ff00", 1)
		if err != nil {
			t.Fatal(err)
		}
		var name, color string
		db.QueryRow("SELECT name, color FROM persons WHERE id = 1").Scan(&name, &color)
		if name != "Jane Doe" {
			t.Errorf("name = %q, want Jane Doe", name)
		}
		if color != "#00ff00" {
			t.Errorf("color = %q, want #00ff00", color)
		}
	})

	t.Run("delete_person", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM persons WHERE id = 1")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM persons").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 persons, got %d", count)
		}
	})
}

func TestTagAutocomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		tags TEXT
	)`)

	db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('Event 1', 'nature, photography')")
	db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('Event 2', 'nature, hiking')")
	db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('Event 3', 'food, cooking')")

	t.Run("distinct_tags", func(t *testing.T) {
		rows, err := db.Query("SELECT DISTINCT tags FROM timeline_events WHERE tags != ''")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var rawTags []string
		for rows.Next() {
			var t string
			rows.Scan(&t)
			rawTags = append(rawTags, t)
		}
		// Flatten comma-separated tags
		tagSet := make(map[string]bool)
		for _, rt := range rawTags {
			for tag := range strings.SplitSeq(rt, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagSet[tag] = true
				}
			}
		}
		expected := []string{"nature", "photography", "hiking", "food", "cooking"}
		for _, e := range expected {
			if !tagSet[e] {
				t.Errorf("missing tag: %s", e)
			}
		}
		if len(tagSet) != len(expected) {
			t.Errorf("expected %d unique tags, got %d", len(expected), len(tagSet))
		}
	})

	t.Run("tag_search", func(t *testing.T) {
		rows, err := db.Query("SELECT DISTINCT tags FROM timeline_events WHERE tags LIKE ?", "%nature%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 2 {
			t.Errorf("expected 2 tag rows matching 'nature', got %d", count)
		}
	})
}

func TestPersonSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		person_id INTEGER
	)`)

	db.Exec("INSERT INTO persons (name, color) VALUES ('Alice Johnson', '#ff0000')")
	db.Exec("INSERT INTO persons (name, color) VALUES ('Bob Smith', '#00ff00')")
	db.Exec("INSERT INTO persons (name, color) VALUES ('Charlie Brown', '#0000ff')")

	t.Run("search_by_name_returns_matching_persons", func(t *testing.T) {
		rows, err := db.Query("SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at, (SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count FROM persons p WHERE p.name LIKE ? ORDER BY p.name ASC", "%Alice%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		var name string
		for rows.Next() {
			var id int
			var avatarURL, bio, birthDate, color, createdAt sql.NullString
			var eventCount int
			if err := rows.Scan(&id, &name, &avatarURL, &bio, &birthDate, &color, &createdAt, &eventCount); err != nil {
				t.Fatal(err)
			}
			count++
		}
		if count != 1 {
			t.Errorf("expected 1 person matching 'Alice', got %d", count)
		}
		if name != "Alice Johnson" {
			t.Errorf("name = %q, want 'Alice Johnson'", name)
		}
	})

	t.Run("empty_query_returns_all_persons", func(t *testing.T) {
		rows, err := db.Query("SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at, (SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count FROM persons p ORDER BY p.name ASC")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var count int
		for rows.Next() {
			count++
		}
		if count != 3 {
			t.Errorf("expected 3 persons with empty query, got %d", count)
		}
	})

	t.Run("partial_match_search", func(t *testing.T) {
		rows, err := db.Query("SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at, (SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count FROM persons p WHERE p.name LIKE ? ORDER BY p.name ASC", "%ob%")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var id int
			var name string
			var avatarURL, bio, birthDate, color, createdAt sql.NullString
			var eventCount int
			if err := rows.Scan(&id, &name, &avatarURL, &bio, &birthDate, &color, &createdAt, &eventCount); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		if len(names) != 1 || names[0] != "Bob Smith" {
			t.Errorf("expected ['Bob Smith'], got %v", names)
		}
	})
}

func TestTagManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		tags TEXT
	)`)

	t.Run("rename_tag", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E1', 'nature, photography')")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E2', 'nature, hiking')")

		db.Exec("UPDATE timeline_events SET tags = REPLACE(tags, 'nature', 'outdoor') WHERE tags LIKE '%nature%'")

		var tags1, tags2 string
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E1'").Scan(&tags1)
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E2'").Scan(&tags2)
		if !strings.Contains(tags1, "outdoor") {
			t.Errorf("expected 'outdoor' in E1, got %q", tags1)
		}
		if strings.Contains(tags1, "nature") {
			t.Errorf("unexpected 'nature' in E1, got %q", tags1)
		}
		if !strings.Contains(tags2, "outdoor") {
			t.Errorf("expected 'outdoor' in E2, got %q", tags2)
		}
	})

	t.Run("delete_tag", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E1', 'food, cooking')")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E2', 'nature, hiking')")

		var id int
		var tags string
		err := db.QueryRow("SELECT id, tags FROM timeline_events WHERE tags LIKE '%cooking%'").Scan(&id, &tags)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(tags, ",")
		var newParts []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && p != "cooking" {
				newParts = append(newParts, p)
			}
		}
		_, err = db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", strings.Join(newParts, ", "), id)
		if err != nil {
			t.Fatal(err)
		}

		var tagsAfter string
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E1'").Scan(&tagsAfter)
		if strings.Contains(tagsAfter, "cooking") {
			t.Errorf("expected 'cooking' removed, got %q", tagsAfter)
		}
		if !strings.Contains(tagsAfter, "food") {
			t.Errorf("expected 'food' to remain, got %q", tagsAfter)
		}
	})

	t.Run("merge_tags", func(t *testing.T) {
		db.Exec("DELETE FROM timeline_events")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E1', 'outdoor, photography')")
		db.Exec("INSERT INTO timeline_events (title, tags) VALUES ('E2', 'outdoor, hiking')")

		var id int
		var tags string
		err := db.QueryRow("SELECT id, tags FROM timeline_events WHERE title = 'E1'").Scan(&id, &tags)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(tags, ",")
		var newParts []string
		seen := make(map[string]bool)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if p == "outdoor" {
				p = "outside"
			}
			if !seen[p] {
				newParts = append(newParts, p)
				seen[p] = true
			}
		}
		_, err = db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", strings.Join(newParts, ", "), id)
		if err != nil {
			t.Fatal(err)
		}

		var tags1 string
		db.QueryRow("SELECT tags FROM timeline_events WHERE title = 'E1'").Scan(&tags1)
		if !strings.Contains(tags1, "outside") {
			t.Errorf("expected 'outside' in E1 after merge, got %q", tags1)
		}
		if strings.Contains(tags1, "outdoor") {
			t.Errorf("unexpected 'outdoor' in E1, got %q", tags1)
		}
	})
}
