package main

import (
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/models"
)

// ---- Filter types ----

// EventFilters holds optional WHERE clause conditions for the event query builder.
type EventFilters struct {
	Year      string
	Month     string
	Tag       string
	Person    string
	PersonID  string
	MediaType string
	Location  string
	UserID    string
	Query     string
	Sort      string
	Limit     int
	Skip      int
	IDs       []int // specific event IDs for share/public queries
	UseFTS    bool  // use full-text search when Query is set
}

// ---- Event query builder ----

// eventSelectColumns returns the static SELECT / JOIN portion for events.
func eventSelectColumns() string {
	return `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '')`
}

// BuildEventQuery builds a complete event SELECT query + args from filters.
// Returns the SQL string and argument slice.
func BuildEventQuery(filters EventFilters) (string, []any) {
	if len(filters.IDs) > 0 {
		placeholders := make([]string, len(filters.IDs))
		args := make([]any, len(filters.IDs))
		for i, id := range filters.IDs {
			placeholders[i] = "?"
			args[i] = id
		}
		sqlStr := eventSelectColumns() + " AND e.id IN (" + strings.Join(placeholders, ",") + ")"
		sqlStr += buildEventOrder(filters.Sort)
		return sqlStr, args
	}

	query := eventSelectColumns()
	args := []any{}
	query, args = appendEventFilters(query, args, filters)
	query += buildEventOrder(filters.Sort)
	if filters.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filters.Limit)
	}
	if filters.Skip > 0 {
		query += " OFFSET ?"
		args = append(args, filters.Skip)
	}
	return query, args
}

// BuildEventQueryPrefix builds SELECT with 1=1 for use with dynamic WHERE from FTS.
func BuildEventQueryPrefix() string {
	return `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE 1=1`
}

// appendEventFilters adds WHERE clauses for each non-empty filter.
func appendEventFilters(query string, args []any, f EventFilters) (string, []any) {
	if f.Year != "" {
		query += " AND strftime('%Y', e.event_date) = ?"
		args = append(args, f.Year)
	}
	if f.Month != "" {
		query += " AND strftime('%m', e.event_date) = ?"
		args = append(args, f.Month)
	}
	if f.Tag != "" {
		query += " AND e.tags LIKE ?"
		args = append(args, "%"+f.Tag+"%")
	}
	if f.Person != "" {
		query += " AND p.name LIKE ?"
		args = append(args, "%"+f.Person+"%")
	}
	if f.PersonID != "" {
		query += " AND e.person_id = ?"
		args = append(args, f.PersonID)
	}
	if f.MediaType != "" {
		query += " AND e.media_type = ?"
		args = append(args, f.MediaType)
	}
	if f.Location != "" {
		query += " AND e.location LIKE ?"
		args = append(args, "%"+f.Location+"%")
	}
	if f.UserID != "" {
		query += " AND e.user_id = ?"
		args = append(args, f.UserID)
	}
	if f.Query != "" && !f.UseFTS {
		query += " AND (e.title LIKE ? OR e.description LIKE ? OR e.location LIKE ? OR p.name LIKE ?)"
		like := "%" + f.Query + "%"
		args = append(args, like, like, like, like)
	}
	return query, args
}

func buildEventOrder(sort string) string {
	if sort == "desc" {
		return " ORDER BY e.event_date DESC"
	}
	return " ORDER BY e.event_date ASC"
}

// ScanEvents scans rows into a TimelineEvent slice (expects the full SELECT + person JOIN).
func ScanEvents(rows *sql.Rows) []models.TimelineEvent {
	events := make([]models.TimelineEvent, 0)
	for rows.Next() {
		var e models.TimelineEvent
		var p models.Person
		var personID sql.NullInt64
		var lat, lng sql.NullFloat64
		var pID sql.NullInt64
		var pName, pAvatar, pBio, pBirth, pColor, pCreated sql.NullString
		var thumbnail, mediaCaption, mediaURL, tags, recurring, weatherData, startTime, endTime sql.NullString
		var isFav sql.NullBool

		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &mediaURL, &thumbnail, &mediaCaption, &tags, &e.SortOrder, &e.IsPublic, &isFav, &e.CreatedAt, &personID, &lat, &lng, &recurring, &weatherData, &e.UserID, &startTime, &endTime,
			&pID, &pName, &pAvatar, &pBio, &pBirth, &pColor, &pCreated)
		if err != nil {
			continue
		}

		e.IsFavorite = isFav.Bool
		e.MediaURL = mediaURL.String
		e.Thumbnail = thumbnail.String
		e.MediaCaption = mediaCaption.String
		e.Tags = tags.String
		e.Recurring = recurring.String
		e.WeatherData = weatherData.String
		e.StartTime = startTime.String
		e.EndTime = endTime.String

		if personID.Valid {
			pid := int(personID.Int64)
			e.PersonID = &pid
		}
		if lat.Valid {
			v := lat.Float64
			e.Latitude = &v
		}
		if lng.Valid {
			v := lng.Float64
			e.Longitude = &v
		}

		if pID.Valid {
			p.ID = int(pID.Int64)
			p.Name = pName.String
			p.AvatarURL = pAvatar.String
			p.Bio = pBio.String
			p.BirthDate = pBirth.String
			p.Color = pColor.String
			p.CreatedAt = pCreated.String
			e.Person = &p
		}

		events = append(events, e)
	}
	return events
}

// ---- Stats query helpers ----

// QueryYearStats returns aggregate statistics for a given year.
func QueryYearStats(d *sql.DB, year string) models.EventStats {
	var s models.EventStats
	s.ByMonth = make(map[string]int)
	s.ByTag = make(map[string]int)
	s.ByMedia = make(map[string]int)
	s.YearOverYear = make(map[string]int)
	s.ByYear = make(map[string]int)

	d.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", year).Scan(&s.Total)
	d.QueryRow("SELECT COUNT(DISTINCT location) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND location != ''", year).Scan(&s.Locations)
	d.QueryRow("SELECT COUNT(DISTINCT person_id) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND person_id IS NOT NULL", year).Scan(&s.Persons)
	d.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND media_url != ''", year).Scan(&s.MediaTotal)
	d.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND location != ''", year).Scan(&s.WithLocation)
	d.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND latitude != 0 AND longitude != 0", year).Scan(&s.WithGeo)
	d.QueryRow("SELECT COUNT(*) FROM persons").Scan(&s.PersonCount)
	s.WithMedia = s.MediaTotal

	if monthRows, err := d.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? GROUP BY strftime('%m', event_date)`, year); err == nil {
		for monthRows.Next() {
			var m string
			var c int
			monthRows.Scan(&m, &c)
			s.ByMonth[m] = c
		}
		monthRows.Close()
	}

	if tagRows, err := d.Query(`SELECT tags, COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND tags != '' GROUP BY tags`, year); err == nil {
		for tagRows.Next() {
			var t string
			var c int
			tagRows.Scan(&t, &c)
			s.ByTag[t] = c
		}
		tagRows.Close()
	}

	if mediaRows, err := d.Query(`SELECT media_type, COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? GROUP BY media_type`, year); err == nil {
		for mediaRows.Next() {
			var m string
			var c int
			mediaRows.Scan(&m, &c)
			s.ByMedia[m] = c
		}
		mediaRows.Close()
	}

	if yoyRows, err := d.Query(`SELECT strftime('%Y', event_date), COUNT(*) FROM timeline_events GROUP BY strftime('%Y', event_date) ORDER BY strftime('%Y', event_date) DESC LIMIT 5`); err == nil {
		for yoyRows.Next() {
			var y string
			var c int
			yoyRows.Scan(&y, &c)
			s.YearOverYear[y] = c
			s.ByYear[y] = c
		}
		yoyRows.Close()
	}

	return s
}

// QueryMonthlyCounts returns event counts grouped by month for a year.
func QueryMonthlyCounts(d *sql.DB, year string) map[string]int {
	result := make(map[string]int)
	rows, err := d.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events
		WHERE strftime('%Y', event_date) = ? GROUP BY strftime('%m', event_date)`, year)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var m string
		var c int
		rows.Scan(&m, &c)
		result[m] = c
	}
	return result
}

// QueryWeekdayCounts returns event counts grouped by weekday (0=Sunday).
func QueryWeekdayCounts(d *sql.DB, year string) map[string]int {
	result := make(map[string]int)
	rows, err := d.Query(`SELECT CAST(strftime('%w', event_date) AS INTEGER), COUNT(*) FROM timeline_events
		WHERE strftime('%Y', event_date) = ? GROUP BY strftime('%w', event_date)`, year)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var wd int
		var c int
		rows.Scan(&wd, &c)
		result[strconv.Itoa(wd)] = c
	}
	return result
}

// QueryTagFrequency returns a sorted list of tag counts for a year.
func QueryTagFrequency(d *sql.DB, year string) []TagCount {
	tagMap := make(map[string]int)
	rows, err := d.Query(`SELECT tags FROM timeline_events
		WHERE strftime('%Y', event_date) = ? AND tags != ''`, year)
	if err != nil {
		return nil
	}
	defer rows.Close()
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
	var result []TagCount
	for name, count := range tagMap {
		result = append(result, TagCount{Name: name, Count: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Count > result[j].Count })
	return result
}

// QueryPersonEventCounts returns person event counts for a year.
func QueryPersonEventCounts(d *sql.DB, year string) []PersonCount {
	result := make([]PersonCount, 0)
	rows, err := d.Query(`SELECT p.id, p.name, COUNT(e.id) as cnt FROM persons p
		LEFT JOIN timeline_events e ON e.person_id = p.id AND strftime('%Y', e.event_date) = ?
		GROUP BY p.id HAVING cnt > 0 ORDER BY cnt DESC`, year)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var pc PersonCount
		rows.Scan(&pc.ID, &pc.Name, &pc.Count)
		result = append(result, pc)
	}
	return result
}

// QueryUserEventCounts returns user event counts for a year.
func QueryUserEventCounts(d *sql.DB, year string) []UserCount {
	result := make([]UserCount, 0)
	rows, err := d.Query(`SELECT u.id, u.display_name, COUNT(e.id) as cnt FROM users u
		LEFT JOIN timeline_events e ON e.user_id = u.id AND strftime('%Y', e.event_date) = ?
		GROUP BY u.id HAVING cnt > 0 ORDER BY cnt DESC`, year)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var uc UserCount
		rows.Scan(&uc.ID, &uc.DisplayName, &uc.Count)
		result = append(result, uc)
	}
	return result
}

// QueryLocationCounts returns top locations with coordinates for a year.
func QueryLocationCounts(d *sql.DB, year string, limit int) []LocationCount {
	result := make([]LocationCount, 0)
	rows, err := d.Query(`SELECT location, latitude, longitude, COUNT(*) as cnt FROM timeline_events
		WHERE strftime('%Y', event_date) = ? AND location != '' AND latitude != 0 AND longitude != 0
		GROUP BY location ORDER BY cnt DESC LIMIT ?`, year, limit)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var lc LocationCount
		rows.Scan(&lc.Location, &lc.Lat, &lc.Lng, &lc.Count)
		result = append(result, lc)
	}
	return result
}

// QueryMediaBreakdown returns media type counts for a year.
func QueryMediaBreakdown(d *sql.DB, year string) map[string]int {
	result := make(map[string]int)
	rows, err := d.Query(`SELECT media_type, COUNT(*) FROM timeline_events
		WHERE strftime('%Y', event_date) = ? AND media_type != '' GROUP BY media_type`, year)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var mt string
		var c int
		rows.Scan(&mt, &c)
		result[mt] = c
	}
	return result
}

// QueryTopDay returns the event date with the most events for a year.
func QueryTopDay(d *sql.DB, year string) string {
	var topDay string
	var topCount int
	d.QueryRow(`SELECT event_date, COUNT(*) as cnt FROM timeline_events
		WHERE strftime('%Y', event_date) = ? GROUP BY event_date ORDER BY cnt DESC LIMIT 1`, year).Scan(&topDay, &topCount)
	return topDay
}

// ---- FTS helper ----

// SanitizeFTSQuery wraps a string for SQLite FTS5 matching.
func SanitizeFTSQuery(query string) string {
	s := query
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, `"`, `""`)
	s = `"` + s + `"`
	return s
}

// QueryInt extracts an int query parameter with default.
func QueryInt(c *gin.Context, name string, defaultVal int) int {
	if v := c.Query(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

// TimeTrackDB logs elapsed time for a database query.
func TimeTrackDB(start time.Time, name string) {
	elapsed := time.Since(start)
	_ = elapsed
	_ = name
}
