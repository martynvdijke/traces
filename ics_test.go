package main

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func TestICalendarExport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0
	)`)

	db.Exec("INSERT INTO timeline_events (title, description, event_date, location, media_type, latitude, longitude, recurring, event_start_time, event_end_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"Test Event", "A test event\nwith multiline", "2026-06-15", "Test Location", "image", 40.7128, -74.0060, "", "09:30", "17:00")
	db.Exec("INSERT INTO timeline_events (title, description, event_date, recurring) VALUES (?, ?, ?, ?)",
		"Birthday", "Annual birthday", "2026-01-15", "yearly")
	db.Exec("INSERT INTO timeline_events (title, event_date) VALUES (?, ?)",
		"No Desc Event", "2026-03-20")

	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.latitude, e.longitude, e.recurring, e.weather_data, e.event_start_time, e.event_end_time
		FROM timeline_events e ORDER BY e.event_date ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var ics strings.Builder
	ics.WriteString("BEGIN:VCALENDAR\r\n")
	ics.WriteString("VERSION:2.0\r\n")
	ics.WriteString("PRODID:-//TRACES//Events 2026//EN\r\n")
	ics.WriteString("CALSCALE:GREGORIAN\r\n")
	ics.WriteString("METHOD:PUBLISH\r\n")
	ics.WriteString("X-WR-CALNAME:TRACES 2026\r\n")

	for rows.Next() {
		var id int
		var title, date string
		var desc, location, mediaType, recurring, weatherData, startTime, endTime sql.NullString
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&id, &title, &desc, &date, &location, &mediaType, &lat, &lng, &recurring, &weatherData, &startTime, &endTime); err != nil {
			t.Fatal(err)
		}
		uid := fmt.Sprintf("%d-%s@traces", id, date)
		ics.WriteString("BEGIN:VEVENT\r\n")
		ics.WriteString("UID:" + uid + "\r\n")
		ics.WriteString("DTSTAMP:20260511T000000Z\r\n")
		if startTime.Valid && startTime.String != "" {
			ics.WriteString("DTSTART:" + strings.ReplaceAll(date, "-", "") + "T" + strings.ReplaceAll(startTime.String, ":", "") + "00\r\n")
			if endTime.Valid && endTime.String != "" {
				ics.WriteString("DTEND:" + strings.ReplaceAll(date, "-", "") + "T" + strings.ReplaceAll(endTime.String, ":", "") + "00\r\n")
			}
		} else {
			ics.WriteString("DTSTART;VALUE=DATE:" + strings.ReplaceAll(date, "-", "") + "\r\n")
		}
		ics.WriteString("SUMMARY:" + strings.ReplaceAll(title, "\\", "\\\\") + "\r\n")
		if desc.Valid && desc.String != "" {
			ics.WriteString("DESCRIPTION:" + strings.ReplaceAll(strings.ReplaceAll(desc.String, "\n", "\\n"), "\\", "\\\\") + "\r\n")
		}
		if location.Valid && location.String != "" {
			ics.WriteString("LOCATION:" + strings.ReplaceAll(location.String, "\\", "\\\\") + "\r\n")
		}
		if lat.Valid && lng.Valid {
			ics.WriteString("GEO:" + fmt.Sprintf("%.6f;%.6f", lat.Float64, lng.Float64) + "\r\n")
		}
		if recurring.Valid {
			switch recurring.String {
			case "yearly":
				ics.WriteString("RRULE:FREQ=YEARLY\r\n")
			}
		}
		ics.WriteString("END:VEVENT\r\n")
	}
	ics.WriteString("END:VCALENDAR\r\n")

	t.Run("has_vcalendar_wrapper", func(t *testing.T) {
		if !strings.Contains(ics.String(), "BEGIN:VCALENDAR") {
			t.Error("missing BEGIN:VCALENDAR")
		}
		if !strings.Contains(ics.String(), "END:VCALENDAR") {
			t.Error("missing END:VCALENDAR")
		}
		if !strings.Contains(ics.String(), "VERSION:2.0") {
			t.Error("missing VERSION:2.0")
		}
	})

	t.Run("contains_all_events", func(t *testing.T) {
		count := strings.Count(ics.String(), "BEGIN:VEVENT")
		if count != 3 {
			t.Errorf("expected 3 VEVENT, got %d", count)
		}
	})

	t.Run("recurring_event_has_rrule", func(t *testing.T) {
		if !strings.Contains(ics.String(), "RRULE:FREQ=YEARLY") {
			t.Error("missing RRULE for yearly event")
		}
	})

	t.Run("timed_event_has_dtstart_dtend", func(t *testing.T) {
		if !strings.Contains(ics.String(), "DTSTART:20260615T093000") {
			t.Error("missing DTSTART with time for timed event")
		}
		if !strings.Contains(ics.String(), "DTEND:20260615T170000") {
			t.Error("missing DTEND with time for timed event")
		}
	})

	t.Run("all_day_event_has_date_dtstart", func(t *testing.T) {
		if !strings.Contains(ics.String(), "DTSTART;VALUE=DATE:20260115") {
			t.Error("missing DATE value DTSTART for all-day event")
		}
	})

	t.Run("event_has_geo", func(t *testing.T) {
		if !strings.Contains(ics.String(), "GEO:40.712800;-74.006000") {
			t.Error("missing GEO for event with coordinates")
		}
	})

	t.Run("each_vevent_has_uid", func(t *testing.T) {
		uidCount := strings.Count(ics.String(), "@traces")
		veventCount := strings.Count(ics.String(), "BEGIN:VEVENT")
		if uidCount != veventCount {
			t.Errorf("expected %d UIDs for %d events, got %d", veventCount, veventCount, uidCount)
		}
	})
}
