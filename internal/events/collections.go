package events

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetCollections(c *gin.Context) {
	rows, err := s.DB.Query(`SELECT c.id, c.name, c.description, c.color, c.created_at,
		(SELECT COUNT(*) FROM collection_events ce WHERE ce.collection_id = c.id) as event_count
		FROM collections c ORDER BY c.name`)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	collections := make([]models.Collection, 0)
	for rows.Next() {
		var col models.Collection
		if err := rows.Scan(&col.ID, &col.Name, &col.Description, &col.Color, &col.CreatedAt, &col.EventCount); err == nil {
			collections = append(collections, col)
		}
	}
	c.JSON(http.StatusOK, collections)
}

func (s *Service) SaveCollection(c *gin.Context) {
	var col models.Collection
	if err := c.ShouldBindJSON(&col); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if col.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}
	if col.Color == "" {
		col.Color = models.DefaultColor
	}
	if col.ID == 0 {
		result, err := s.DB.Exec("INSERT INTO collections (name, description, color) VALUES (?, ?, ?)", col.Name, col.Description, col.Color)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		col.ID = int(id)
	} else {
		_, err := s.DB.Exec("UPDATE collections SET name=?, description=?, color=? WHERE id=?", col.Name, col.Description, col.Color, col.ID)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
	}
	col.CreatedAt = time.Now().Format(time.RFC3339)
	c.JSON(http.StatusOK, col)
}

func (s *Service) DeleteCollection(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid collection ID"})
		return
	}
	s.DB.Exec("DELETE FROM collection_events WHERE collection_id=?", id)
	_, err = s.DB.Exec("DELETE FROM collections WHERE id=?", id)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) GetCollectionEvents(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid collection ID"})
		return
	}
	query := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e
		LEFT JOIN persons p ON e.person_id = p.id
		INNER JOIN collection_events ce ON ce.event_id = e.id
		WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND ce.collection_id = ?
		ORDER BY e.event_date ASC`
	rows, err := s.DB.Query(query, id)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	evs := ScanEventsWithPerson(rows)
	c.JSON(http.StatusOK, evs)
}

func (s *Service) AddEventToCollection(c *gin.Context) {
	colIDStr := c.Param("id")
	colID, err := strconv.Atoi(colIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid collection ID"})
		return
	}
	var input struct {
		EventID int `json:"event_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_, err = s.DB.Exec("INSERT OR IGNORE INTO collection_events (collection_id, event_id) VALUES (?, ?)", colID, input.EventID)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) RemoveEventFromCollection(c *gin.Context) {
	colIDStr := c.Param("id")
	colID, err := strconv.Atoi(colIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid collection ID"})
		return
	}
	eventIDStr := c.Query("event_id")
	eventID, err := strconv.Atoi(eventIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}
	_, err = s.DB.Exec("DELETE FROM collection_events WHERE collection_id=? AND event_id=?", colID, eventID)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
