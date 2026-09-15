package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	"traces/internal/httpx"
	"traces/internal/integrations"
	"traces/internal/logging"
	"traces/internal/models"
)

// CurrentUser is the resolved identity of the logged-in account.
// ID 0 is the admin; any other value is a users.id from a family login.
type CurrentUser struct {
	ID    int64
	Name  string
	Color string
}

const (
	CtxKeyUserID    = "current_user_id"
	CtxKeyUserName  = "current_user_name"
	CtxKeyUserColor = "current_user_color"
)

// GetCurrentUser returns the identity resolved by AuthMiddlewareGin.
func GetCurrentUser(c *gin.Context) CurrentUser {
	id, _ := c.Get(CtxKeyUserID)
	uid, _ := id.(int64)
	name, _ := c.Get(CtxKeyUserName)
	uname, _ := name.(string)
	color, _ := c.Get(CtxKeyUserColor)
	ucolor, _ := color.(string)
	return CurrentUser{ID: uid, Name: uname, Color: ucolor}
}

func GenerateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type Deps struct {
	DB           *sql.DB
	Log          *logging.LogService
	Renderer     *httpx.Renderer
	Sessions     *SessionStore
	Integrations *integrations.Service
	PublicMode   func() bool
}

type Service struct {
	db           *sql.DB
	log          *logging.LogService
	renderer     *httpx.Renderer
	sessions     *SessionStore
	integrations *integrations.Service
	publicMode   func() bool

	oidcCfg      oidcConfig
	oidcMu       sync.Mutex
	oidcProvider *oidc.Provider
	oidcVerifier *oidc.IDTokenVerifier
	oidcOAuth2   *oauth2.Config
}

func New(d Deps) *Service {
	return &Service{
		db:           d.DB,
		log:          d.Log,
		renderer:     d.Renderer,
		sessions:     d.Sessions,
		integrations: d.Integrations,
		publicMode:   d.PublicMode,
	}
}

// SessionsStore exposes the underlying store for tests.
func (s *Service) SessionsStore() *SessionStore { return s.sessions }

func (s *Service) resolveSessionUser(userID int64) CurrentUser {
	if userID != 0 {
		var name, color string
		err := s.db.QueryRow("SELECT COALESCE(NULLIF(display_name,''), username), COALESCE(color, ?) FROM users WHERE id = ?", models.DefaultColor, userID).Scan(&name, &color)
		if err == nil {
			return CurrentUser{ID: userID, Name: name, Color: color}
		}
	}
	return CurrentUser{ID: 0, Name: "Admin", Color: models.DefaultColor}
}

func (s *Service) AuthMiddlewareGin() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		sess, ok := s.sessions.Get(cookie)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			return
		}
		if time.Now().Unix() > sess.ExpiresAt {
			s.sessions.DeleteWithCSRF(cookie)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			return
		}
		cu := s.resolveSessionUser(sess.UserID)
		c.Set(CtxKeyUserID, cu.ID)
		c.Set(CtxKeyUserName, cu.Name)
		c.Set(CtxKeyUserColor, cu.Color)
		c.Set("session_id", cookie)
		c.Next()
	}
}

func (s *Service) CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" {
			c.Next()
			return
		}
		token := c.GetHeader("X-CSRF-Token")
		if token == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "CSRF token required"})
			return
		}
		cookie, _ := c.Cookie("session")
		stored, ok := s.sessions.GetCSRF(cookie)
		if !ok || token != stored {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Invalid CSRF token"})
			return
		}
		c.Next()
	}
}

func (s *Service) GetCSRFToken(c *gin.Context) {
	cookie, _ := c.Cookie("session")
	if cookie == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}
	token, ok := s.sessions.GetCSRF(cookie)
	if !ok {
		token = fmt.Sprintf("%x", sha256.Sum256([]byte(cookie+"-csrf")))
		s.sessions.SetCSRF(cookie, token)
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func (s *Service) HandleLogin(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Setup    bool   `json:"setup"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)

	if input.Setup && count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Setup already completed"})
		return
	}

	if count == 0 {
		if len(input.Password) < 8 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
			return
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		_, dbErr := s.db.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", input.Username, string(hashed))
		if dbErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
		s.db.Exec("INSERT OR IGNORE INTO users (id, username, display_name, email, color) VALUES (1, ?, ?, '', ?)", input.Username, input.Username, models.DefaultColor)
		sessionID, err := GenerateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session"})
			return
		}
		s.sessions.Set(sessionID, SessionInfo{UserID: 0, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()})
		s.sessions.SetCSRF(sessionID, fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"-csrf"))))
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     "session",
			Value:    sessionID,
			Path:     "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	var adminID int
	var adminHash string
	err := s.db.QueryRow("SELECT id, password FROM admin_users WHERE username = ?", input.Username).Scan(&adminID, &adminHash)
	if err == nil {
		if bcryptErr := bcrypt.CompareHashAndPassword([]byte(adminHash), []byte(input.Password)); bcryptErr != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
			return
		}
		s.createSession(c, 0)
		return
	}

	var familyID int64
	var familyHash string
	famErr := s.db.QueryRow("SELECT id, COALESCE(password_hash, '') FROM users WHERE username = ?", input.Username).Scan(&familyID, &familyHash)
	if famErr != nil || familyHash == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(familyHash), []byte(input.Password)); bcryptErr != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}
	s.createSession(c, familyID)
}

func (s *Service) createSession(c *gin.Context, userID int64) {
	sessionID, err := GenerateSessionID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session"})
		return
	}
	s.sessions.Set(sessionID, SessionInfo{UserID: userID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()})
	s.sessions.SetCSRF(sessionID, fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"-csrf"))))
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) HandleLogout(c *gin.Context) {
	cookie, err := c.Cookie("session")
	if err == nil {
		s.sessions.Delete(cookie)
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// User management handlers

func (s *Service) GetUsers(c *gin.Context) {
	rows, err := s.db.Query(`SELECT id, username, display_name, email, color, avatar_url, created_at,
		(SELECT COUNT(*) FROM timeline_events WHERE user_id = users.id) as event_count
		FROM users ORDER BY display_name ASC`)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	users := make([]models.User, 0)
	for rows.Next() {
		var u models.User
		err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Color, &u.AvatarURL, &u.CreatedAt, &u.EventCount)
		if err != nil {
			continue
		}
		users = append(users, u)
	}
	c.JSON(http.StatusOK, users)
}

func (s *Service) SaveUser(c *gin.Context) {
	var u models.User
	if err := c.ShouldBindJSON(&u); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var passwordHash string
	if u.Password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		passwordHash = string(hashed)
	}
	if u.ID == 0 {
		var adminCount int
		s.db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE username = ?", u.Username).Scan(&adminCount)
		if adminCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username already taken"})
			return
		}
		var userCount int
		s.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", u.Username).Scan(&userCount)
		if userCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username already taken"})
			return
		}
		result, err := s.db.Exec("INSERT INTO users (username, display_name, email, color, avatar_url, password_hash) VALUES (?, ?, ?, ?, ?, ?)",
			u.Username, u.DisplayName, u.Email, u.Color, u.AvatarURL, passwordHash)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		u.ID = int(id)
	} else {
		if passwordHash != "" {
			_, err := s.db.Exec("UPDATE users SET username=?, display_name=?, email=?, color=?, avatar_url=?, password_hash=? WHERE id=?",
				u.Username, u.DisplayName, u.Email, u.Color, u.AvatarURL, passwordHash, u.ID)
			if err != nil {
				httpx.ServerError(c, err)
				return
			}
		} else {
			_, err := s.db.Exec("UPDATE users SET username=?, display_name=?, email=?, color=?, avatar_url=? WHERE id=?",
				u.Username, u.DisplayName, u.Email, u.Color, u.AvatarURL, u.ID)
			if err != nil {
				httpx.ServerError(c, err)
				return
			}
		}
	}
	u.Password = ""
	c.JSON(http.StatusOK, u)
}

func (s *Service) DeleteUser(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}
	s.db.Exec("UPDATE timeline_events SET user_id = 0 WHERE user_id = ?", id)
	_, err = s.db.Exec("DELETE FROM users WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) GetUserEvents(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}
	rows, err := s.db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND e.user_id = ? ORDER BY e.event_date ASC`, id)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()
	evs := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, evs)
}

func scanEventsWithPerson(rows *sql.Rows) []models.TimelineEvent {
	evs := make([]models.TimelineEvent, 0)
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
		evs = append(evs, e)
	}
	return evs
}
