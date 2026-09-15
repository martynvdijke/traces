package events

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetTemplates(c *gin.Context) {
	rows, err := s.DB.Query("SELECT id, title, description, tags, person_id, user_id, location, media_type, created_at FROM event_templates ORDER BY title")
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	templates := make([]models.EventTemplate, 0)
	for rows.Next() {
		var t models.EventTemplate
		var pid sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Tags, &pid, &t.UserID, &t.Location, &t.MediaType, &t.CreatedAt); err == nil {
			if pid.Valid {
				p := int(pid.Int64)
				t.PersonID = &p
			}
			templates = append(templates, t)
		}
	}
	c.JSON(http.StatusOK, templates)
}

func (s *Service) SaveTemplate(c *gin.Context) {
	var t models.EventTemplate
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if t.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
		return
	}
	pid := 0
	if t.PersonID != nil {
		pid = *t.PersonID
	}
	if t.ID == 0 {
		result, err := s.DB.Exec("INSERT INTO event_templates (title, description, tags, person_id, user_id, location, media_type) VALUES (?, ?, ?, ?, ?, ?, ?)",
			t.Title, t.Description, t.Tags, pid, t.UserID, t.Location, t.MediaType)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		t.ID = int(id)
	} else {
		_, err := s.DB.Exec("UPDATE event_templates SET title=?, description=?, tags=?, person_id=?, user_id=?, location=?, media_type=? WHERE id=?",
			t.Title, t.Description, t.Tags, pid, t.UserID, t.Location, t.MediaType, t.ID)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, t)
}

func (s *Service) DeleteTemplate(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
		return
	}
	_, err = s.DB.Exec("DELETE FROM event_templates WHERE id=?", id)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) ApplyTemplate(c *gin.Context) {
	var input struct {
		TemplateID int    `json:"template_id"`
		Date       string `json:"date"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Date == "" {
		input.Date = time.Now().Format("2006-01-02")
	}
	var t models.EventTemplate
	var pid sql.NullInt64
	err := s.DB.QueryRow("SELECT id, title, description, tags, person_id, user_id, location, media_type FROM event_templates WHERE id=?", input.TemplateID).
		Scan(&t.ID, &t.Title, &t.Description, &t.Tags, &pid, &t.UserID, &t.Location, &t.MediaType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Template not found"})
		return
	}
	if pid.Valid {
		p := int(pid.Int64)
		t.PersonID = &p
	}
	event := models.TimelineEvent{
		Title:       t.Title,
		Description: t.Description,
		Date:        input.Date,
		Location:    t.Location,
		Tags:        t.Tags,
		MediaType:   t.MediaType,
		UserID:      t.UserID,
		PersonID:    t.PersonID,
	}
	result, err := s.DB.Exec(`INSERT INTO timeline_events
		(title, description, event_date, location, media_type, tags, user_id, person_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Title, event.Description, event.Date, event.Location, event.MediaType, event.Tags, event.UserID, event.PersonID)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	id, _ := result.LastInsertId()
	event.ID = int(id)
	s.Integrations.SendGotifyNotification(fmt.Sprintf("Event created from template: %s", event.Title), event.Description)
	c.JSON(http.StatusOK, event)
}
