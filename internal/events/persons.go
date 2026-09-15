package events

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetPersonEvents(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
		return
	}
	var person models.Person
	err = s.DB.QueryRow(`SELECT id, name, avatar_url, bio, birth_date, color, created_at FROM persons WHERE id = ?`, id).
		Scan(&person.ID, &person.Name, &person.AvatarURL, &person.Bio, &person.BirthDate, &person.Color, &person.CreatedAt)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Person not found"})
		return
	} else if err != nil {
		httpx.ServerError(c, err)
		return
	}
	query, args := BuildEventQuery(EventFilters{PersonID: idStr, Sort: "asc"})
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	evs := ScanEvents(rows)
	milestones := make([]PersonMilestone, 0, len(evs))
	for _, e := range evs {
		m := PersonMilestone{TimelineEvent: e, Group: LifeGroup(person.BirthDate, e.Date)}
		if years, months, ok := AgeAt(person.BirthDate, e.Date); ok {
			m.AgeYears = &years
			m.AgeMonths = &months
		}
		milestones = append(milestones, m)
	}
	c.JSON(http.StatusOK, PersonEventsResponse{Person: person, Events: milestones})
}

func (s *Service) GetPersons(c *gin.Context) {
	q := c.Query("q")
	query := `SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at,
		(SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count
		FROM persons p`
	args := []any{}
	if q != "" {
		query += ` WHERE p.name LIKE ?`
		args = append(args, "%"+q+"%")
	}
	query += ` ORDER BY p.name ASC`
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	persons := make([]models.Person, 0)
	for rows.Next() {
		var p models.Person
		err := rows.Scan(&p.ID, &p.Name, &p.AvatarURL, &p.Bio, &p.BirthDate, &p.Color, &p.CreatedAt, &p.EventCount)
		if err != nil {
			continue
		}
		persons = append(persons, p)
	}
	c.JSON(http.StatusOK, persons)
}

func (s *Service) SavePerson(c *gin.Context) {
	var p models.Person
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if p.ID == 0 {
		result, err := s.DB.Exec("INSERT INTO persons (name, avatar_url, bio, birth_date, color) VALUES (?, ?, ?, ?, ?)",
			p.Name, p.AvatarURL, p.Bio, p.BirthDate, p.Color)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		p.ID = int(id)
		s.Integrations.SendGotifyNotification(fmt.Sprintf("Person created: %s", p.Name), p.Bio)
	} else {
		_, err := s.DB.Exec("UPDATE persons SET name=?, avatar_url=?, bio=?, birth_date=?, color=? WHERE id=?",
			p.Name, p.AvatarURL, p.Bio, p.BirthDate, p.Color, p.ID)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		s.Integrations.SendGotifyNotification(fmt.Sprintf("Person updated: %s", p.Name), p.Bio)
	}
	c.JSON(http.StatusOK, p)
}

func (s *Service) DeletePerson(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
		return
	}
	var name string
	s.DB.QueryRow("SELECT name FROM persons WHERE id=?", id).Scan(&name)
	s.DB.Exec("UPDATE timeline_events SET person_id = NULL WHERE person_id = ?", id)
	_, err = s.DB.Exec("DELETE FROM persons WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete person"})
		return
	}
	s.Integrations.SendGotifyNotification(fmt.Sprintf("Person deleted: %s", name), "")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
