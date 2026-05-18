package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func registerHTMXRoutes(r *gin.Engine) {
	admin := r.Group("/api/admin")
	admin.Use(authMiddlewareGin(), csrfMiddleware())
	{
		admin.GET("/events", htmxListEvents)
		admin.GET("/events/search", htmxSearchEvents)
		admin.POST("/events", htmxSaveEvent)
		admin.DELETE("/events/:id", htmxDeleteEvent)
		admin.GET("/events/:id/edit", htmxEditEventForm)

		admin.GET("/persons", htmxListPersons)
		admin.POST("/persons", htmxSavePerson)
		admin.DELETE("/persons/:id", htmxDeletePerson)
		admin.GET("/persons/:id/events", htmxPersonEvents)

		admin.GET("/tags", htmxListTags)
		admin.DELETE("/tags/:name", htmxDeleteTag)
		admin.GET("/tags/:name/rename", htmxRenameTag)

		admin.GET("/collections", htmxListCollections)
		admin.POST("/collections", htmxSaveCollection)
		admin.DELETE("/collections/:id", htmxDeleteCollection)
		admin.GET("/collections/:id/edit", htmxEditCollectionForm)

		admin.GET("/templates", htmxListTemplates)
		admin.POST("/templates", htmxSaveTemplate)
		admin.DELETE("/templates/:id", htmxDeleteTemplate)
		admin.GET("/templates/:id/edit", htmxEditTemplateForm)

		admin.GET("/users", htmxListUsers)
		admin.POST("/users", htmxSaveUser)
		admin.DELETE("/users/:id", htmxDeleteUser)
		admin.GET("/users/:id/edit", htmxEditUserForm)

		admin.GET("/trash", htmxListTrash)
		admin.POST("/trash/:id/restore", htmxRestoreEvent)
		admin.DELETE("/trash/:id", htmxPermanentDelete)
		admin.POST("/trash/empty", htmxEmptyTrash)
	}
}

func getEventsQuery(year, month, q, personID, mediaType, tag, limit, skip string) ([]EventRow, error) {
	query := `SELECT e.id, e.title, e.event_date, e.location, e.media_type, COALESCE(e.media_url,''), e.is_favorite, e.person_id, COALESCE(e.tags,''), e.description, e.event_start_time, e.event_end_time, e.recurring, e.latitude, e.longitude,
		p.name, p.color
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '')`
	args := []interface{}{}

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

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventRow
	for rows.Next() {
		var e EventRow
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

func htmxListEvents(c *gin.Context) {
	year := c.Query("year")
	events, err := getEventsQuery(year, "", "", "", "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	renderTemplate(c.Writer, "event-list", events)
}

func htmxSearchEvents(c *gin.Context) {
	q := c.Query("q")
	personID := c.Query("person_id")
	mediaType := c.Query("media")
	tag := c.Query("tag")
	year := c.Query("year")

	events, err := getEventsQuery(year, "", q, personID, mediaType, tag, "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	renderTemplate(c.Writer, "event-list", events)
}

func htmxReadForm(c *gin.Context) map[string]string {
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
			for k, v := range jsonData {
				data[k] = v
			}
		}
	}
	return data
}

func htmxSaveEvent(c *gin.Context) {
	data := htmxReadForm(c)

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

	id := parseIntOrZero(idStr)

	var personID int
	if personName != "" {
		db.QueryRow("SELECT id FROM persons WHERE name = ?", personName).Scan(&personID)
		if personID == 0 {
			result, err := db.Exec("INSERT INTO persons (name, color) VALUES (?, ?)", personName, "#7c3aed")
			if err == nil {
				lid, _ := result.LastInsertId()
				personID = int(lid)
			}
		}
	}

	if id == 0 {
		_, err := db.Exec(`INSERT INTO timeline_events 
			(title, description, event_date, location, media_type, tags, recurring, event_start_time, event_end_time, person_id) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			title, desc, date, location, mediaType, tags, recurring, startTime, endTime, personID)
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to save event")
			return
		}
	} else {
		_, err := db.Exec(`UPDATE timeline_events SET 
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
				db.Exec("UPDATE timeline_events SET latitude=?, longitude=? WHERE id=(SELECT MAX(id) FROM timeline_events)", lat, lng)
			} else {
				db.Exec("UPDATE timeline_events SET latitude=?, longitude=? WHERE id=?", lat, lng, id)
			}
		}
	}

	events, err := getEventsQuery("", "", "", "", "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("HX-Trigger", "reloadEvents")
	renderTemplate(c.Writer, "event-list", events)
}

func htmxDeleteEvent(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	db.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=?", id)

	events, err := getEventsQuery("", "", "", "", "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	renderTemplate(c.Writer, "event-list", events)
}

func htmxEditEventForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	var e EventRow
	var personID sql.NullInt64
	var personName, personColor sql.NullString
	var lat, lng sql.NullFloat64
	var desc, startTime, endTime, recurring sql.NullString
	err = db.QueryRow(`SELECT e.id, e.title, e.event_date, e.location, e.media_type, e.media_url, e.is_favorite, e.person_id, e.tags, e.description, e.event_start_time, e.event_end_time, e.recurring, e.latitude, e.longitude,
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

	renderTemplate(c.Writer, "event-form", e)
}

func htmxListPersons(c *gin.Context) {
	q := c.Query("q")
	query := `SELECT p.id, p.name, COALESCE(p.avatar_url,''), COALESCE(p.bio,''), COALESCE(p.birth_date,''), COALESCE(p.color,'#7c3aed'),
		(SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count
		FROM persons p`
	args := []interface{}{}
	if q != "" {
		query += " WHERE p.name LIKE ?"
		args = append(args, "%"+q+"%")
	}
	query += " ORDER BY p.name ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var persons []PersonRow
	for rows.Next() {
		var p PersonRow
		if err := rows.Scan(&p.ID, &p.Name, &p.AvatarURL, &p.Bio, &p.BirthDate, &p.Color, &p.EventCount); err == nil {
			persons = append(persons, p)
		}
	}

	renderTemplate(c.Writer, "person-list", persons)
}

func htmxSavePerson(c *gin.Context) {
	data := htmxReadForm(c)
	id := parseIntOrZero(data["id"])
	name := data["name"]
	bio := data["bio"]
	birthDate := data["birth_date"]
	color := data["color"]
	if color == "" {
		color = "#7c3aed"
	}

	if id == 0 {
		db.Exec("INSERT INTO persons (name, bio, birth_date, color) VALUES (?, ?, ?, ?)", name, bio, birthDate, color)
	} else {
		db.Exec("UPDATE persons SET name=?, bio=?, birth_date=?, color=? WHERE id=?", name, bio, birthDate, color, id)
	}

	htmxListPersons(c)
}

func htmxDeletePerson(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	db.Exec("UPDATE timeline_events SET person_id = NULL WHERE person_id = ?", id)
	db.Exec("DELETE FROM persons WHERE id=?", id)

	htmxListPersons(c)
}

func htmxPersonEvents(c *gin.Context) {
	idStr := c.Param("id")
	if _, err := strconv.Atoi(idStr); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	events, err := getEventsQuery("", "", "", idStr, "", "", "100", "")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	renderTemplate(c.Writer, "event-list", events)
}

func htmxListTags(c *gin.Context) {
	rows, err := db.Query(`SELECT name, COUNT(*) as cnt FROM (
		SELECT TRIM(value) as name FROM timeline_events, json_each('["' || REPLACE(tags, ',', '","') || '"]') WHERE tags != '' AND tags IS NOT NULL AND (deleted_at IS NULL OR deleted_at = '')
	) GROUP BY name ORDER BY cnt DESC, name ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tags []TagRow
	for rows.Next() {
		var t TagRow
		if err := rows.Scan(&t.Name, &t.Count); err == nil && t.Name != "" {
			tags = append(tags, t)
		}
	}

	renderTemplate(c.Writer, "tag-table", tags)
}

func htmxDeleteTag(c *gin.Context) {
	name := c.Param("name")
	db.Exec(`UPDATE timeline_events SET tags = TRIM(REPLACE(REPLACE(',' || tags || ',', ',' || ? || ',', ','), ',', ' ')) WHERE tags LIKE ?`, name, "%"+name+"%")
	db.Exec(`UPDATE timeline_events SET tags = TRIM(REPLACE(tags, ',', '')) WHERE tags LIKE ?`, name)

	htmxListTags(c)
}

func htmxRenameTag(c *gin.Context) {
	oldName := c.Param("name")
	newName := c.Query("new_name")
	if newName == "" {
		newName = oldName
	}

	db.Exec(`UPDATE timeline_events SET tags = REPLACE(tags, ?, ?) WHERE tags LIKE ?`, oldName, newName, "%"+oldName+"%")

	htmxListTags(c)
}

func htmxListCollections(c *gin.Context) {
	rows, err := db.Query(`SELECT c.id, c.name, COALESCE(c.description,''), COALESCE(c.color,'#7c3aed'),
		(SELECT COUNT(*) FROM collection_events WHERE collection_id = c.id) as event_count
		FROM collections c ORDER BY c.name ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var collections []CollectionRow
	for rows.Next() {
		var col CollectionRow
		if err := rows.Scan(&col.ID, &col.Name, &col.Description, &col.Color, &col.EventCount); err == nil {
			collections = append(collections, col)
		}
	}

	renderTemplate(c.Writer, "collection-list-htmx", collections)
}

func htmxSaveCollection(c *gin.Context) {
	data := htmxReadForm(c)
	id := parseIntOrZero(data["id"])
	name := data["name"]
	description := data["description"]
	color := data["color"]
	if color == "" {
		color = "#7c3aed"
	}

	if id == 0 {
		db.Exec("INSERT INTO collections (name, description, color) VALUES (?, ?, ?)", name, description, color)
	} else {
		db.Exec("UPDATE collections SET name=?, description=?, color=? WHERE id=?", name, description, color, id)
	}

	htmxListCollections(c)
}

func htmxDeleteCollection(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	db.Exec("DELETE FROM collection_events WHERE collection_id = ?", id)
	db.Exec("DELETE FROM collections WHERE id=?", id)

	htmxListCollections(c)
}

func htmxEditCollectionForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	if id == 0 {
		renderTemplate(c.Writer, "collection-form", CollectionRow{Color: "#7c3aed"})
		return
	}

	var col CollectionRow
	err = db.QueryRow("SELECT id, name, COALESCE(description,''), COALESCE(color,'#7c3aed'), 0 FROM collections WHERE id=?", id).Scan(
		&col.ID, &col.Name, &col.Description, &col.Color, &col.EventCount)
	if err != nil {
		c.String(http.StatusNotFound, "Collection not found")
		return
	}

	renderTemplate(c.Writer, "collection-form", col)
}

func htmxListTemplates(c *gin.Context) {
	rows, err := db.Query(`SELECT t.id, t.title, COALESCE(t.tags,''), COALESCE(t.location,''), COALESCE(p.name,'')
		FROM event_templates t LEFT JOIN persons p ON t.person_id = p.id ORDER BY t.title ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var templates []TemplateRow
	for rows.Next() {
		var t TemplateRow
		if err := rows.Scan(&t.ID, &t.Title, &t.Tags, &t.Location, &t.PersonName); err == nil {
			templates = append(templates, t)
		}
	}

	renderTemplate(c.Writer, "template-list-htmx", templates)
}

func htmxSaveTemplate(c *gin.Context) {
	data := htmxReadForm(c)
	id := parseIntOrZero(data["id"])
	title := data["title"]
	tags := data["tags"]
	location := data["location"]

	if id == 0 {
		db.Exec("INSERT INTO event_templates (title, tags, location) VALUES (?, ?, ?)", title, tags, location)
	} else {
		db.Exec("UPDATE event_templates SET title=?, tags=?, location=? WHERE id=?", title, tags, location, id)
	}

	htmxListTemplates(c)
}

func htmxDeleteTemplate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	db.Exec("DELETE FROM event_templates WHERE id=?", id)

	htmxListTemplates(c)
}

func htmxEditTemplateForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	if id == 0 {
		renderTemplate(c.Writer, "template-form", TemplateRow{})
		return
	}

	var t TemplateRow
	err = db.QueryRow("SELECT id, title, COALESCE(tags,''), COALESCE(location,''), '' FROM event_templates WHERE id=?", id).Scan(
		&t.ID, &t.Title, &t.Tags, &t.Location, &t.PersonName)
	if err != nil {
		c.String(http.StatusNotFound, "Template not found")
		return
	}

	renderTemplate(c.Writer, "template-form", t)
}

func htmxListUsers(c *gin.Context) {
	rows, err := db.Query(`SELECT u.id, u.username, COALESCE(u.display_name,''), COALESCE(u.color,'#7c3aed'),
		(SELECT COUNT(*) FROM timeline_events WHERE user_id = u.id) as event_count
		FROM users u ORDER BY u.display_name ASC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Color, &u.EventCount); err == nil {
			if u.DisplayName == "" {
				u.DisplayName = u.Username
			}
			users = append(users, u)
		}
	}

	renderTemplate(c.Writer, "user-list-htmx", users)
}

func htmxSaveUser(c *gin.Context) {
	data := htmxReadForm(c)
	id := parseIntOrZero(data["id"])
	username := data["username"]
	displayName := data["display_name"]
	color := data["color"]
	if color == "" {
		color = "#7c3aed"
	}

	if id == 0 {
		db.Exec("INSERT INTO users (username, display_name, color) VALUES (?, ?, ?)", username, displayName, color)
	} else {
		db.Exec("UPDATE users SET username=?, display_name=?, color=? WHERE id=?", username, displayName, color, id)
	}

	htmxListUsers(c)
}

func htmxDeleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	db.Exec("UPDATE timeline_events SET user_id = 0 WHERE user_id = ?", id)
	db.Exec("DELETE FROM users WHERE id=?", id)

	htmxListUsers(c)
}

func htmxEditUserForm(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	if id == 0 {
		renderTemplate(c.Writer, "user-form", UserRow{Color: "#7c3aed"})
		return
	}

	var u UserRow
	err = db.QueryRow("SELECT id, username, COALESCE(display_name,''), COALESCE(color,'#7c3aed'), 0 FROM users WHERE id=?", id).Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Color, &u.EventCount)
	if err != nil {
		c.String(http.StatusNotFound, "User not found")
		return
	}

	renderTemplate(c.Writer, "user-form", u)
}

func htmxListTrash(c *gin.Context) {
	rows, err := db.Query(`SELECT id, title, event_date, COALESCE(deleted_at,'') FROM timeline_events WHERE deleted_at IS NOT NULL AND deleted_at != '' ORDER BY deleted_at DESC`)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var trash []TrashRow
	for rows.Next() {
		var t TrashRow
		if err := rows.Scan(&t.ID, &t.Title, &t.Date, &t.DeletedAt); err == nil {
			trash = append(trash, t)
		}
	}

	renderTemplate(c.Writer, "trash-list-htmx", trash)
}

func htmxRestoreEvent(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	db.Exec("UPDATE timeline_events SET deleted_at=NULL WHERE id=?", id)
	htmxListTrash(c)
}

func htmxPermanentDelete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	var mediaURL string
	db.QueryRow("SELECT media_url FROM timeline_events WHERE id=?", id).Scan(&mediaURL)

	db.Exec("DELETE FROM timeline_events WHERE id=?", id)

	if mediaURL != "" {
		mediaPath := "./media/" + mediaURL
		go func() {
			names := []string{mediaPath}
			for _, name := range names {
				os.Remove(name)
				os.Remove(name + ".thumb.jpg")
			}
		}()
	}

	htmxListTrash(c)
}

func htmxEmptyTrash(c *gin.Context) {
	db.Exec("DELETE FROM timeline_events WHERE deleted_at IS NOT NULL AND deleted_at != ''")
	htmxListTrash(c)
}
