package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"traces/internal/httpx"
	"traces/internal/models"
)

var (
	// ErrLegacyAdmin is returned for the undeletable legacy root admin (user 0).
	ErrLegacyAdmin = errors.New("the legacy root administrator account cannot be deleted")
	// ErrLastAdmin is returned when deleting an account would leave no admin.
	ErrLastAdmin = errors.New("cannot delete the last administrator account")
	// ErrUserNotFound is returned when the target account does not exist.
	ErrUserNotFound = errors.New("user not found")
)

type AccountProfile struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Color       string `json:"color"`
	AvatarURL   string `json:"avatar_url"`
	AuthMethod  string `json:"auth_method"`
	CreatedAt   string `json:"created_at"`
	IsAdmin     bool   `json:"is_admin"`
}

// AccountExport is the machine-readable response of GET /api/me/export.
// It intentionally contains no password hashes, OIDC client secrets, session
// identifiers, reset token hashes, or integration credentials.
type AccountExport struct {
	ExportedAt     string                 `json:"exported_at"`
	Account        AccountProfile         `json:"account"`
	Events         []models.TimelineEvent `json:"events"`
	Persons        []models.Person        `json:"persons"`
	EventTemplates []models.EventTemplate `json:"event_templates"`
	Preferences    map[string]any         `json:"preferences"`
}

// deleteConfirmation accepts the typed confirmation from a JSON body, a form
// field, or (for scripted admin calls) a query parameter.
func deleteConfirmation(c *gin.Context) string {
	var raw string
	if strings.Contains(c.GetHeader("Content-Type"), "application/json") {
		var input struct {
			Confirm string `json:"confirm"`
		}
		if err := c.ShouldBindJSON(&input); err == nil {
			raw = input.Confirm
		}
	} else {
		raw = c.PostForm("confirm")
		if raw == "" {
			raw = c.Query("confirm")
		}
	}
	return strings.TrimSpace(raw)
}

// DeleteOwnAccount handles DELETE /api/me and POST /settings/account/delete.
// The caller must type DELETE exactly; the in-app flow sends {"confirm":"DELETE"}.
func (s *Service) DeleteOwnAccount(c *gin.Context) {
	cu := GetCurrentUser(c)
	if cu.ID == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": ErrLegacyAdmin.Error()})
		return
	}
	if deleteConfirmation(c) != "DELETE" {
		c.JSON(http.StatusBadRequest, gin.H{"error": `Type DELETE to confirm account deletion`})
		return
	}
	deletedEvents, err := s.DeleteAccount(cu.ID)
	if err != nil {
		s.writeDeleteError(c, err)
		return
	}
	// DeleteAccount already revoked every session for the user; clear the
	// browser cookie as well so the client does not keep presenting a dead id.
	if cookie, err := c.Cookie("session"); err == nil && cookie != "" {
		if s.sessions != nil {
			s.sessions.Delete(cookie)
		}
	}
	s.setSessionCookie(c, "", -1)
	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"deleted_events": deletedEvents,
		"message":        "Your account and personal timeline data have been deleted.",
	})
}

func (s *Service) writeDeleteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrLegacyAdmin):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, ErrLastAdmin):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		httpx.ServerError(c, err)
	}
}

// DeleteAccount hard-deletes an account and its user-owned content. Persons
// are shared instance-wide records with no ownership column, so they are kept
// (see docs/legal.md). Timeline events are deleted rather than reassigned to
// user 0, which would only pseudonymise them.
func (s *Service) DeleteAccount(userID int64) (int, error) {
	if userID == 0 {
		return 0, ErrLegacyAdmin
	}

	var isAdmin int
	err := s.db.QueryRow("SELECT COALESCE(is_admin, 0) FROM users WHERE id = ?", userID).Scan(&isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrUserNotFound
	}
	if err != nil {
		return 0, err
	}
	if isAdmin == 1 {
		var adminCount int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE COALESCE(is_admin, 0) = 1").Scan(&adminCount); err != nil {
			return 0, err
		}
		if adminCount <= 1 {
			return 0, ErrLastAdmin
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var mediaURLs []string
	rows, err := tx.Query("SELECT COALESCE(media_url, ''), COALESCE(thumbnail, '') FROM timeline_events WHERE user_id = ?", userID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var mediaURL, thumbnail string
		if err := rows.Scan(&mediaURL, &thumbnail); err != nil {
			rows.Close()
			return 0, err
		}
		if mediaURL != "" {
			mediaURLs = append(mediaURLs, mediaURL)
		}
		if thumbnail != "" && thumbnail != mediaURL {
			mediaURLs = append(mediaURLs, thumbnail)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(`DELETE FROM collection_events WHERE event_id IN (SELECT id FROM timeline_events WHERE user_id = ?)`, userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM event_people WHERE event_id IN (SELECT id FROM timeline_events WHERE user_id = ?)`, userID); err != nil {
		return 0, err
	}
	result, err := tx.Exec("DELETE FROM timeline_events WHERE user_id = ?", userID)
	if err != nil {
		return 0, err
	}
	deletedEvents, _ := result.RowsAffected()

	if _, err := tx.Exec("DELETE FROM event_templates WHERE user_id = ?", userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM password_reset_tokens WHERE user_id = ?", userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM users WHERE id = ?", userID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	// Clear the hot session cache after the durable rows are gone.
	if s.sessions != nil {
		s.sessions.DeleteForUser(userID)
	}
	s.removeMedia(mediaURLs)
	return int(deletedEvents), nil
}

// removeMedia deletes the stored file plus sibling variants for a media URL.
// Only paths under the configured media directory are touched.
func (s *Service) removeMedia(urls []string) {
	if len(urls) == 0 || s.mediaPath == nil {
		return
	}
	base := s.mediaPath()
	if base == "" {
		return
	}
	for _, mediaURL := range urls {
		if !strings.HasPrefix(mediaURL, "/media/") {
			continue
		}
		rel := strings.TrimPrefix(mediaURL, "/media/")
		clean := filepath.Clean(rel)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			continue
		}
		full := filepath.Join(base, clean)
		if relCheck, err := filepath.Rel(base, full); err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
			continue
		}
		stem := strings.TrimSuffix(full, filepath.Ext(full))
		if matches, err := filepath.Glob(stem + "*"); err == nil {
			for _, match := range matches {
				if info, err := os.Lstat(match); err == nil && !info.IsDir() {
					_ = os.Remove(match)
				}
			}
		}
		_ = os.Remove(full)
		_ = os.Remove(full + ".thumb.jpg")
	}
}

// ExportOwnAccount handles GET /api/me/export with a requester-scoped export.
func (s *Service) ExportOwnAccount(c *gin.Context) {
	cu := GetCurrentUser(c)
	export, err := s.BuildAccountExport(cu.ID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		httpx.ServerError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="traces-export.json"`)
	c.JSON(http.StatusOK, export)
}

// BuildAccountExport collects the requester's account, own events, related
// person records, templates, and empty server-side preferences.
func (s *Service) BuildAccountExport(userID int64) (*AccountExport, error) {
	profile, err := s.accountProfile(userID)
	if err != nil {
		return nil, err
	}
	preferences := map[string]any{"analytics_opt_out": false}
	if userID > 0 {
		var optOut int
		if err := s.db.QueryRow("SELECT COALESCE(analytics_opt_out, 0) FROM users WHERE id = ?", userID).Scan(&optOut); err == nil {
			preferences["analytics_opt_out"] = optOut == 1
		}
	}

	export := &AccountExport{
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		Account:        profile,
		Events:         []models.TimelineEvent{},
		Persons:        []models.Person{},
		EventTemplates: []models.EventTemplate{},
		Preferences:    preferences,
	}

	rows, err := s.db.Query(`SELECT id, COALESCE(title,''), COALESCE(description,''), COALESCE(event_date,''),
		COALESCE(location,''), COALESCE(media_type,''), COALESCE(media_url,''), COALESCE(thumbnail,''),
		COALESCE(media_caption,''), COALESCE(tags,''), COALESCE(sort_order,0), COALESCE(is_public,0),
		COALESCE(is_favorite,0), COALESCE(created_at,''), person_id, latitude, longitude,
		COALESCE(recurring,''), COALESCE(weather_data,''), COALESCE(user_id,0),
		COALESCE(event_start_time,''), COALESCE(event_end_time,''), COALESCE(deleted_at,'')
		FROM timeline_events WHERE user_id = ? ORDER BY event_date ASC, id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e models.TimelineEvent
		var personID sql.NullInt64
		var lat, lng sql.NullFloat64
		var isPublic, isFav int
		if err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType,
			&e.MediaURL, &e.Thumbnail, &e.MediaCaption, &e.Tags, &e.SortOrder, &isPublic,
			&isFav, &e.CreatedAt, &personID, &lat, &lng, &e.Recurring, &e.WeatherData,
			&e.UserID, &e.StartTime, &e.EndTime, &e.DeletedAt); err != nil {
			return nil, err
		}
		e.IsPublic = isPublic == 1
		e.IsFavorite = isFav == 1
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
		export.Events = append(export.Events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	personRows, err := s.db.Query(`SELECT DISTINCT p.id, p.name, COALESCE(p.avatar_url,''), COALESCE(p.bio,''),
		COALESCE(p.birth_date,''), COALESCE(p.color,''), COALESCE(p.created_at,'')
		FROM persons p JOIN timeline_events e ON e.person_id = p.id
		WHERE e.user_id = ? ORDER BY p.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer personRows.Close()
	for personRows.Next() {
		var p models.Person
		if err := personRows.Scan(&p.ID, &p.Name, &p.AvatarURL, &p.Bio, &p.BirthDate, &p.Color, &p.CreatedAt); err != nil {
			return nil, err
		}
		export.Persons = append(export.Persons, p)
	}
	if err := personRows.Err(); err != nil {
		return nil, err
	}

	templateRows, err := s.db.Query(`SELECT id, title, COALESCE(description,''), COALESCE(tags,''),
		COALESCE(person_id,0), COALESCE(user_id,0), COALESCE(location,''), COALESCE(media_type,''),
		COALESCE(created_at,'') FROM event_templates WHERE user_id = ? ORDER BY title ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer templateRows.Close()
	for templateRows.Next() {
		var t models.EventTemplate
		if err := templateRows.Scan(&t.ID, &t.Title, &t.Description, &t.Tags, &t.PersonID,
			&t.UserID, &t.Location, &t.MediaType, &t.CreatedAt); err != nil {
			return nil, err
		}
		export.EventTemplates = append(export.EventTemplates, t)
	}
	if err := templateRows.Err(); err != nil {
		return nil, err
	}

	return export, nil
}

func (s *Service) accountProfile(userID int64) (AccountProfile, error) {
	if userID == 0 {
		var username string
		err := s.db.QueryRow("SELECT username FROM admin_users ORDER BY id ASC LIMIT 1").Scan(&username)
		if errors.Is(err, sql.ErrNoRows) {
			username = "admin"
		} else if err != nil {
			return AccountProfile{}, err
		}
		return AccountProfile{
			ID:          0,
			Username:    username,
			DisplayName: "Legacy administrator",
			Color:       models.DefaultColor,
			AuthMethod:  "password",
			IsAdmin:     true,
		}, nil
	}

	var p AccountProfile
	var isAdmin int
	err := s.db.QueryRow(`SELECT id, username, COALESCE(display_name,''), COALESCE(email,''),
		COALESCE(color,''), COALESCE(avatar_url,''), COALESCE(auth_method,''),
		COALESCE(created_at,''), COALESCE(is_admin,0) FROM users WHERE id = ?`, userID).
		Scan(&p.ID, &p.Username, &p.DisplayName, &p.Email, &p.Color, &p.AvatarURL,
			&p.AuthMethod, &p.CreatedAt, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountProfile{}, ErrUserNotFound
	}
	if err != nil {
		return AccountProfile{}, err
	}
	p.IsAdmin = isAdmin == 1
	return p, nil
}
