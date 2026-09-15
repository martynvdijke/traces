package events

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rwcarlsen/goexif/exif"
	"go.opentelemetry.io/otel/attribute"

	"traces/internal/auth"
	"traces/internal/httpx"
	"traces/internal/media"
	"traces/internal/models"
)

func (s *Service) GetEvents(c *gin.Context) {
	ctx, span := httpx.StartSpan(s.tracer(), c, "getEvents")
	defer span.End()

	filters := EventFilters{
		Year:   c.Query("year"),
		Month:  c.Query("month"),
		Tag:    c.Query("tag"),
		UserID: c.Query("user_id"),
		Sort:   c.Query("sort"),
	}
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter"})
			return
		}
		filters.Limit = n
	}

	span.SetAttributes(
		attribute.String("year", filters.Year),
		attribute.String("month", filters.Month),
		attribute.String("tag", filters.Tag),
		attribute.String("user_id", filters.UserID),
	)

	query, args := BuildEventQuery(filters)

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	evs := ScanEvents(rows)
	span.SetAttributes(attribute.Int("event_count", len(evs)))
	c.JSON(http.StatusOK, evs)
	_ = ctx
}

func (s *Service) GetEventsFull(c *gin.Context) {
	query, _ := BuildEventQuery(EventFilters{Sort: "asc"})
	rows, err := s.DB.Query(query)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	evs := ScanEvents(rows)
	c.JSON(http.StatusOK, evs)
}

func (s *Service) GetPublicEvents(c *gin.Context) {
	shareToken := c.Query("share")
	year := c.Query("year")
	month := c.Query("month")

	var eventIDs string
	var shareYear string

	if shareToken != "" {
		s.DB.QueryRow("SELECT event_ids, year FROM share_tokens WHERE token = ?", shareToken).Scan(&eventIDs, &shareYear)
		if year == "" {
			year = shareYear
		}
	} else if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	var query string
	var args []any

	if eventIDs != "" {
		idStrs := strings.Split(eventIDs, ",")
		placeholders := make([]string, len(idStrs))
		idArgs := make([]any, len(idStrs))
		for i, idStr := range idStrs {
			id, err := strconv.Atoi(strings.TrimSpace(idStr))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid share token"})
				return
			}
			placeholders[i] = "?"
			idArgs[i] = id
		}
		query = `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND e.id IN (` + strings.Join(placeholders, ",") + `) ORDER BY e.event_date ASC`
		args = idArgs
	} else if s.isPublicMode() {
		query = `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '')`
		if year != "" {
			query += " AND strftime('%Y', e.event_date) = ?"
			args = append(args, year)
		}
		if month != "" {
			query += " AND strftime('%m', e.event_date) = ?"
			args = append(args, month)
		}
		query += " ORDER BY e.event_date ASC"
	} else {
		query = `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND e.is_public = 1`
		if year != "" {
			query += " AND strftime('%Y', e.event_date) = ?"
			args = append(args, year)
		}
		if month != "" {
			query += " AND strftime('%m', e.event_date) = ?"
			args = append(args, month)
		}
		query += " ORDER BY e.event_date ASC"
	}

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	evs := ScanEventsWithPerson(rows)
	c.JSON(http.StatusOK, evs)
}

func (s *Service) GetContributions(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	rows, err := s.DB.Query(`SELECT event_date FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date) = ?`, year)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	contributions := make(map[string]int)
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err == nil {
			contributions[date]++
		}
	}
	c.JSON(http.StatusOK, contributions)
}

func (s *Service) SaveEvent(c *gin.Context) {
	_, span := httpx.StartSpan(s.tracer(), c, "saveEvent")
	defer span.End()

	var e models.TimelineEvent
	if err := c.ShouldBindJSON(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if e.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
		return
	}
	if e.Date == "" {
		e.Date = time.Now().Format("2006-01-02")
	}
	if cu := auth.GetCurrentUser(c); cu.ID != 0 {
		e.UserID = int(cu.ID)
	}
	span.SetAttributes(
		attribute.Int("event.id", e.ID),
		attribute.String("event.title", e.Title),
		attribute.String("event.date", e.Date),
	)
	log.Printf("[EVENT] Saving event: ID=%d, Title=%s, Date=%s", e.ID, e.Title, e.Date)
	action := "created"
	if e.ID == 0 {
		result, err := s.DB.Exec(`INSERT INTO timeline_events
			(title, description, event_date, location, media_type, media_url, thumbnail, media_caption, tags, sort_order, is_public, is_favorite, person_id, latitude, longitude, recurring, weather_data, event_start_time, event_end_time, user_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.SortOrder, e.IsPublic, e.IsFavorite, e.PersonID, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		e.ID = int(id)
	} else {
		_, err := s.DB.Exec(`UPDATE timeline_events SET
			title=?, description=?, event_date=?, location=?, media_type=?, media_url=?, thumbnail=?, media_caption=?, tags=?, sort_order=?, is_public=?, is_favorite=?, person_id=?, latitude=?, longitude=?, recurring=?, weather_data=?, event_start_time=?, event_end_time=?, user_id=?
			WHERE id=?`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.SortOrder, e.IsPublic, e.IsFavorite, e.PersonID, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID, e.ID)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		action = "updated"
	}
	span.SetAttributes(attribute.String("action", action))
	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification(fmt.Sprintf("Event %s: %s (%s)", action, e.Title, e.Date), e.Description)
	}
	c.JSON(http.StatusOK, e)
}

func (s *Service) DeleteEvent(c *gin.Context) {
	_, span := httpx.StartSpan(s.tracer(), c, "deleteEvent")
	defer span.End()
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}
	span.SetAttributes(attribute.Int("event.id", id))
	var title string
	s.DB.QueryRow("SELECT title FROM timeline_events WHERE id=?", id).Scan(&title)
	_, err = s.DB.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=?", id)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification(fmt.Sprintf("Event deleted: %s", title), "")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) GetTrashEvents(c *gin.Context) {
	rows, err := s.DB.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time, e.deleted_at,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE e.deleted_at != '' AND e.deleted_at IS NOT NULL ORDER BY e.deleted_at DESC`)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	events := make([]models.TimelineEvent, 0)
	for rows.Next() {
		var e models.TimelineEvent
		var p models.Person
		var personID sql.NullInt64
		var lat, lng sql.NullFloat64
		var pID sql.NullInt64
		var pName, pAvatar, pBio, pBirth, pColor, pCreated sql.NullString
		var title, desc, date, location, mediaType, thumbnail, mediaCaption, mediaURL, tags, recurring, weatherData, startTime, endTime, deletedAt sql.NullString
		var sortOrder sql.NullInt64
		var isFav, isPub sql.NullBool
		var createdAt sql.NullString
		var userID sql.NullInt64
		err := rows.Scan(&e.ID, &title, &desc, &date, &location, &mediaType, &mediaURL, &thumbnail, &mediaCaption, &tags, &sortOrder, &isPub, &isFav, &createdAt, &personID, &lat, &lng, &recurring, &weatherData, &userID, &startTime, &endTime, &deletedAt,
			&pID, &pName, &pAvatar, &pBio, &pBirth, &pColor, &pCreated)
		if err != nil {
			continue
		}
		e.Title = title.String
		e.Description = desc.String
		e.Date = date.String
		e.Location = location.String
		e.MediaType = mediaType.String
		e.MediaURL = mediaURL.String
		e.Thumbnail = thumbnail.String
		e.MediaCaption = mediaCaption.String
		e.Tags = tags.String
		e.SortOrder = int(sortOrder.Int64)
		e.IsPublic = isPub.Bool
		e.IsFavorite = isFav.Bool
		e.CreatedAt = createdAt.String
		e.Recurring = recurring.String
		e.WeatherData = weatherData.String
		e.StartTime = startTime.String
		e.EndTime = endTime.String
		e.DeletedAt = deletedAt.String
		e.UserID = int(userID.Int64)
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
	c.JSON(http.StatusOK, events)
}

func (s *Service) RestoreEvents(c *gin.Context) {
	var input struct {
		IDs []int `json:"ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if len(input.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No event IDs provided"})
		return
	}
	placeholders := make([]string, len(input.IDs))
	args := make([]any, len(input.IDs))
	for i, id := range input.IDs {
		placeholders[i] = "?"
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")
	_, err := s.DB.Exec("UPDATE timeline_events SET deleted_at='' WHERE id IN ("+inClause+")", args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "restored": len(input.IDs)})
}

func (s *Service) EmptyTrash(c *gin.Context) {
	res, err := s.DB.Exec("DELETE FROM timeline_events WHERE deleted_at != '' AND deleted_at IS NOT NULL")
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	count, _ := res.RowsAffected()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "permanently_deleted": count})
}

func (s *Service) HandleUpload(c *gin.Context) {
	_, span := httpx.StartSpan(s.tracer(), c, "handleUpload")
	defer span.End()
	mediaType := c.PostForm("media_type")
	if mediaType == "" {
		mediaType = "image"
	}
	span.SetAttributes(attribute.String("media_type", mediaType))
	var formKey string
	switch mediaType {
	case "video":
		formKey = "video"
	case "audio":
		formKey = "audio"
	default:
		formKey = "image"
	}
	file, err := c.FormFile(formKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	span.SetAttributes(attribute.String("filename", file.Filename))
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string][]string{
		"image": {".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".svg", ".bmp", ".tiff", ".tif"},
		"video": {".mp4", ".webm", ".mov", ".avi", ".mkv", ".flv", ".wmv", ".m4v", ".3gp", ".ogv"},
		"audio": {".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a", ".wma", ".opus", ".oga", ".mid", ".midi"},
	}
	validExt := slices.Contains(allowedExts[mediaType], ext)
	if !validExt {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file type"})
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}
	mimeType := http.DetectContentType(data)
	if mediaType == "image" && !strings.HasPrefix(mimeType, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File content does not match image type"})
		return
	}
	if mediaType == "video" && !strings.HasPrefix(mimeType, "video/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File content does not match video type"})
		return
	}
	if mediaType == "audio" && !strings.HasPrefix(mimeType, "audio/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File content does not match audio type"})
		return
	}
	hash := sha256.Sum256(data)
	hashStr := fmt.Sprintf("%x", hash)
	subDir := hashStr[:2]
	mediaPath := ""
	if s.Media != nil {
		mediaPath = s.Media.MediaPath()
	}
	dir := filepath.Join(mediaPath, subDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}
	filename := hashStr + ext
	uploadPath := filepath.Join(dir, filename)
	url := "/media/" + subDir + "/" + filename
	var thumbnailURL string
	variants := map[string]string{}
	if mediaType == "image" && ext != ".gif" && ext != ".svg" && ext != ".tiff" && ext != ".tif" {
		img, format, err := image.Decode(bytes.NewReader(data))
		if err == nil {
			size := img.Bounds().Size()
			if size.X > 10000 || size.Y > 10000 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Image dimensions too large (max 10000x10000)"})
				return
			}
			if size.X > media.FullMaxDim || size.Y > media.FullMaxDim {
				img = media.ResizeImage(img, media.FullMaxDim)
			}
			if s.Media != nil {
				if err := s.Media.SaveImage(uploadPath, img, format); err != nil {
					os.WriteFile(uploadPath, data, 0644)
				}
				variants = s.Media.WriteImageVariants(mediaPath, subDir, hashStr, ext, format, img)
				if v, ok := variants["thumb"]; ok {
					thumbnailURL = v
				}
			} else {
				os.WriteFile(uploadPath, data, 0644)
			}
		} else {
			os.WriteFile(uploadPath, data, 0644)
		}
	} else {
		if err := os.WriteFile(uploadPath, data, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
			return
		}
		if mediaType == "video" && s.Media != nil {
			thumbnailURL = s.Media.ExtractVideoPoster(uploadPath, hashStr, subDir)
		}
	}
	var exifLat, exifLng *float64
	if mediaType == "image" {
		exifLat, exifLng = extractEXIFGPS(data)
	}
	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification(fmt.Sprintf("New media uploaded: %s (%s)", filename, mediaType), url)
	}
	resp := gin.H{
		"url":        url,
		"media_type": mediaType,
		"thumbnail":  thumbnailURL,
	}
	if len(variants) > 0 {
		resp["variants"] = variants
	}
	if exifLat != nil && exifLng != nil {
		resp["latitude"] = *exifLat
		resp["longitude"] = *exifLng
		if s.Media != nil {
			resp["location_suggestion"] = s.Media.ReverseGeocode(*exifLat, *exifLng)
		}
	}
	c.JSON(http.StatusOK, resp)
}

func extractEXIFGPS(data []byte) (*float64, *float64) {
	ex, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil
	}
	lat, lng, err := ex.LatLong()
	if err != nil {
		return nil, nil
	}
	return &lat, &lng
}

func (s *Service) CloneEvent(c *gin.Context) {
	var input struct {
		ID   int    `json:"id"`
		Date string `json:"date"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var e models.TimelineEvent
	var thumbnail, mediaURL, tags, recurring, weatherData sql.NullString
	err := s.DB.QueryRow(`SELECT title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, event_start_time, event_end_time, user_id FROM timeline_events WHERE id = ?`, input.ID).
		Scan(&e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &mediaURL, &thumbnail, &tags, &e.SortOrder, &recurring, &weatherData, &e.StartTime, &e.EndTime, &e.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}
	e.MediaURL = mediaURL.String
	e.Thumbnail = thumbnail.String
	e.Tags = tags.String
	e.Recurring = recurring.String
	e.WeatherData = weatherData.String
	e.Date = input.Date
	e.ID = 0
	_, err = s.DB.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, event_start_time, event_end_time, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification(fmt.Sprintf("Event cloned: %s", e.Title), "")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) ImportEvents(c *gin.Context) {
	format := c.Query("format")
	if format == "" {
		format = "json"
	}
	var evs []models.TimelineEvent
	if format == "csv" {
		file, _, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer file.Close()
		reader := csv.NewReader(file)
		records, err := reader.ReadAll()
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		for i, record := range records {
			if i == 0 {
				continue
			}
			if len(record) < 4 {
				continue
			}
			e := models.TimelineEvent{
				Title:       record[0],
				Description: record[1],
				Date:        record[2],
				Location:    record[3],
				MediaType:   "image",
			}
			if len(record) > 4 {
				e.Tags = record[4]
			}
			if len(record) > 5 {
				if lat, err := strconv.ParseFloat(record[5], 64); err == nil {
					e.Latitude = &lat
				}
			}
			if len(record) > 6 {
				if lng, err := strconv.ParseFloat(record[6], 64); err == nil {
					e.Longitude = &lng
				}
			}
			if len(record) > 7 {
				e.Recurring = record[7]
			}
			if len(record) > 8 {
				if uid, err := strconv.Atoi(record[8]); err == nil {
					e.UserID = uid
				}
			}
			evs = append(evs, e)
		}
	} else {
		if err := c.ShouldBindJSON(&evs); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	count := 0
	for _, e := range evs {
		_, err := s.DB.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, latitude, longitude, recurring, weather_data, event_start_time, event_end_time, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID)
		if err == nil {
			count++
		}
	}
	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification(fmt.Sprintf("Imported %d events", count), "")
	}
	c.JSON(http.StatusOK, gin.H{"imported": count})
}

func (s *Service) ExportEvents(c *gin.Context) {
	year := c.Query("year")
	format := c.Query("format")
	if format == "" {
		format = "json"
	}
	sqlStr := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '')`
	args := []any{}
	if year != "" {
		sqlStr += " AND strftime('%Y', e.event_date) = ?"
		args = append(args, year)
	}
	sqlStr += " ORDER BY e.event_date ASC"
	rows, err := s.DB.Query(sqlStr, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	evs := ScanEventsWithPerson(rows)
	if format == "csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=events.csv")
		c.String(http.StatusOK, "Title,Description,Date,Location,MediaType,Tags,Latitude,Longitude,Recurring,UserID\n")
		for _, e := range evs {
			lat, lng := "", ""
			if e.Latitude != nil {
				lat = fmt.Sprintf("%f", *e.Latitude)
			}
			if e.Longitude != nil {
				lng = fmt.Sprintf("%f", *e.Longitude)
			}
			c.Writer.WriteString(fmt.Sprintf("%q,%q,%s,%q,%s,%s,%s,%s,%s,%d\n", e.Title, e.Description, e.Date, e.Location, e.MediaType, e.Tags, lat, lng, e.Recurring, e.UserID))
		}
		return
	}
	c.JSON(http.StatusOK, evs)
}

func (s *Service) GetEventsICS(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	sqlStr := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time
		FROM timeline_events e
		WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND strftime('%Y', e.event_date) = ?
		ORDER BY e.event_date ASC`
	rows, err := s.DB.Query(sqlStr, year)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	now := time.Now().UTC().Format("20060102T150405Z")
	prodid := "-//TRACES//Events " + year + "//EN"
	var ics strings.Builder
	ics.WriteString("BEGIN:VCALENDAR\r\n")
	ics.WriteString("VERSION:2.0\r\n")
	ics.WriteString("PRODID:" + prodid + "\r\n")
	ics.WriteString("CALSCALE:GREGORIAN\r\n")
	ics.WriteString("METHOD:PUBLISH\r\n")
	ics.WriteString("X-WR-CALNAME:TRACES " + year + "\r\n")
	for rows.Next() {
		var id int
		var title, desc, date, location, mediaType, recurring, weatherData, startTime, endTime string
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&id, &title, &desc, &date, &location, &mediaType, &lat, &lng, &recurring, &weatherData, &startTime, &endTime); err != nil {
			continue
		}
		uid := fmt.Sprintf("%d-%s@traces", id, date)
		summary := escapeICal(title)
		description := escapeICal(strings.ReplaceAll(desc, "\n", "\\n"))
		ics.WriteString("BEGIN:VEVENT\r\n")
		ics.WriteString("UID:" + uid + "\r\n")
		ics.WriteString("DTSTAMP:" + now + "\r\n")
		if startTime != "" {
			st := strings.ReplaceAll(date, "-", "") + "T" + strings.ReplaceAll(startTime, ":", "") + "00"
			ics.WriteString("DTSTART:" + st + "\r\n")
			if endTime != "" {
				et := strings.ReplaceAll(date, "-", "") + "T" + strings.ReplaceAll(endTime, ":", "") + "00"
				ics.WriteString("DTEND:" + et + "\r\n")
			} else {
				ics.WriteString("DTEND:" + st + "\r\n")
			}
		} else {
			ics.WriteString("DTSTART;VALUE=DATE:" + strings.ReplaceAll(date, "-", "") + "\r\n")
		}
		ics.WriteString("SUMMARY:" + summary + "\r\n")
		if description != "" {
			ics.WriteString("DESCRIPTION:" + description + "\r\n")
		}
		if location != "" {
			ics.WriteString("LOCATION:" + escapeICal(location) + "\r\n")
		}
		if lat.Valid && lng.Valid {
			ics.WriteString("GEO:" + fmt.Sprintf("%.6f;%.6f", lat.Float64, lng.Float64) + "\r\n")
		}
		switch recurring {
		case "daily":
			ics.WriteString("RRULE:FREQ=DAILY\r\n")
		case "weekly":
			ics.WriteString("RRULE:FREQ=WEEKLY\r\n")
		case "monthly":
			ics.WriteString("RRULE:FREQ=MONTHLY\r\n")
		case "yearly":
			ics.WriteString("RRULE:FREQ=YEARLY\r\n")
		}
		ics.WriteString("END:VEVENT\r\n")
	}
	ics.WriteString("END:VCALENDAR\r\n")
	c.Header("Content-Type", "text/calendar; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=traces-%s.ics", year))
	c.String(http.StatusOK, ics.String())
}

func escapeICal(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\r\n", "\\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func (s *Service) ToggleFavorite(c *gin.Context) {
	var input struct {
		ID int `json:"id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var current bool
	s.DB.QueryRow("SELECT is_favorite FROM timeline_events WHERE id=?", input.ID).Scan(&current)
	_, err := s.DB.Exec("UPDATE timeline_events SET is_favorite=? WHERE id=?", !current, input.ID)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "is_favorite": !current})
}

func (s *Service) BatchEvents(c *gin.Context) {
	var input struct {
		IDs      []int  `json:"ids"`
		Action   string `json:"action"`
		Tags     string `json:"tags,omitempty"`
		PersonID *int   `json:"person_id,omitempty"`
		UserID   *int   `json:"user_id,omitempty"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(input.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No event IDs provided"})
		return
	}
	placeholders := make([]string, len(input.IDs))
	args := make([]any, len(input.IDs))
	for i, id := range input.IDs {
		placeholders[i] = "?"
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")
	switch input.Action {
	case "delete":
		_, err := s.DB.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id IN ("+inClause+")", args...)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": len(input.IDs)})
	case "permanent_delete":
		_, err := s.DB.Exec("DELETE FROM timeline_events WHERE id IN ("+inClause+")", args...)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": len(input.IDs)})
	case "add_tags":
		for _, id := range input.IDs {
			var existing string
			s.DB.QueryRow("SELECT COALESCE(tags,'') FROM timeline_events WHERE id=?", id).Scan(&existing)
			newTags := existing
			if input.Tags != "" {
				if existing != "" {
					newTags = existing + ", " + input.Tags
				} else {
					newTags = input.Tags
				}
			}
			s.DB.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(input.IDs)})
	case "set_person":
		if input.PersonID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "person_id required"})
			return
		}
		_, err := s.DB.Exec("UPDATE timeline_events SET person_id=? WHERE id IN ("+inClause+")", append([]any{*input.PersonID}, args...)...)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(input.IDs)})
	case "set_user":
		if input.UserID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user_id required"})
			return
		}
		_, err := s.DB.Exec("UPDATE timeline_events SET user_id=? WHERE id IN ("+inClause+")", append([]any{*input.UserID}, args...)...)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(input.IDs)})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown action"})
	}
}

func (s *Service) GenerateRecurringEvents(c *gin.Context) {
	var input struct {
		EventID   int    `json:"event_id"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var e models.TimelineEvent
	var thumbnail, mediaURL, tags, recurring, weatherData sql.NullString
	err := s.DB.QueryRow(`SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, event_start_time, event_end_time, user_id FROM timeline_events WHERE id = ?`, input.EventID).
		Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &mediaURL, &thumbnail, &tags, &e.SortOrder, &recurring, &weatherData, &e.StartTime, &e.EndTime, &e.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}
	e.MediaURL = mediaURL.String
	e.Thumbnail = thumbnail.String
	e.Tags = tags.String
	e.Recurring = recurring.String
	e.WeatherData = weatherData.String
	if e.Recurring == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Event is not recurring"})
		return
	}
	start, err := time.Parse("2006-01-02", input.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start date"})
		return
	}
	end, err := time.Parse("2006-01-02", input.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end date"})
		return
	}
	if end.Sub(start).Hours() > 365*24 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Date range exceeds 365 days"})
		return
	}
	originalDate, err := time.Parse("2006-01-02", e.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event date"})
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer tx.Rollback()
	generated := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		shouldGenerate := false
		switch e.Recurring {
		case "daily":
			shouldGenerate = true
		case "weekly":
			shouldGenerate = d.Weekday() == originalDate.Weekday()
		case "monthly":
			shouldGenerate = d.Day() == originalDate.Day()
		case "yearly":
			shouldGenerate = d.Month() == originalDate.Month() && d.Day() == originalDate.Day()
		}
		if shouldGenerate {
			var existing int
			tx.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE event_date = ? AND user_id = ? AND id = ?", d.Format("2006-01-02"), e.UserID, e.ID).Scan(&existing)
			if existing == 0 {
				_, err := tx.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, event_start_time, event_end_time, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					e.Title, e.Description, d.Format("2006-01-02"), e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID)
				if err == nil {
					generated++
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"generated": generated})
}
