package events

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) RegisterHTMXRoutes(group *gin.RouterGroup) {
	group.GET("/events", s.htmxListEvents)
	group.GET("/events/search", s.htmxSearchEvents)
	group.POST("/events", s.htmxSaveEvent)
	group.DELETE("/events/:id", s.htmxDeleteEvent)
	group.GET("/events/:id/edit", s.htmxEditEventForm)

	group.GET("/persons", s.htmxListPersons)
	group.POST("/persons", s.htmxSavePerson)
	group.DELETE("/persons/:id", s.htmxDeletePerson)
	group.GET("/persons/:id/events", s.htmxPersonEvents)

	group.GET("/tags", s.htmxListTags)
	group.DELETE("/tags/:name", s.htmxDeleteTag)
	group.GET("/tags/:name/rename", s.htmxRenameTag)

	group.GET("/collections", s.htmxListCollections)
	group.POST("/collections", s.htmxSaveCollection)
	group.DELETE("/collections/:id", s.htmxDeleteCollection)
	group.GET("/collections/:id/edit", s.htmxEditCollectionForm)

	group.GET("/templates", s.htmxListTemplates)
	group.POST("/templates", s.htmxSaveTemplate)
	group.DELETE("/templates/:id", s.htmxDeleteTemplate)
	group.GET("/templates/:id/edit", s.htmxEditTemplateForm)

	group.GET("/users", s.htmxListUsers)
	group.POST("/users", s.htmxSaveUser)
	group.DELETE("/users/:id", s.htmxDeleteUser)
	group.GET("/users/:id/edit", s.htmxEditUserForm)

	group.GET("/trash", s.htmxListTrash)
	group.POST("/trash/:id/restore", s.htmxRestoreEvent)
	group.DELETE("/trash/:id", s.htmxPermanentDelete)
	group.POST("/trash/empty", s.htmxEmptyTrash)
}

func (s *Service) getEventsQuery(year, month, q, personID, mediaType, tag, limit, skip string) ([]httpx.EventRow, error) {
	query := `SELECT e.id, e.title, e.event_date, e.location, e.media_type, COALESCE(e.media_url,''), e.is_favorite, e.person_id, COALESCE(e.tags,''), e.description, e.event_start_time, e.event_end_time, e.recurring, e.latitude, e.longitude,
		p.name, p.color
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '')`
	args := []any{}

	if year != "" {
		query += " AND strftime('%Y', e.event_date) = ?"
		args = append(args, year)
	}
	if month != "" {
		query += " AND strftime('%m', e.event_date) = ?"
		args = append(args, month)
	}
	if q != "" {
		query += " AND (e.title LIKE ? OR e.description LIKE ? OR e.location LIKE ? OR p.name LIKE ?)"
		qp := "%" + q + "%"
		args = append(args, qp, qp, qp, qp)
	}
	if personID != "" {
		query += " AND e.person_id = ?"
		args = append(args, personID)
	}
	if mediaType != "" {
		query += " AND e.media_type = ?"
		args = append(args, mediaType)
	}
	if tag != "" {
		query += " AND e.tags LIKE ?"
		args = append(args, "%"+tag+"%")
	}

	query += " ORDER BY e.event_date ASC"

	if limit != "" {
		l, err := strconv.Atoi(limit)
		if err == nil && l > 0 {
			query += " LIMIT ?"
			args = append(args, l)
			if skip != "" {
				s, err := strconv.Atoi(skip)
				if err == nil && s > 0 {
					query += " OFFSET ?"
					args = append(args, s)
				}
			}
		}
	}

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []httpx.EventRow
	for rows.Next() {
		var e httpx.EventRow
		var personID sql.NullInt64
		var personName, personColor sql.NullString
		var lat, lng sql.NullFloat64
		var desc, startTime, endTime, recurring sql.NullString
		err := rows.Scan(&e.ID, &e.Title, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.IsFavorite, &personID, &e.Tags, &desc, &startTime, &endTime, &recurring, &lat, &lng, &personName, &personColor)
		if err != nil {
			continue
		}
		if personID.Valid {
			e.PersonID = int(personID.Int64)
		}
		if personName.Valid {
			e.PersonName = personName.String
		}
		if personColor.Valid {
			e.PersonColor = personColor.String
		}
		if desc.Valid {
			e.Description = desc.String
		}
		if startTime.Valid {
			e.StartTime = startTime.String
		}
		if endTime.Valid {
			e.EndTime = endTime.String
		}
		if recurring.Valid {
			e.Recurring = recurring.String
		}
		if lat.Valid {
			e.Latitude = lat.Float64
		}
		if lng.Valid {
			e.Longitude = lng.Float64
		}
		events = append(events, e)
	}

	return events, nil
}

func (s *Service) htmxListEvents(c *gin.Context) {
	year := c.Query("year")
	events, err := s.getEventsQuery(year, "", "", "", "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	s.Renderer.Render(c.Writer, "event-list", events)
}

func (s *Service) htmxSearchEvents(c *gin.Context) {
	q := c.Query("q")
	personID := c.Query("person_id")
	mediaType := c.Query("media")
	tag := c.Query("tag")
	year := c.Query("year")

	events, err := s.getEventsQuery(year, "", q, personID, mediaType, tag, "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	s.Renderer.Render(c.Writer, "event-list", events)
}

func (s *Service) htmxReadForm(c *gin.Context) map[string]string {
	data := make(map[string]string)
	body, err := c.GetRawData()
	if err != nil {
		return data
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ParseForm()
	for k := range c.Request.PostForm {
		data[k] = c.Request.PostForm.Get(k)
	}
	bodyStr := strings.TrimSpace(string(body))
	useJSON := false
	if len(body) > 0 {
		if len(data) == 0 {
			useJSON = true
		} else if bodyStr[0] == '{' || bodyStr[0] == '[' {
			useJSON = true
		}
	}
	if useJSON {
		var jsonData map[string]string
		if err := json.Unmarshal(body, &jsonData); err == nil {
			maps.Copy(data, jsonData)
		}
	}
	return data
}

func (s *Service) htmxSaveEvent(c *gin.Context) {
	data := s.htmxReadForm(c)

	idStr := data["id"]
	title := strings.TrimSpace(data["title"])
	if title == "" {
		c.String(http.StatusBadRequest, "Title is required")
		return
	}

	date := data["date"]
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	location := data["location"]
	tags := data["tags"]
	mediaType := data["media_type"]
	if mediaType == "" {
		mediaType = "image"
	}
	recurring := data["recurring"]
	startTime := data["start_time"]
	endTime := data["end_time"]
	latStr := data["latitude"]
	lngStr := data["longitude"]
	personName := data["person_name"]

	desc := data["description"]

	id := httpx.ParseIntOrZero(idStr)

	var personID int
	if personName != "" {
		s.DB.QueryRow("SELECT id FROM persons WHERE name = ?", personName).Scan(&personID)
		if personID == 0 {
			result, err := s.DB.Exec("INSERT INTO persons (name, color) VALUES (?, ?)", personName, models.DefaultColor)
			if err == nil {
				lid, _ := result.LastInsertId()
				personID = int(lid)
			}
		}
	}

	if id == 0 {
		_, err := s.DB.Exec(`INSERT INTO timeline_events 
			(title, description, event_date, location, media_type, tags, recurring, event_start_time, event_end_time, person_id) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			title, desc, date, location, mediaType, tags, recurring, startTime, endTime, personID)
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to save event")
			return
		}
	} else {
		_, err := s.DB.Exec(`UPDATE timeline_events SET 
			title=?, description=?, event_date=?, location=?, media_type=?, tags=?, recurring=?, event_start_time=?, event_end_time=?, person_id=?
			WHERE id=?`,
			title, desc, date, location, mediaType, tags, recurring, startTime, endTime, personID, id)
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to update event")
			return
		}
	}

	if latStr != "" && lngStr != "" {
		lat, err1 := strconv.ParseFloat(latStr, 64)
		lng, err2 := strconv.ParseFloat(lngStr, 64)
		if err1 == nil && err2 == nil {
			if id == 0 {
				s.DB.Exec("UPDATE timeline_events SET latitude=?, longitude=? WHERE id=(SELECT MAX(id) FROM timeline_events)", lat, lng)
			} else {
				s.DB.Exec("UPDATE timeline_events SET latitude=?, longitude=? WHERE id=?", lat, lng, id)
			}
		}
	}

	events, err := s.getEventsQuery("", "", "", "", "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("HX-Trigger", "reloadEvents")
	s.Renderer.Render(c.Writer, "event-list", events)
}

func (s *Service) htmxDeleteEvent(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	s.DB.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=?", id)

	events, err := s.getEventsQuery("", "", "", "", "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	s.Renderer.Render(c.Writer, "event-list", events)
}

func (s *Service) htmxEditEventForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	var e httpx.EventRow
	var personID sql.NullInt64
	var personName, personColor sql.NullString
	var lat, lng sql.NullFloat64
	var desc, startTime, endTime, recurring sql.NullString
	err = s.DB.QueryRow(`SELECT e.id, e.title, e.event_date, e.location, e.media_type, e.media_url, e.is_favorite, e.person_id, e.tags, e.description, e.event_start_time, e.event_end_time, e.recurring, e.latitude, e.longitude,
		p.name, p.color
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE e.id=?`, id).Scan(
		&e.ID, &e.Title, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.IsFavorite, &personID, &e.Tags, &desc, &startTime, &endTime, &recurring, &lat, &lng, &personName, &personColor)

	if err != nil {
		c.String(http.StatusNotFound, "Event not found")
		return
	}

	if personID.Valid {
		e.PersonID = int(personID.Int64)
	}
	if personName.Valid {
		e.PersonName = personName.String
	}
	if personColor.Valid {
		e.PersonColor = personColor.String
	}
	if desc.Valid {
		e.Description = desc.String
	}
	if startTime.Valid {
		e.StartTime = startTime.String
	}
	if endTime.Valid {
		e.EndTime = endTime.String
	}
	if recurring.Valid {
		e.Recurring = recurring.String
	}
	if lat.Valid {
		e.Latitude = lat.Float64
	}
	if lng.Valid {
		e.Longitude = lng.Float64
	}

	s.Renderer.Render(c.Writer, "event-form", e)
}

func (s *Service) htmxListPersons(c *gin.Context) {
	q := c.Query("q")
	query := `SELECT p.id, p.name, COALESCE(p.avatar_url,''), COALESCE(p.bio,''), COALESCE(p.birth_date,''), COALESCE(p.color,'#7c3aed'),
		(SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count
		FROM persons p`
	args := []any{}
	if q != "" {
		query += " WHERE p.name LIKE ?"
		args = append(args, "%"+q+"%")
	}
	query += " ORDER BY p.name ASC"

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var persons []httpx.PersonRow
	for rows.Next() {
		var p httpx.PersonRow
		if err := rows.Scan(&p.ID, &p.Name, &p.AvatarURL, &p.Bio, &p.BirthDate, &p.Color, &p.EventCount); err == nil {
			persons = append(persons, p)
		}
	}

	s.Renderer.Render(c.Writer, "person-list", persons)
}

func (s *Service) htmxSavePerson(c *gin.Context) {
	data := s.htmxReadForm(c)
	id := httpx.ParseIntOrZero(data["id"])
	name := data["name"]
	bio := data["bio"]
	birthDate := data["birth_date"]
	color := data["color"]
	if color == "" {
		color = models.DefaultColor
	}

	if id == 0 {
		s.DB.Exec("INSERT INTO persons (name, bio, birth_date, color) VALUES (?, ?, ?, ?)", name, bio, birthDate, color)
	} else {
		s.DB.Exec("UPDATE persons SET name=?, bio=?, birth_date=?, color=? WHERE id=?", name, bio, birthDate, color, id)
	}

	s.htmxListPersons(c)
}

func (s *Service) htmxDeletePerson(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	s.DB.Exec("UPDATE timeline_events SET person_id = NULL WHERE person_id = ?", id)
	s.DB.Exec("DELETE FROM persons WHERE id=?", id)

	s.htmxListPersons(c)
}

func (s *Service) htmxPersonEvents(c *gin.Context) {
	idStr := c.Param("id")
	if _, err := strconv.Atoi(idStr); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	events, err := s.getEventsQuery("", "", "", idStr, "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	s.Renderer.Render(c.Writer, "event-list", events)
}

func (s *Service) htmxListTags(c *gin.Context) {
	rows, err := s.DB.Query(`SELECT name, COUNT(*) as cnt FROM (
		SELECT TRIM(value) as name FROM timeline_events, json_each('["' || REPLACE(tags, ',', '","') || '"]') WHERE tags != '' AND tags IS NOT NULL AND (deleted_at IS NULL OR deleted_at = '')
	) GROUP BY name ORDER BY cnt DESC, name ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tags []httpx.TagRow
	for rows.Next() {
		var t httpx.TagRow
		if err := rows.Scan(&t.Name, &t.Count); err == nil && t.Name != "" {
			tags = append(tags, t)
		}
	}

	s.Renderer.Render(c.Writer, "tag-table", tags)
}

func (s *Service) htmxDeleteTag(c *gin.Context) {
	name := c.Param("name")
	s.DB.Exec(`UPDATE timeline_events SET tags = TRIM(REPLACE(REPLACE(',' || tags || ',', ',' || ? || ',', ','), ',', ' ')) WHERE tags LIKE ?`, name, "%"+name+"%")
	s.DB.Exec(`UPDATE timeline_events SET tags = TRIM(REPLACE(tags, ',', '')) WHERE tags LIKE ?`, name)

	s.htmxListTags(c)
}

func (s *Service) htmxRenameTag(c *gin.Context) {
	oldName := c.Param("name")
	newName := c.Query("new_name")
	if newName == "" {
		newName = oldName
	}

	s.DB.Exec(`UPDATE timeline_events SET tags = REPLACE(tags, ?, ?) WHERE tags LIKE ?`, oldName, newName, "%"+oldName+"%")

	s.htmxListTags(c)
}

func (s *Service) htmxListCollections(c *gin.Context) {
	rows, err := s.DB.Query(`SELECT c.id, c.name, COALESCE(c.description,''), COALESCE(c.color,'#7c3aed'),
		(SELECT COUNT(*) FROM collection_events WHERE collection_id = c.id) as event_count
		FROM collections c ORDER BY c.name ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var collections []httpx.CollectionRow
	for rows.Next() {
		var col httpx.CollectionRow
		if err := rows.Scan(&col.ID, &col.Name, &col.Description, &col.Color, &col.EventCount); err == nil {
			collections = append(collections, col)
		}
	}

	s.Renderer.Render(c.Writer, "collection-list-htmx", collections)
}

func (s *Service) htmxSaveCollection(c *gin.Context) {
	data := s.htmxReadForm(c)
	id := httpx.ParseIntOrZero(data["id"])
	name := data["name"]
	description := data["description"]
	color := data["color"]
	if color == "" {
		color = models.DefaultColor
	}

	if id == 0 {
		s.DB.Exec("INSERT INTO collections (name, description, color) VALUES (?, ?, ?)", name, description, color)
	} else {
		s.DB.Exec("UPDATE collections SET name=?, description=?, color=? WHERE id=?", name, description, color, id)
	}

	s.htmxListCollections(c)
}

func (s *Service) htmxDeleteCollection(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	s.DB.Exec("DELETE FROM collection_events WHERE collection_id = ?", id)
	s.DB.Exec("DELETE FROM collections WHERE id=?", id)

	s.htmxListCollections(c)
}

func (s *Service) htmxEditCollectionForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	if id == 0 {
		s.Renderer.Render(c.Writer, "collection-form", httpx.CollectionRow{Color: models.DefaultColor})
		return
	}

	var col httpx.CollectionRow
	err = s.DB.QueryRow("SELECT id, name, COALESCE(description,''), COALESCE(color,'#7c3aed'), 0 FROM collections WHERE id=?", id).Scan(
		&col.ID, &col.Name, &col.Description, &col.Color, &col.EventCount)
	if err != nil {
		c.String(http.StatusNotFound, "Collection not found")
		return
	}

	s.Renderer.Render(c.Writer, "collection-form", col)
}

func (s *Service) htmxListTemplates(c *gin.Context) {
	rows, err := s.DB.Query(`SELECT t.id, t.title, COALESCE(t.tags,''), COALESCE(t.location,''), COALESCE(p.name,'')
		FROM event_templates t LEFT JOIN persons p ON t.person_id = p.id ORDER BY t.title ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var templates []httpx.TemplateRow
	for rows.Next() {
		var t httpx.TemplateRow
		if err := rows.Scan(&t.ID, &t.Title, &t.Tags, &t.Location, &t.PersonName); err == nil {
			templates = append(templates, t)
		}
	}

	s.Renderer.Render(c.Writer, "template-list-htmx", templates)
}

func (s *Service) htmxSaveTemplate(c *gin.Context) {
	data := s.htmxReadForm(c)
	id := httpx.ParseIntOrZero(data["id"])
	title := data["title"]
	tags := data["tags"]
	location := data["location"]

	if id == 0 {
		s.DB.Exec("INSERT INTO event_templates (title, tags, location) VALUES (?, ?, ?)", title, tags, location)
	} else {
		s.DB.Exec("UPDATE event_templates SET title=?, tags=?, location=? WHERE id=?", title, tags, location, id)
	}

	s.htmxListTemplates(c)
}

func (s *Service) htmxDeleteTemplate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	s.DB.Exec("DELETE FROM event_templates WHERE id=?", id)

	s.htmxListTemplates(c)
}

func (s *Service) htmxEditTemplateForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	if id == 0 {
		s.Renderer.Render(c.Writer, "template-form", httpx.TemplateRow{})
		return
	}

	var t httpx.TemplateRow
	err = s.DB.QueryRow("SELECT id, title, COALESCE(tags,''), COALESCE(location,''), '' FROM event_templates WHERE id=?", id).Scan(
		&t.ID, &t.Title, &t.Tags, &t.Location, &t.PersonName)
	if err != nil {
		c.String(http.StatusNotFound, "Template not found")
		return
	}

	s.Renderer.Render(c.Writer, "template-form", t)
}

func (s *Service) htmxListUsers(c *gin.Context) {
	rows, err := s.DB.Query(`SELECT u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.email,''), COALESCE(u.color,'#7c3aed'),
		(SELECT COUNT(*) FROM timeline_events WHERE user_id = u.id) as event_count
		FROM users u ORDER BY u.display_name ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []httpx.UserRow
	for rows.Next() {
		var u httpx.UserRow
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Color, &u.EventCount); err == nil {
			if u.DisplayName == "" {
				u.DisplayName = u.Username
			}
			users = append(users, u)
		}
	}

	s.Renderer.Render(c.Writer, "user-list-htmx", users)
}

func (s *Service) htmxSaveUser(c *gin.Context) {
	data := s.htmxReadForm(c)
	id := httpx.ParseIntOrZero(data["id"])
	username := data["username"]
	displayName := data["display_name"]
	email := data["email"]
	color := data["color"]
	if color == "" {
		color = models.DefaultColor
	}

	if id == 0 {
		s.DB.Exec("INSERT INTO users (username, display_name, email, color) VALUES (?, ?, ?, ?)", username, displayName, email, color)
	} else {
		s.DB.Exec("UPDATE users SET username=?, display_name=?, email=?, color=? WHERE id=?", username, displayName, email, color, id)
	}

	s.htmxListUsers(c)
}

func (s *Service) htmxDeleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	s.DB.Exec("UPDATE timeline_events SET user_id = 0 WHERE user_id = ?", id)
	s.DB.Exec("DELETE FROM users WHERE id=?", id)

	s.htmxListUsers(c)
}

func (s *Service) htmxEditUserForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	if id == 0 {
		s.Renderer.Render(c.Writer, "user-form", httpx.UserRow{Color: models.DefaultColor})
		return
	}

	var u httpx.UserRow
	err = s.DB.QueryRow("SELECT id, username, COALESCE(display_name,''), COALESCE(email,''), COALESCE(color,'#7c3aed'), 0 FROM users WHERE id=?", id).Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Color, &u.EventCount)
	if err != nil {
		c.String(http.StatusNotFound, "User not found")
		return
	}

	s.Renderer.Render(c.Writer, "user-form", u)
}

func (s *Service) htmxListTrash(c *gin.Context) {
	rows, err := s.DB.Query(`SELECT id, title, event_date, COALESCE(deleted_at,'') FROM timeline_events WHERE deleted_at IS NOT NULL AND deleted_at != '' ORDER BY deleted_at DESC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var trash []httpx.TrashRow
	for rows.Next() {
		var t httpx.TrashRow
		if err := rows.Scan(&t.ID, &t.Title, &t.Date, &t.DeletedAt); err == nil {
			trash = append(trash, t)
		}
	}

	s.Renderer.Render(c.Writer, "trash-list-htmx", trash)
}

func (s *Service) htmxRestoreEvent(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	s.DB.Exec("UPDATE timeline_events SET deleted_at=NULL WHERE id=?", id)
	s.htmxListTrash(c)
}

func (s *Service) htmxPermanentDelete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	var mediaURL string
	s.DB.QueryRow("SELECT media_url FROM timeline_events WHERE id=?", id).Scan(&mediaURL)

	s.DB.Exec("DELETE FROM timeline_events WHERE id=?", id)

	if mediaURL != "" {
		fullPath := filepath.Join(s.Media.MediaPath(), mediaURL)
		go func() {
			os.Remove(fullPath)
			os.Remove(fullPath + ".thumb.jpg")
		}()
	}

	s.htmxListTrash(c)
}

func (s *Service) htmxEmptyTrash(c *gin.Context) {
	s.DB.Exec("DELETE FROM timeline_events WHERE deleted_at IS NOT NULL AND deleted_at != ''")
	s.htmxListTrash(c)
}
