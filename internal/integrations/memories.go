package integrations

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetMemories(c *gin.Context) {
	_, span := httpx.StartSpan(s.currentTracer(), c, "getMemories")
	defer span.End()

	var cfg models.MemoriesConfig
	var enabledInt int
	err := s.db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &cfg.DaysWindow, &cfg.EmailEnabled)
	if err != nil || enabledInt == 0 {
		c.JSON(http.StatusOK, []any{})
		return
	}
	cfg.Enabled = enabledInt == 1

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -cfg.DaysWindow)
	end := today.AddDate(0, 0, cfg.DaysWindow)

	startMD := start.Format("01-02")
	endMD := end.Format("01-02")

	var rows *sql.Rows
	if startMD <= endMD {
		rows, err = s.db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE (e.deleted_at IS NULL OR e.deleted_at = '')
			AND e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND strftime('%m-%d', e.event_date) BETWEEN ? AND ?
			ORDER BY e.event_date DESC`, startMD, endMD)
	} else {
		rows, err = s.db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE (e.deleted_at IS NULL OR e.deleted_at = '')
			AND e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND (strftime('%m-%d', e.event_date) >= ? OR strftime('%m-%d', e.event_date) <= ?)
			ORDER BY e.event_date DESC`, startMD, endMD)
	}

	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	type MemoryEvent struct {
		models.TimelineEvent
		YearsAgo int `json:"years_ago"`
	}

	memories := make([]MemoryEvent, 0)
	pMap := make(map[int]models.Person)
	pRows, _ := s.db.Query("SELECT id, name, avatar_url, bio, birth_date, color, created_at FROM persons")
	if pRows != nil {
		defer pRows.Close()
		for pRows.Next() {
			var p models.Person
			if pRows.Scan(&p.ID, &p.Name, &p.AvatarURL, &p.Bio, &p.BirthDate, &p.Color, &p.CreatedAt) == nil {
				pMap[p.ID] = p
			}
		}
	}

	for rows.Next() {
		var me MemoryEvent
		var personID sql.NullInt64
		var thumbnail, mediaCaption, mediaURL, tags, recurring, weatherData, startTime, endTime sql.NullString
		var isFav sql.NullBool
		err := rows.Scan(&me.ID, &me.Title, &me.Description, &me.Date, &me.Location, &me.MediaType, &mediaURL, &thumbnail, &mediaCaption, &tags, &me.SortOrder, &me.IsPublic, &isFav, &me.CreatedAt, &personID, &me.Latitude, &me.Longitude, &recurring, &weatherData, &me.UserID, &startTime, &endTime, &me.YearsAgo)
		if err != nil {
			continue
		}
		me.IsFavorite = isFav.Bool
		me.MediaURL = mediaURL.String
		me.Thumbnail = thumbnail.String
		me.MediaCaption = mediaCaption.String
		me.Tags = tags.String
		me.Recurring = recurring.String
		me.WeatherData = weatherData.String
		me.StartTime = startTime.String
		me.EndTime = endTime.String
		if personID.Valid {
			pid := int(personID.Int64)
			me.PersonID = &pid
			if p, ok := pMap[pid]; ok {
				me.Person = &p
			}
		}
		memories = append(memories, me)
	}

	c.JSON(http.StatusOK, memories)
}

func (s *Service) GetMemoriesConfig(c *gin.Context) {
	var cfg models.MemoriesConfig
	var enabledInt int
	err := s.db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &cfg.DaysWindow, &cfg.EmailEnabled)
	if err != nil {
		c.JSON(http.StatusOK, models.MemoriesConfig{Enabled: true, DaysWindow: 3, EmailEnabled: false})
		return
	}
	cfg.Enabled = enabledInt == 1
	c.JSON(http.StatusOK, cfg)
}

func (s *Service) SaveMemoriesConfig(c *gin.Context) {
	var cfg models.MemoriesConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}
	emailInt := 0
	if cfg.EmailEnabled {
		emailInt = 1
	}
	if cfg.DaysWindow < 1 {
		cfg.DaysWindow = 1
	}
	if cfg.DaysWindow > 14 {
		cfg.DaysWindow = 14
	}
	_, err := s.db.Exec(`UPDATE memories_settings SET enabled=?, days_window=?, email_enabled=? WHERE id=1`, enabledInt, cfg.DaysWindow, emailInt)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	if s.log != nil {
		s.log.Log("info", "memories", "Memories settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) SendMemoriesEmailHandler(c *gin.Context) {
	var memCfg models.MemoriesConfig
	var enabledInt int
	s.db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &memCfg.DaysWindow, &memCfg.EmailEnabled)
	memCfg.Enabled = enabledInt == 1

	if !memCfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Memories are disabled"})
		return
	}

	var emailCfg models.EmailConfig
	var port int
	err := s.db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&emailCfg.SMTPHost, &port, &emailCfg.SMTPUser, &emailCfg.SMTPPass, &emailCfg.FromAddr, &emailCfg.ToAddr)
	if err != nil || emailCfg.SMTPHost == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email not configured"})
		return
	}
	emailCfg.SMTPPort = port

	var recipients []string
	userRows, err := s.db.Query("SELECT email FROM users WHERE email != '' AND email IS NOT NULL")
	if err == nil {
		for userRows.Next() {
			var email string
			if err := userRows.Scan(&email); err == nil && email != "" {
				recipients = append(recipients, email)
			}
		}
		userRows.Close()
	}
	if len(recipients) == 0 && emailCfg.ToAddr != "" {
		recipients = append(recipients, emailCfg.ToAddr)
	}

	if len(recipients) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No recipient emails configured"})
		return
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -memCfg.DaysWindow)
	end := today.AddDate(0, 0, memCfg.DaysWindow)
	startMD := start.Format("01-02")
	endMD := end.Format("01-02")

	var rows *sql.Rows
	if startMD <= endMD {
		rows, err = s.db.Query(`SELECT e.title, e.event_date,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE (e.deleted_at IS NULL OR e.deleted_at = '')
			AND e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND strftime('%m-%d', e.event_date) BETWEEN ? AND ?
			ORDER BY e.event_date DESC`, startMD, endMD)
	} else {
		rows, err = s.db.Query(`SELECT e.title, e.event_date,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE (e.deleted_at IS NULL OR e.deleted_at = '')
			AND e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND (strftime('%m-%d', e.event_date) >= ? OR strftime('%m-%d', e.event_date) <= ?)
			ORDER BY e.event_date DESC`, startMD, endMD)
	}

	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	var memories []string
	for rows.Next() {
		var title, date string
		var yearsAgo int
		if err := rows.Scan(&title, &date, &yearsAgo); err == nil {
			memories = append(memories, fmt.Sprintf("  - %d year(s) ago: %s (%s)", yearsAgo, title, date))
		}
	}

	if len(memories) == 0 {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "No memories for today"})
		return
	}

	subject := "TRACES Memories - " + today.Format("January 2, 2006")
	body := "You have " + strconv.Itoa(len(memories)) + " memory/memories from this date in past years:\n\n" + strings.Join(memories, "\n")

	sent := 0
	var lastErr error
	for _, to := range recipients {
		if err := SendEmail(emailCfg, to, subject, body); err != nil {
			lastErr = err
			log.Printf("[MEMORIES] Failed to send email to %s: %v", to, err)
			continue
		}
		sent++
	}

	if sent == 0 {
		if s.log != nil {
			s.log.Log("error", "memories", "Failed to send memories email: "+lastErr.Error(), nil)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to send email"})
		return
	}

	if s.log != nil {
		s.log.Log("info", "memories", fmt.Sprintf("Sent %d memories via email to %d recipient(s)", len(memories), sent), nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": fmt.Sprintf("Sent %d memories via email to %d recipient(s)", len(memories), sent)})
}
