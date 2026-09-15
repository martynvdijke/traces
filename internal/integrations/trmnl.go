package integrations

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
)

type trmnlEvent struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Date       string `json:"date"`
	Year       int    `json:"year"`
	MediaType  string `json:"media_type"`
	MediaURL   string `json:"media_url"`
	Thumbnail  string `json:"thumbnail"`
	Location   string `json:"location"`
	PersonName string `json:"person_name"`
	Tags       string `json:"tags"`
	IsFavorite bool   `json:"is_favorite"`
}

type trmnlMediaStat struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type trmnlTagStat struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type trmnlPersonStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type trmnlStats struct {
	EventCount    int               `json:"event_count"`
	FavoriteCount int               `json:"favorite_count"`
	Media         []trmnlMediaStat  `json:"media"`
	TopTags       []trmnlTagStat    `json:"top_tags"`
	TopPersons    []trmnlPersonStat `json:"top_persons"`
}

type trmnlSummary struct {
	Month  string       `json:"month"`
	Events []trmnlEvent `json:"events"`
	Stats  trmnlStats   `json:"stats"`
}

func (s *Service) GetTRMNLSummary(c *gin.Context) {
	_, span := httpx.StartSpan(s.currentTracer(), c, "getTRMNLSummary")
	defer span.End()

	isPublicMode := false
	if s.publicMode != nil {
		isPublicMode = s.publicMode()
	}
	visibility := ""
	if !isPublicMode {
		visibility = " AND e.is_public = 1"
	}
	filter := "(e.deleted_at IS NULL OR e.deleted_at = '') AND e.event_date != '' AND strftime('%m', e.event_date) = strftime('%m', 'now')" + visibility

	rows, err := s.db.Query(`SELECT e.id, e.title, e.event_date, CAST(strftime('%Y', e.event_date) AS INTEGER), COALESCE(e.location, ''), COALESCE(e.media_type, ''), COALESCE(e.media_url, ''), COALESCE(e.thumbnail, ''), COALESCE(e.tags, ''), COALESCE(e.is_favorite, 0), COALESCE(p.name, '')
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id
		WHERE ` + filter + `
		ORDER BY e.is_favorite DESC, e.event_date DESC
		LIMIT 8`)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	events := make([]trmnlEvent, 0)
	for rows.Next() {
		var ev trmnlEvent
		var fav int
		if err := rows.Scan(&ev.ID, &ev.Title, &ev.Date, &ev.Year, &ev.Location, &ev.MediaType, &ev.MediaURL, &ev.Thumbnail, &ev.Tags, &fav, &ev.PersonName); err != nil {
			continue
		}
		ev.IsFavorite = fav == 1
		events = append(events, ev)
	}

	stats := trmnlStats{
		Media:      make([]trmnlMediaStat, 0),
		TopTags:    make([]trmnlTagStat, 0),
		TopPersons: make([]trmnlPersonStat, 0),
	}
	mediaCount := make(map[string]int)
	tagCount := make(map[string]int)
	personCount := make(map[string]int)

	sRows, err := s.db.Query(`SELECT COALESCE(e.media_type, ''), COALESCE(e.tags, ''), COALESCE(p.name, ''), COALESCE(e.is_favorite, 0)
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id
		WHERE ` + filter)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer sRows.Close()

	for sRows.Next() {
		var mediaType, tags, personName string
		var fav int
		if err := sRows.Scan(&mediaType, &tags, &personName, &fav); err != nil {
			continue
		}
		stats.EventCount++
		if fav == 1 {
			stats.FavoriteCount++
		}
		if mediaType == "" {
			mediaType = "text"
		}
		mediaCount[mediaType]++
		for _, tag := range strings.Split(tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tagCount[tag]++
			}
		}
		if personName != "" {
			personCount[personName]++
		}
	}

	for mt, count := range mediaCount {
		stats.Media = append(stats.Media, trmnlMediaStat{Type: mt, Count: count})
	}
	sort.Slice(stats.Media, func(i, j int) bool {
		return stats.Media[i].Count > stats.Media[j].Count
	})

	for tag, count := range tagCount {
		stats.TopTags = append(stats.TopTags, trmnlTagStat{Tag: tag, Count: count})
	}
	sort.Slice(stats.TopTags, func(i, j int) bool {
		return stats.TopTags[i].Count > stats.TopTags[j].Count
	})
	if len(stats.TopTags) > 3 {
		stats.TopTags = stats.TopTags[:3]
	}

	for name, count := range personCount {
		stats.TopPersons = append(stats.TopPersons, trmnlPersonStat{Name: name, Count: count})
	}
	sort.Slice(stats.TopPersons, func(i, j int) bool {
		return stats.TopPersons[i].Count > stats.TopPersons[j].Count
	})
	if len(stats.TopPersons) > 3 {
		stats.TopPersons = stats.TopPersons[:3]
	}

	c.JSON(http.StatusOK, trmnlSummary{
		Month:  time.Now().Month().String(),
		Events: events,
		Stats:  stats,
	})
}
