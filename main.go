package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/image/draw"
)

func init() {
	image.RegisterFormat("png", "png", png.Decode, png.DecodeConfig)
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
}

type TimelineEvent struct {
	ID           int      `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Date         string   `json:"date"`
	Location     string   `json:"location"`
	MediaType    string   `json:"media_type"`
	MediaURL     string   `json:"media_url"`
	Thumbnail    string   `json:"thumbnail"`
	MediaCaption string   `json:"media_caption"`
	Tags         string   `json:"tags"`
	SortOrder    int      `json:"sort_order"`
	IsPublic     bool     `json:"is_public"`
	CreatedAt    string   `json:"created_at"`
	PersonID     *int     `json:"person_id"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	Person       *Person  `json:"person,omitempty"`
	Recurring    string   `json:"recurring"`
	WeatherData  string   `json:"weather_data"`
	UserID       int      `json:"user_id"`
	User         *User    `json:"user,omitempty"`
}

type EventStats struct {
	Total        int            `json:"total"`
	ByMonth      map[string]int `json:"by_month"`
	ByTag        map[string]int `json:"by_tag"`
	ByMedia      map[string]int `json:"by_media"`
	Locations    int            `json:"locations"`
	Persons      int            `json:"persons"`
	YearOverYear map[string]int `json:"year_over_year"`
	MediaTotal   int            `json:"media_total"`
	WithLocation int            `json:"with_location"`
	PersonCount  int            `json:"person_count"`
	TotalYears   int            `json:"total_years"`
	WithMedia    int            `json:"with_media"`
	WithGeo      int            `json:"with_geo"`
	ByYear       map[string]int `json:"by_year"`
}

type AdminUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Password string `json:"-"`
}

type Person struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url"`
	Bio        string `json:"bio"`
	BirthDate  string `json:"birth_date"`
	Color      string `json:"color"`
	EventCount int    `json:"event_count,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type User struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
	AvatarURL   string `json:"avatar_url"`
	EventCount  int    `json:"event_count,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type GotifyConfig struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
}

type MemoriesConfig struct {
	Enabled      bool `json:"enabled"`
	DaysWindow   int  `json:"days_window"`
	EmailEnabled bool `json:"email_enabled"`
}

type EmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPUser string `json:"smtp_user"`
	SMTPPass string `json:"smtp_pass"`
	FromAddr string `json:"from_addr"`
	ToAddr   string `json:"to_addr"`
}

type OllamaConfig struct {
	URL     string `json:"url"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type WeatherData struct {
	Temperature  float64 `json:"temperature"`
	Condition    string  `json:"condition"`
	Icon         string  `json:"icon"`
	Humidity     float64 `json:"humidity"`
	WindSpeed    float64 `json:"wind_speed"`
	FetchedAt    string  `json:"fetched_at"`
}

type CalendarDay struct {
	Date   string         `json:"date"`
	Events []TimelineEvent `json:"events"`
	Count  int            `json:"count"`
}

const currentSchemaVersion = 7
const currentVersion = "1.8.0"

var (
	publicMode    bool = false
	gotifyEnabled bool
)

var (
	db           *sql.DB
	sessionStore = make(map[string]int64)
	sessionMu    sync.RWMutex
	basePath     = "/app"
	dbPath       = "/db/traces.db"
	mediaPath    = "/app/media"
	gotifyURL    = ""
	gotifyToken  = ""
	umamiURL     = ""
	umamiSiteID  = ""
	currentUserID int
)

func main() {
	if os.Getenv("DOCKER") != "true" {
		basePath = "."
		dbPath = "./traces.db"
		mediaPath = filepath.Join(basePath, "media")
	}

	gotifyURL = os.Getenv("GOTIFY_URL")
	gotifyToken = os.Getenv("GOTIFY_TOKEN")
	if os.Getenv("GOTIFY_ENABLED") == "true" {
		gotifyEnabled = true
	}

	umamiURL = os.Getenv("UMAMI_URL")
	umamiSiteID = os.Getenv("UMAMI_SITE_ID")

	if err := os.MkdirAll(mediaPath, 0755); err != nil {
		log.Printf("Warning: could not create media directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Printf("Warning: could not create database directory: %v", err)
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	initDB()

	r := gin.Default()
	r.MaxMultipartMemory = 32 << 20

	api := r.Group("/api")
	{
		api.GET("/version", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"version": currentVersion})
		})
		api.GET("/check-setup", handleCheckSetup)
		api.POST("/login", handleLogin)
		api.POST("/logout", handleLogout)
		api.GET("/events", getEvents)
		api.GET("/events/full", getEventsFull)
		api.GET("/events/search", searchEvents)
		api.GET("/events/export", exportEvents)
		api.GET("/contributions", getContributions)
		api.GET("/stats", getEventStats)
		api.GET("/tags", getTags)
		api.GET("/public", getPublicEvents)
		api.GET("/share", getShareLink)
		api.GET("/map", getMapData)
		api.GET("/persons", getPersons)
		api.GET("/autocomplete", getAutocomplete)
		api.GET("/calendar", getCalendar)
		api.GET("/users", getUsers)

		auth := api.Group("")
		auth.Use(authMiddlewareGin())
		{
			auth.POST("/events", saveEvent)
			auth.DELETE("/events", deleteEvent)
			auth.POST("/upload", handleUpload)
			auth.POST("/events/clone", cloneEvent)
			auth.POST("/events/import", importEvents)
			auth.POST("/share/create", createShareLink)
			auth.POST("/persons", savePerson)
			auth.DELETE("/persons", deletePerson)
			auth.GET("/persons/:id/events", getPersonEvents)
			auth.GET("/gotify/config", getGotifyConfig)
			auth.POST("/gotify/config", saveGotifyConfig)
			auth.POST("/gotify/test", testGotify)
			auth.GET("/memories", getMemories)
			auth.GET("/memories/config", getMemoriesConfig)
			auth.POST("/memories/config", saveMemoriesConfig)
			auth.POST("/memories/send", sendMemoriesEmailHandler)
			auth.GET("/email/config", getEmailConfig)
			auth.POST("/email/config", saveEmailConfig)
			auth.POST("/email/test", testEmail)
			auth.POST("/weather/fetch", fetchWeather)
			auth.POST("/auto-tag", autoTagEvent)
			auth.POST("/users", saveUser)
			auth.DELETE("/users", deleteUser)
			auth.GET("/users/:id/events", getUserEvents)
			auth.POST("/events/recurring/generate", generateRecurringEvents)
			auth.GET("/ollama/config", getOllamaConfig)
			auth.POST("/ollama/config", saveOllamaConfig)
		}

		api.GET("/config", getPublicConfig)
		api.GET("/manifest.json", serveManifest)
		api.GET("/sw.js", serveServiceWorker)
	}

	r.GET("/admin.html", func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err == nil {
			sessionMu.RLock()
			expiry, ok := sessionStore[cookie]
			sessionMu.RUnlock()
			if ok && time.Now().Unix() <= expiry {
				c.File(filepath.Join(basePath, "static/admin.html"))
				return
			}
		}
		c.Redirect(http.StatusFound, "/login.html")
	})

	r.GET("/login.html", func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err == nil {
			sessionMu.RLock()
			expiry, ok := sessionStore[cookie]
			sessionMu.RUnlock()
			if ok && time.Now().Unix() <= expiry {
				c.Redirect(http.StatusFound, "/admin.html")
				return
			}
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
		if count == 0 {
			c.Redirect(http.StatusFound, "/setup.html")
			return
		}
		c.File(filepath.Join(basePath, "static/login.html"))
	})

	r.GET("/setup.html", func(c *gin.Context) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
		if count > 0 {
			c.Redirect(http.StatusFound, "/login.html")
			return
		}
		c.File(filepath.Join(basePath, "static/setup.html"))
	})

	r.GET("/docs", func(c *gin.Context) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
    <title>TRACES API Documentation</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
        window.onload = function() {
            SwaggerUIBundle({
                url: "/api-docs",
                dom_id: '#swagger-ui',
                presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
                layout: "BaseLayout"
            });
        };
    </script>
</body>
</html>`)
	})

	r.GET("/api-docs", func(c *gin.Context) {
		c.File(filepath.Join(basePath, "static/swagger.json"))
	})

	r.Static("/static", filepath.Join(basePath, "static"))
	r.Static("/media", mediaPath)

	r.GET("/map.html", func(c *gin.Context) {
		c.File(filepath.Join(basePath, "static/map.html"))
	})

	r.GET("/", func(c *gin.Context) {
		c.File(filepath.Join(basePath, "static/index.html"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "6270"
	}

	log.Printf("Server starting on port %s...", port)
	log.Fatal(r.Run(":" + port))
}

func authMiddlewareGin() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		sessionMu.RLock()
		expiry, ok := sessionStore[cookie]
		sessionMu.RUnlock()
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			return
		}
		if time.Now().Unix() > expiry {
			sessionMu.Lock()
			delete(sessionStore, cookie)
			sessionMu.Unlock()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			return
		}
		c.Next()
	}
}

func getPublicConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"umami_url":  umamiURL,
		"umami_site": umamiSiteID,
	})
}

func handleCheckSetup(c *gin.Context) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	c.JSON(http.StatusOK, gin.H{"setup": count > 0})
}

func handleLogin(c *gin.Context) {
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
	db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)

	if input.Setup && count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Setup already completed"})
		return
	}

	if count == 0 {
		hashed, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		_, dbErr := db.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", input.Username, string(hashed))
		if dbErr != nil {
			log.Printf("Error creating admin user: %v", dbErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}

		db.Exec("INSERT OR IGNORE INTO users (id, username, display_name, color) VALUES (1, ?, ?, ?)", input.Username, input.Username, "#7c3aed")

		sessionID, err := generateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session"})
			return
		}
		sessionMu.Lock()
		sessionStore[sessionID] = time.Now().Add(24 * time.Hour).Unix()
		sessionMu.Unlock()
		c.SetCookie("session", sessionID, 86400, "/", "", true, true)
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     "session",
			Value:    sessionID,
			Path:     "/",
			MaxAge:   86400,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	var user AdminUser
	err := db.QueryRow("SELECT id, username, password FROM admin_users WHERE username = ?", input.Username).Scan(&user.ID, &user.Username, &user.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	sessionID, err := generateSessionID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session"})
		return
	}
	sessionMu.Lock()
	sessionStore[sessionID] = time.Now().Add(24 * time.Hour).Unix()
	sessionMu.Unlock()
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleLogout(c *gin.Context) {
	cookie, err := c.Cookie("session")
	if err == nil {
		sessionMu.Lock()
		delete(sessionStore, cookie)
		sessionMu.Unlock()
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getEvents(c *gin.Context) {
	year := c.Query("year")
	month := c.Query("month")
	tag := c.Query("tag")
	limit := c.Query("limit")
	sort := c.Query("sort")
	userID := c.Query("user_id")

	query := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE 1=1`
	args := []interface{}{}

	if year != "" {
		query += " AND strftime('%Y', e.event_date) = ?"
		args = append(args, year)
	}
	if month != "" {
		query += " AND strftime('%m', e.event_date) = ?"
		args = append(args, month)
	}
	if tag != "" {
		query += " AND e.tags LIKE ?"
		args = append(args, "%"+tag+"%")
	}
	if userID != "" {
		query += " AND e.user_id = ?"
		args = append(args, userID)
	}
	if sort == "desc" {
		query += " ORDER BY e.event_date DESC"
	} else {
		query += " ORDER BY e.event_date ASC"
	}
	if limit != "" {
		query += " LIMIT ?"
		l, err := strconv.Atoi(limit)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter"})
			return
		}
		args = append(args, l)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

func getEventsFull(c *gin.Context) {
	query := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id ORDER BY e.event_date ASC`

	rows, err := db.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

func getPublicEvents(c *gin.Context) {
	shareToken := c.Query("share")
	if shareToken == "" && !publicMode {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
		return
	}

	var eventIDs string
	var year string

	if shareToken != "" {
		db.QueryRow("SELECT event_ids, year FROM share_tokens WHERE token = ?", shareToken).Scan(&eventIDs, &year)
	} else {
		year = c.Query("year")
		if year == "" {
			year = fmt.Sprintf("%d", time.Now().Year())
		}
	}

	var query string
	var args []interface{}

	if eventIDs != "" {
		idStrs := strings.Split(eventIDs, ",")
		placeholders := make([]string, len(idStrs))
		idArgs := make([]interface{}, len(idStrs))
		for i, idStr := range idStrs {
			id, err := strconv.Atoi(strings.TrimSpace(idStr))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid share token"})
				return
			}
			placeholders[i] = "?"
			idArgs[i] = id
		}
		query = `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE e.id IN (` + strings.Join(placeholders, ",") + `) ORDER BY e.event_date ASC`
		args = idArgs
	} else {
		query = `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
			p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
			FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE e.is_public = 1`
		if year != "" {
			query += " AND strftime('%Y', e.event_date) = ?"
			args = append(args, year)
		}
		query += " ORDER BY e.event_date ASC"
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

func getContributions(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	rows, err := db.Query(`SELECT event_date FROM timeline_events WHERE strftime('%Y', event_date) = ?`, year)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

func saveEvent(c *gin.Context) {
	var e TimelineEvent
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

	log.Printf("[EVENT] Saving event: ID=%d, Title=%s, Date=%s", e.ID, e.Title, e.Date)

	action := "created"
	if e.ID == 0 {
		result, err := db.Exec(`INSERT INTO timeline_events 
			(title, description, event_date, location, media_type, media_url, thumbnail, media_caption, tags, sort_order, is_public, person_id, latitude, longitude, recurring, weather_data, user_id) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.SortOrder, e.IsPublic, e.PersonID, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		id, _ := result.LastInsertId()
		e.ID = int(id)
	} else {
		_, err := db.Exec(`UPDATE timeline_events SET 
			title=?, description=?, event_date=?, location=?, media_type=?, media_url=?, thumbnail=?, media_caption=?, tags=?, sort_order=?, is_public=?, person_id=?, latitude=?, longitude=?, recurring=?, weather_data=?, user_id=?
			WHERE id=?`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.SortOrder, e.IsPublic, e.PersonID, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.UserID, e.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		action = "updated"
	}

	sendGotifyNotification(fmt.Sprintf("Event %s: %s (%s)", action, e.Title, e.Date), e.Description)
	c.JSON(http.StatusOK, e)
}

func deleteEvent(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	var title string
	db.QueryRow("SELECT title FROM timeline_events WHERE id=?", id).Scan(&title)

	_, err = db.Exec("DELETE FROM timeline_events WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sendGotifyNotification(fmt.Sprintf("Event deleted: %s", title), "")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleUpload(c *gin.Context) {
	mediaType := c.PostForm("media_type")
	if mediaType == "" {
		mediaType = "image"
	}

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

	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string][]string{
		"image": {".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".svg", ".bmp", ".tiff", ".tif"},
		"video": {".mp4", ".webm", ".mov", ".avi", ".mkv", ".flv", ".wmv", ".m4v", ".3gp", ".ogv"},
		"audio": {".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a", ".wma", ".opus", ".oga", ".mid", ".midi"},
	}

	validExt := false
	for _, e := range allowedExts[mediaType] {
		if ext == e {
			validExt = true
			break
		}
	}
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

	hash := sha256.Sum256(data)
	hashStr := fmt.Sprintf("%x", hash)

	subDir := hashStr[:2]
	dir := filepath.Join(mediaPath, subDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}

	filename := hashStr + ext
	uploadPath := filepath.Join(dir, filename)
	url := "/media/" + subDir + "/" + filename
	var thumbnailURL string

	if mediaType == "image" && ext != ".gif" && ext != ".svg" && ext != ".tiff" && ext != ".tif" {
		img, format, err := image.Decode(bytes.NewReader(data))
		if err == nil {
			size := img.Bounds().Size()
			if size.X > 1920 || size.Y > 1920 {
				img = resizeImage(img, 1920)
			}

			if err := saveImage(uploadPath, img, format); err != nil {
				os.WriteFile(uploadPath, data, 0644)
			}

			thumb := resizeImage(img, 300)
			thumbFilename := hashStr + "_thumb" + ext
			if err := saveImage(filepath.Join(dir, thumbFilename), thumb, format); err == nil {
				thumbnailURL = "/media/" + subDir + "/" + thumbFilename
			}
		} else {
			os.WriteFile(uploadPath, data, 0644)
		}
	} else {
		if err := os.WriteFile(uploadPath, data, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
			return
		}
	}

	sendGotifyNotification(fmt.Sprintf("New media uploaded: %s (%s)", filename, mediaType), url)

	c.JSON(http.StatusOK, gin.H{
		"url":        url,
		"media_type": mediaType,
		"thumbnail":  thumbnailURL,
	})
}

func resizeImage(img image.Image, maxDim int) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w <= maxDim && h <= maxDim {
		return img
	}

	ratio := float64(maxDim) / float64(max(w, h))
	newW := int(float64(w) * ratio)
	newH := int(float64(h) * ratio)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	return dst
}

func saveImage(path string, img image.Image, format string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	switch format {
	case "png":
		return png.Encode(f, img)
	default:
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
	}
}

func searchEvents(c *gin.Context) {
	query := c.Query("q")
	year := c.Query("year")
	tag := c.Query("tag")
	person := c.Query("person")
	mediaType := c.Query("media_type")
	location := c.Query("location")
	month := c.Query("month")
	userID := c.Query("user_id")

	sqlStr := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE 1=1`
	args := []interface{}{}

	if query != "" {
		sqlStr += " AND (e.title LIKE ? OR e.description LIKE ? OR e.location LIKE ? OR p.name LIKE ?)"
		like := "%" + query + "%"
		args = append(args, like, like, like, like)
	}
	if year != "" {
		sqlStr += " AND strftime('%Y', e.event_date) = ?"
		args = append(args, year)
	}
	if month != "" {
		sqlStr += " AND strftime('%m', e.event_date) = ?"
		args = append(args, month)
	}
	if tag != "" {
		sqlStr += " AND e.tags LIKE ?"
		args = append(args, "%"+tag+"%")
	}
	if person != "" {
		sqlStr += " AND p.name LIKE ?"
		args = append(args, "%"+person+"%")
	}
	if mediaType != "" {
		sqlStr += " AND e.media_type = ?"
		args = append(args, mediaType)
	}
	if location != "" {
		sqlStr += " AND e.location LIKE ?"
		args = append(args, "%"+location+"%")
	}
	if userID != "" {
		sqlStr += " AND e.user_id = ?"
		args = append(args, userID)
	}

	sqlStr += " ORDER BY e.event_date ASC"

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

func getAutocomplete(c *gin.Context) {
	field := c.Query("field")
	q := c.Query("q")

	var results []string

	switch field {
	case "location":
		rows, err := db.Query(`SELECT DISTINCT location FROM timeline_events WHERE location != '' AND location LIKE ? ORDER BY location LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	case "person":
		rows, err := db.Query(`SELECT name FROM persons WHERE name LIKE ? ORDER BY name LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	case "tag":
		rows, err := db.Query(`SELECT DISTINCT tags FROM timeline_events WHERE tags != '' AND tags LIKE ? ORDER BY tags LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				for _, t := range strings.Split(v, ",") {
					t = strings.TrimSpace(t)
					if t != "" && strings.Contains(strings.ToLower(t), strings.ToLower(q)) {
						results = append(results, t)
					}
				}
			}
		}
		results = uniqueStrings(results)
		if len(results) > 10 {
			results = results[:10]
		}
	case "media":
		rows, err := db.Query(`SELECT DISTINCT media_url FROM timeline_events WHERE media_url != '' AND media_url LIKE ? ORDER BY media_url LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	case "user":
		rows, err := db.Query(`SELECT display_name FROM users WHERE display_name LIKE ? ORDER BY display_name LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	}

	c.JSON(http.StatusOK, results)
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	var r []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			r = append(r, v)
		}
	}
	return r
}

func getPersonEvents(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
		return
	}

	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE e.person_id = ? ORDER BY e.event_date ASC`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

func cloneEvent(c *gin.Context) {
	var input struct {
		ID   int    `json:"id"`
		Date string `json:"date"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var e TimelineEvent
	err := db.QueryRow(`SELECT title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, user_id FROM timeline_events WHERE id = ?`, input.ID).
		Scan(&e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &e.Tags, &e.SortOrder, &e.Recurring, &e.WeatherData, &e.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}

	e.Date = input.Date
	e.ID = 0

	_, err = db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Recurring, e.WeatherData, e.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sendGotifyNotification(fmt.Sprintf("Event cloned: %s", e.Title), "")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func importEvents(c *gin.Context) {
	format := c.Query("format")
	if format == "" {
		format = "json"
	}

	var events []TimelineEvent
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		for i, record := range records {
			if i == 0 {
				continue
			}
			if len(record) < 4 {
				continue
			}
			e := TimelineEvent{
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
			events = append(events, e)
		}
	} else {
		if err := c.ShouldBindJSON(&events); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	count := 0
	for _, e := range events {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, latitude, longitude, recurring, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.UserID)
		if err == nil {
			count++
		}
	}

	sendGotifyNotification(fmt.Sprintf("Imported %d events", count), "")
	c.JSON(http.StatusOK, gin.H{"imported": count})
}

func exportEvents(c *gin.Context) {
	year := c.Query("year")
	format := c.Query("format")
	if format == "" {
		format = "json"
	}

	sqlStr := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id`
	args := []interface{}{}

	if year != "" {
		sqlStr += " WHERE strftime('%Y', e.event_date) = ?"
		args = append(args, year)
	}
	sqlStr += " ORDER BY e.event_date ASC"

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)

	if format == "csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=events.csv")
		c.String(http.StatusOK, "Title,Description,Date,Location,MediaType,Tags,Latitude,Longitude,Recurring,UserID\n")
		for _, e := range events {
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

	c.JSON(http.StatusOK, events)
}

func getEventStats(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	var stats EventStats
	stats.ByMonth = make(map[string]int)
	stats.ByTag = make(map[string]int)
	stats.ByMedia = make(map[string]int)
	stats.YearOverYear = make(map[string]int)
	stats.ByYear = make(map[string]int)

	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", year).Scan(&stats.Total)
	db.QueryRow("SELECT COUNT(DISTINCT location) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND location != ''", year).Scan(&stats.Locations)
	db.QueryRow("SELECT COUNT(DISTINCT person_id) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND person_id IS NOT NULL", year).Scan(&stats.Persons)
	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND media_url != ''", year).Scan(&stats.MediaTotal)
	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND location != ''", year).Scan(&stats.WithLocation)
	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND latitude != 0 AND longitude != 0", year).Scan(&stats.WithGeo)
	db.QueryRow("SELECT COUNT(*) FROM persons").Scan(&stats.PersonCount)
	stats.WithMedia = stats.MediaTotal

	monthRows, _ := db.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events 
		WHERE strftime('%Y', event_date) = ? GROUP BY strftime('%m', event_date)`, year)
	for monthRows.Next() {
		var month string
		var count int
		monthRows.Scan(&month, &count)
		stats.ByMonth[month] = count
	}
	monthRows.Close()

	tagRows, _ := db.Query(`SELECT tags, COUNT(*) FROM timeline_events 
		WHERE strftime('%Y', event_date) = ? AND tags != '' GROUP BY tags`, year)
	for tagRows.Next() {
		var tags string
		var count int
		tagRows.Scan(&tags, &count)
		stats.ByTag[tags] = count
	}
	tagRows.Close()

	mediaRows, _ := db.Query(`SELECT media_type, COUNT(*) FROM timeline_events 
		WHERE strftime('%Y', event_date) = ? GROUP BY media_type`, year)
	for mediaRows.Next() {
		var media string
		var count int
		mediaRows.Scan(&media, &count)
		stats.ByMedia[media] = count
	}
	mediaRows.Close()

	yoyRows, _ := db.Query(`SELECT strftime('%Y', event_date), COUNT(*) FROM timeline_events GROUP BY strftime('%Y', event_date) ORDER BY strftime('%Y', event_date) DESC LIMIT 5`)
	for yoyRows.Next() {
		var y string
		var count int
		yoyRows.Scan(&y, &count)
		stats.YearOverYear[y] = count
		stats.ByYear[y] = count
	}
	yoyRows.Close()
	stats.TotalYears = len(stats.ByYear)

	c.JSON(http.StatusOK, stats)
}

func createShareLink(c *gin.Context) {
	var input struct {
		EventIDs []int  `json:"event_ids"`
		Year     string `json:"year"`
		Days     int    `json:"days"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Days == 0 {
		input.Days = 7
	}

	token, err := generateSessionID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	eventIDsStr := ""
	for idx, idVal := range input.EventIDs {
		if idx > 0 {
			eventIDsStr += ","
		}
		eventIDsStr += strconv.Itoa(idVal)
	}

	expires := time.Now().Add(time.Duration(input.Days) * 24 * time.Hour)

	_, err = db.Exec(`INSERT INTO share_tokens (token, event_ids, year, expires_at) VALUES (?, ?, ?, ?)`,
		token, eventIDsStr, input.Year, expires.Format("2006-01-02"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create share link"})
		return
	}

	sendGotifyNotification("New share link created", fmt.Sprintf("Expires: %s", expires.Format("2006-01-02")))
	c.JSON(http.StatusOK, gin.H{"token": token, "expires": expires.Format("2006-01-02")})
}

func getShareLink(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token required"})
		return
	}

	var eventIDs, year string
	var expires string
	err := db.QueryRow("SELECT event_ids, year, expires_at FROM share_tokens WHERE token = ?", token).Scan(&eventIDs, &year, &expires)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid token"})
		return
	}

	expTime, _ := time.Parse("2006-01-02", expires)
	if time.Now().After(expTime) {
		c.JSON(http.StatusGone, gin.H{"error": "Token expired"})
		return
	}

	c.Redirect(http.StatusFound, "/?share="+token)
}

func getTags(c *gin.Context) {
	year := c.Query("year")
	query := "SELECT DISTINCT tags FROM timeline_events WHERE tags != ''"
	args := []interface{}{}

	if year != "" {
		query += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tags = append(tags, t)
	}

	c.JSON(http.StatusOK, tags)
}

func getPersons(c *gin.Context) {
	query := `SELECT p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at,
		(SELECT COUNT(*) FROM timeline_events WHERE person_id = p.id) as event_count
		FROM persons p ORDER BY p.name ASC`

	rows, err := db.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	persons := make([]Person, 0)
	for rows.Next() {
		var p Person
		err := rows.Scan(&p.ID, &p.Name, &p.AvatarURL, &p.Bio, &p.BirthDate, &p.Color, &p.CreatedAt, &p.EventCount)
		if err != nil {
			continue
		}
		persons = append(persons, p)
	}

	c.JSON(http.StatusOK, persons)
}

func savePerson(c *gin.Context) {
	var p Person
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if p.ID == 0 {
		result, err := db.Exec("INSERT INTO persons (name, avatar_url, bio, birth_date, color) VALUES (?, ?, ?, ?, ?)",
			p.Name, p.AvatarURL, p.Bio, p.BirthDate, p.Color)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		id, _ := result.LastInsertId()
		p.ID = int(id)
		sendGotifyNotification(fmt.Sprintf("Person created: %s", p.Name), p.Bio)
	} else {
		_, err := db.Exec("UPDATE persons SET name=?, avatar_url=?, bio=?, birth_date=?, color=? WHERE id=?",
			p.Name, p.AvatarURL, p.Bio, p.BirthDate, p.Color, p.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		sendGotifyNotification(fmt.Sprintf("Person updated: %s", p.Name), p.Bio)
	}

	c.JSON(http.StatusOK, p)
}

func deletePerson(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
		return
	}

	var name string
	db.QueryRow("SELECT name FROM persons WHERE id=?", id).Scan(&name)

	db.Exec("UPDATE timeline_events SET person_id = NULL WHERE person_id = ?", id)
	_, err = db.Exec("DELETE FROM persons WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete person"})
		return
	}

	sendGotifyNotification(fmt.Sprintf("Person deleted: %s", name), "")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getMapData(c *gin.Context) {
	year := c.Query("year")
	query := `SELECT id, title, description, event_date, location, media_type, media_url, latitude, longitude 
		FROM timeline_events WHERE latitude IS NOT NULL AND longitude IS NOT NULL AND latitude != 0 AND longitude != 0`
	args := []interface{}{}

	if year != "" {
		query += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}
	query += " ORDER BY event_date ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type MapFeature struct {
		ID          int     `json:"id"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Date        string  `json:"date"`
		Location    string  `json:"location"`
		MediaType   string  `json:"media_type"`
		MediaURL    string  `json:"media_url"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}

	var features []MapFeature
	for rows.Next() {
		var f MapFeature
		var lat, lng sql.NullFloat64
		var mediaURL, mediaType sql.NullString
		err := rows.Scan(&f.ID, &f.Title, &f.Description, &f.Date, &f.Location, &mediaType, &mediaURL, &lat, &lng)
		if err != nil {
			continue
		}
		f.MediaType = mediaType.String
		f.MediaURL = mediaURL.String
		if lat.Valid {
			f.Latitude = lat.Float64
		}
		if lng.Valid {
			f.Longitude = lng.Float64
		}
		features = append(features, f)
	}

	result := gin.H{
		"type":     "FeatureCollection",
		"features": features,
	}
	c.JSON(http.StatusOK, result)
}

func getGotifyConfig(c *gin.Context) {
	var cfg GotifyConfig
	var enabledInt int
	err := db.QueryRow("SELECT url, token, enabled FROM gotify_settings WHERE id = 1").Scan(&cfg.URL, &cfg.Token, &enabledInt)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

func saveGotifyConfig(c *gin.Context) {
	var cfg GotifyConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}

	_, err := db.Exec(`UPDATE gotify_settings SET url=?, token=?, enabled=? WHERE id=1`, cfg.URL, cfg.Token, enabledInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	gotifyURL = cfg.URL
	gotifyToken = cfg.Token
	gotifyEnabled = cfg.Enabled

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func testGotify(c *gin.Context) {
	if gotifyURL == "" || gotifyToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Gotify URL and token not configured"})
		return
	}

	body := fmt.Sprintf(`{"title":"TRACES Test","message":"This is a test notification from TRACES","priority":5}`)
	req, err := http.NewRequest("POST", gotifyURL+"/message", strings.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", gotifyToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Gotify: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Notification sent successfully"})
	} else {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Gotify returned %d", resp.StatusCode)})
	}
}

func getMemories(c *gin.Context) {
	var cfg MemoriesConfig
	var enabledInt int
	err := db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &cfg.DaysWindow, &cfg.EmailEnabled)
	if err != nil || enabledInt == 0 {
		c.JSON(http.StatusOK, []interface{}{})
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
		rows, err = db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND strftime('%m-%d', e.event_date) BETWEEN ? AND ?
			ORDER BY e.event_date DESC`, startMD, endMD)
	} else {
		rows, err = db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND (strftime('%m-%d', e.event_date) >= ? OR strftime('%m-%d', e.event_date) <= ?)
			ORDER BY e.event_date DESC`, startMD, endMD)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type MemoryEvent struct {
		TimelineEvent
		YearsAgo int `json:"years_ago"`
	}

	memories := make([]MemoryEvent, 0)
	pMap := make(map[int]Person)
	pRows, _ := db.Query("SELECT id, name, avatar_url, bio, birth_date, color, created_at FROM persons")
	if pRows != nil {
		defer pRows.Close()
		for pRows.Next() {
			var p Person
			if pRows.Scan(&p.ID, &p.Name, &p.AvatarURL, &p.Bio, &p.BirthDate, &p.Color, &p.CreatedAt) == nil {
				pMap[p.ID] = p
			}
		}
	}

	for rows.Next() {
		var me MemoryEvent
		var personID sql.NullInt64
		err := rows.Scan(&me.ID, &me.Title, &me.Description, &me.Date, &me.Location, &me.MediaType, &me.MediaURL, &me.Thumbnail, &me.MediaCaption, &me.Tags, &me.SortOrder, &me.IsPublic, &me.CreatedAt, &personID, &me.Latitude, &me.Longitude, &me.Recurring, &me.WeatherData, &me.UserID, &me.YearsAgo)
		if err != nil {
			continue
		}
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

func getMemoriesConfig(c *gin.Context) {
	var cfg MemoriesConfig
	var enabledInt int
	err := db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &cfg.DaysWindow, &cfg.EmailEnabled)
	if err != nil {
		c.JSON(http.StatusOK, MemoriesConfig{Enabled: true, DaysWindow: 3, EmailEnabled: false})
		return
	}
	cfg.Enabled = enabledInt == 1
	c.JSON(http.StatusOK, cfg)
}

func saveMemoriesConfig(c *gin.Context) {
	var cfg MemoriesConfig
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
	_, err := db.Exec(`UPDATE memories_settings SET enabled=?, days_window=?, email_enabled=? WHERE id=1`, enabledInt, cfg.DaysWindow, emailInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getEmailConfig(c *gin.Context) {
	var cfg EmailConfig
	var port int
	err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&cfg.SMTPHost, &port, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err != nil {
		c.JSON(http.StatusOK, EmailConfig{SMTPPort: 587})
		return
	}
	cfg.SMTPPort = port
	c.JSON(http.StatusOK, cfg)
}

func saveEmailConfig(c *gin.Context) {
	var cfg EmailConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}
	_, err := db.Exec(`UPDATE email_settings SET smtp_host=?, smtp_port=?, smtp_user=?, smtp_pass=?, from_addr=?, to_addr=? WHERE id=1`,
		cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.FromAddr, cfg.ToAddr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func testEmail(c *gin.Context) {
	var cfg EmailConfig
	var port int
	err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&cfg.SMTPHost, &port, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err != nil || cfg.SMTPHost == "" || cfg.ToAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email not configured"})
		return
	}
	cfg.SMTPPort = port

	subject := "TRACES Test Email"
	body := "This is a test email from TRACES. If you receive this, your email settings are working correctly."
	if err := sendEmail(cfg, subject, body); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to send email: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Test email sent successfully"})
}

func sendEmail(cfg EmailConfig, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", cfg.FromAddr, cfg.ToAddr, subject, body))

	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	}

	if cfg.SMTPPort == 465 {
		tlsCfg := &tls.Config{ServerName: cfg.SMTPHost}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return err
		}
		defer client.Close()
		if auth != nil {
			if err = client.Auth(auth); err != nil {
				return err
			}
		}
		if err = client.Mail(cfg.FromAddr); err != nil {
			return err
		}
		if err = client.Rcpt(cfg.ToAddr); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		_, err = w.Write(msg)
		if err != nil {
			return err
		}
		return w.Close()
	}

	return smtp.SendMail(addr, auth, cfg.FromAddr, []string{cfg.ToAddr}, msg)
}

func sendMemoriesEmailHandler(c *gin.Context) {
	var memCfg MemoriesConfig
	var enabledInt int
	db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &memCfg.DaysWindow, &memCfg.EmailEnabled)
	memCfg.Enabled = enabledInt == 1

	if !memCfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Memories are disabled"})
		return
	}

	var emailCfg EmailConfig
	var port int
	err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&emailCfg.SMTPHost, &port, &emailCfg.SMTPUser, &emailCfg.SMTPPass, &emailCfg.FromAddr, &emailCfg.ToAddr)
	if err != nil || emailCfg.SMTPHost == "" || emailCfg.ToAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email not configured"})
		return
	}
	emailCfg.SMTPPort = port

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -memCfg.DaysWindow)
	end := today.AddDate(0, 0, memCfg.DaysWindow)
	startMD := start.Format("01-02")
	endMD := end.Format("01-02")

	var rows *sql.Rows
	if startMD <= endMD {
		rows, err = db.Query(`SELECT e.title, e.event_date,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND strftime('%m-%d', e.event_date) BETWEEN ? AND ?
			ORDER BY e.event_date DESC`, startMD, endMD)
	} else {
		rows, err = db.Query(`SELECT e.title, e.event_date,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND (strftime('%m-%d', e.event_date) >= ? OR strftime('%m-%d', e.event_date) <= ?)
			ORDER BY e.event_date DESC`, startMD, endMD)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

	if err := sendEmail(emailCfg, subject, body); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to send email: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": fmt.Sprintf("Sent %d memories via email", len(memories))})
}

func scanEventsWithPerson(rows *sql.Rows) []TimelineEvent {
	events := make([]TimelineEvent, 0)
	for rows.Next() {
		var e TimelineEvent
		var p Person
		var personID sql.NullInt64
		var lat, lng sql.NullFloat64
		var pID sql.NullInt64
		var pName, pAvatar, pBio, pBirth, pColor, pCreated sql.NullString

		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &e.MediaCaption, &e.Tags, &e.SortOrder, &e.IsPublic, &e.CreatedAt, &personID, &lat, &lng, &e.Recurring, &e.WeatherData, &e.UserID,
			&pID, &pName, &pAvatar, &pBio, &pBirth, &pColor, &pCreated)
		if err != nil {
			continue
		}

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
	return events
}

func getCalendar(c *gin.Context) {
	year := c.Query("year")
	month := c.Query("month")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	if month == "" {
		month = fmt.Sprintf("%02d", time.Now().Month())
	}

	startDate := fmt.Sprintf("%s-%s-01", year, month)
	firstDay, _ := time.Parse("2006-01-02", startDate)
	lastDay := firstDay.AddDate(0, 1, -1)

	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id
		WHERE e.event_date BETWEEN ? AND ?
		ORDER BY e.event_date ASC`, startDate, lastDay.Format("2006-01-02"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)

	daysMap := make(map[string][]TimelineEvent)
	for _, e := range events {
		daysMap[e.Date] = append(daysMap[e.Date], e)
	}

	var calendar []CalendarDay
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		dayEvents := daysMap[dateStr]
		sort.Slice(dayEvents, func(i, j int) bool {
			return dayEvents[i].SortOrder < dayEvents[j].SortOrder
		})
		calendar = append(calendar, CalendarDay{
			Date:   dateStr,
			Events: dayEvents,
			Count:  len(dayEvents),
		})
	}

	c.JSON(http.StatusOK, calendar)
}

func fetchWeather(c *gin.Context) {
	var input struct {
		EventID   int     `json:"event_id"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Date      string  `json:"date"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max&timezone=auto&start_date=%s&end_date=%s",
		input.Latitude, input.Longitude, input.Date, input.Date)

	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch weather: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var weatherResp struct {
		Daily struct {
			Time             []string  `json:"time"`
			TemperatureMax   []float64 `json:"temperature_2m_max"`
			TemperatureMin   []float64 `json:"temperature_2m_min"`
			WeatherCode      []int     `json:"weathercode"`
			WindSpeedMax     []float64 `json:"wind_speed_10m_max"`
		} `json:"daily"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&weatherResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to parse weather data"})
		return
	}

	if len(weatherResp.Daily.Time) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No weather data available for this date"})
		return
	}

	condition := weatherCodeToCondition(weatherResp.Daily.WeatherCode[0])
	icon := weatherCodeToIcon(weatherResp.Daily.WeatherCode[0])
	temp := (weatherResp.Daily.TemperatureMax[0] + weatherResp.Daily.TemperatureMin[0]) / 2

	weather := WeatherData{
		Temperature: temp,
		Condition:   condition,
		Icon:        icon,
		WindSpeed:   weatherResp.Daily.WindSpeedMax[0],
		FetchedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	weatherJSON, _ := json.Marshal(weather)

	if input.EventID > 0 {
		db.Exec("UPDATE timeline_events SET weather_data = ? WHERE id = ?", string(weatherJSON), input.EventID)
	}

	c.JSON(http.StatusOK, weather)
}

func weatherCodeToCondition(code int) string {
	switch {
	case code == 0:
		return "Clear sky"
	case code <= 3:
		return "Partly cloudy"
	case code <= 48:
		return "Foggy"
	case code <= 57:
		return "Drizzle"
	case code <= 67:
		return "Rain"
	case code <= 77:
		return "Snow"
	case code <= 82:
		return "Rain showers"
	case code <= 86:
		return "Snow showers"
	default:
		return "Thunderstorm"
	}
}

func weatherCodeToIcon(code int) string {
	switch {
	case code == 0:
		return "sun"
	case code <= 3:
		return "cloud-sun"
	case code <= 48:
		return "smog"
	case code <= 57:
		return "cloud-rain"
	case code <= 67:
		return "cloud-showers-heavy"
	case code <= 77:
		return "snowflake"
	case code <= 82:
		return "cloud-showers-heavy"
	case code <= 86:
		return "snowflake"
	default:
		return "bolt"
	}
}

func autoTagEvent(c *gin.Context) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Location    string `json:"location"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var ollamaURL, ollamaModel string
	var enabledInt int
	err := db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&ollamaURL, &ollamaModel, &enabledInt)
	if err != nil || enabledInt == 0 {
		ollamaURL = os.Getenv("OLLAMA_URL")
		if ollamaURL == "" {
			ollamaURL = "http://localhost:11434"
		}
		ollamaModel = os.Getenv("OLLAMA_MODEL")
		if ollamaModel == "" {
			ollamaModel = "llama3.2"
		}
	}

	prompt := fmt.Sprintf(`Given the following event, suggest 3-5 relevant tags (comma-separated, single words only):
Title: %s
Description: %s
Location: %s
Tags:`, input.Title, input.Description, input.Location)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":  ollamaModel,
		"prompt": prompt,
		"stream": false,
	})

	resp, err := http.Post(ollamaURL+"/api/generate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Ollama: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var ollamaResp struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to parse Ollama response"})
		return
	}

	tags := strings.Split(ollamaResp.Response, ",")
	cleanTags := make([]string, 0)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "- ")
		t = strings.TrimPrefix(t, "* ")
		if t != "" && !strings.HasPrefix(t, "Tags:") {
			cleanTags = append(cleanTags, t)
		}
	}

	c.JSON(http.StatusOK, gin.H{"tags": cleanTags})
}

func getUsers(c *gin.Context) {
	rows, err := db.Query(`SELECT id, username, display_name, color, avatar_url, created_at,
		(SELECT COUNT(*) FROM timeline_events WHERE user_id = users.id) as event_count
		FROM users ORDER BY display_name ASC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var u User
		err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Color, &u.AvatarURL, &u.CreatedAt, &u.EventCount)
		if err != nil {
			continue
		}
		users = append(users, u)
	}

	c.JSON(http.StatusOK, users)
}

func saveUser(c *gin.Context) {
	var u User
	if err := c.ShouldBindJSON(&u); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if u.ID == 0 {
		result, err := db.Exec("INSERT INTO users (username, display_name, color, avatar_url) VALUES (?, ?, ?, ?)",
			u.Username, u.DisplayName, u.Color, u.AvatarURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		id, _ := result.LastInsertId()
		u.ID = int(id)
	} else {
		_, err := db.Exec("UPDATE users SET username=?, display_name=?, color=?, avatar_url=? WHERE id=?",
			u.Username, u.DisplayName, u.Color, u.AvatarURL, u.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, u)
}

func deleteUser(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	db.Exec("UPDATE timeline_events SET user_id = 0 WHERE user_id = ?", id)
	_, err = db.Exec("DELETE FROM users WHERE id=?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getUserEvents(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE e.user_id = ? ORDER BY e.event_date ASC`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

func generateRecurringEvents(c *gin.Context) {
	var input struct {
		EventID   int    `json:"event_id"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var e TimelineEvent
	err := db.QueryRow(`SELECT title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, user_id FROM timeline_events WHERE id = ?`, input.EventID).
		Scan(&e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &e.Tags, &e.SortOrder, &e.Recurring, &e.WeatherData, &e.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}

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

	originalDate, err := time.Parse("2006-01-02", e.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event date"})
		return
	}

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
			db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE title = ? AND event_date = ? AND user_id = ?", e.Title, d.Format("2006-01-02"), e.UserID).Scan(&existing)
			if existing == 0 {
				db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					e.Title, e.Description, d.Format("2006-01-02"), e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Recurring, e.WeatherData, e.UserID)
				generated++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"generated": generated})
}

func getOllamaConfig(c *gin.Context) {
	var cfg OllamaConfig
	var enabledInt int
	err := db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&cfg.URL, &cfg.Model, &enabledInt)
	if err != nil {
		c.JSON(http.StatusOK, OllamaConfig{URL: "http://localhost:11434", Model: "llama3.2", Enabled: false})
		return
	}
	cfg.Enabled = enabledInt == 1
	c.JSON(http.StatusOK, cfg)
}

func saveOllamaConfig(c *gin.Context) {
	var cfg OllamaConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}
	if cfg.URL == "" {
		cfg.URL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "llama3.2"
	}
	_, err := db.Exec(`UPDATE ollama_settings SET url=?, model=?, enabled=? WHERE id=1`, cfg.URL, cfg.Model, enabledInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func serveManifest(c *gin.Context) {
	c.Header("Content-Type", "application/json")
	c.String(http.StatusOK, `{
		"name": "TRACES - Your Year in Review",
		"short_name": "TRACES",
		"description": "Personal timeline management system for capturing everyday moments",
		"start_url": "/",
		"display": "standalone",
		"background_color": "#0f172a",
		"theme_color": "#7c3aed",
		"icons": [
			{"src": "/static/favicon.svg", "sizes": "any", "type": "image/svg+xml"},
			{"src": "/static/logo.svg", "sizes": "any", "type": "image/svg+xml"}
		]
	}`)
}

func serveServiceWorker(c *gin.Context) {
	c.Header("Content-Type", "application/javascript")
	c.String(http.StatusOK, `const CACHE = 'traces-v1';
self.addEventListener('install', e => { e.waitUntil(caches.open(CACHE).then(c => c.addAll(['/','/static/style.css','/static/app.js']))); self.skipWaiting(); });
self.addEventListener('activate', e => { e.waitUntil(clients.claim()); });
self.addEventListener('fetch', e => {
	e.respondWith(
		fetch(e.request).catch(() => caches.match(e.request))
	);
});`)
}

func initDB() {
	_, _ = db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)")

	var version int
	err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err != nil {
		version = 0
		db.Exec("INSERT INTO schema_version (version) VALUES (0)")
	}

	log.Printf("[DB] Current schema version: %d, target: %d", version, currentSchemaVersion)

	for version < currentSchemaVersion {
		runMigration(version)
		version++
		db.Exec("DELETE FROM schema_version")
		db.Exec("INSERT INTO schema_version (version) VALUES (?)", version)
		log.Printf("[DB] Migrated to schema version %d", version)
	}

	createTables()
	publicMode = os.Getenv("PUBLIC_MODE") == "true"
	seedEvents()
}

func createTables() {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS timeline_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			description TEXT,
			event_date TEXT,
			location TEXT,
			media_type TEXT,
			media_url TEXT,
			thumbnail TEXT,
			media_caption TEXT,
			tags TEXT,
			sort_order INTEGER DEFAULT 0,
			is_public INTEGER DEFAULT 0,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			person_id INTEGER,
			latitude REAL,
			longitude REAL,
			recurring TEXT DEFAULT '',
			weather_data TEXT DEFAULT '',
			user_id INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			password TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS share_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE,
			event_ids TEXT,
			year TEXT,
			expires_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS persons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			avatar_url TEXT DEFAULT '',
			bio TEXT DEFAULT '',
			birth_date TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS gotify_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			token TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS memories_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER DEFAULT 1,
			days_window INTEGER DEFAULT 3,
			email_enabled INTEGER DEFAULT 0,
			last_sent_date TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS email_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			smtp_host TEXT DEFAULT '',
			smtp_port INTEGER DEFAULT 587,
			smtp_user TEXT DEFAULT '',
			smtp_pass TEXT DEFAULT '',
			from_addr TEXT DEFAULT '',
			to_addr TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			avatar_url TEXT DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS ollama_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT 'http://localhost:11434',
			model TEXT DEFAULT 'llama3.2',
			enabled INTEGER DEFAULT 0
		)`,
	}

	for _, q := range queries {
		_, err := db.Exec(q)
		if err != nil {
			log.Fatal(err)
		}
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM gotify_settings").Scan(&count)
	if count == 0 {
		db.Exec("INSERT INTO gotify_settings (id, url, token, enabled) VALUES (1, '', '', 0)")
	}

	var memCount int
	db.QueryRow("SELECT COUNT(*) FROM memories_settings").Scan(&memCount)
	if memCount == 0 {
		db.Exec("INSERT INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)")
	}

	var emailCount int
	db.QueryRow("SELECT COUNT(*) FROM email_settings").Scan(&emailCount)
	if emailCount == 0 {
		db.Exec("INSERT INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)")
	}

	var userCount int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	if userCount == 0 {
		db.Exec("INSERT INTO users (id, username, display_name, color) VALUES (1, 'default', 'Default User', '#7c3aed')")
	}

	var ollamaCount int
	db.QueryRow("SELECT COUNT(*) FROM ollama_settings").Scan(&ollamaCount)
	if ollamaCount == 0 {
		db.Exec("INSERT INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)")
	}
}

func runMigration(fromVersion int) {
	switch fromVersion {
	case 0:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			description TEXT,
			event_date TEXT,
			location TEXT,
			media_type TEXT,
			media_url TEXT,
			thumbnail TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
	case 1:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN media_caption TEXT`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN tags TEXT`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN sort_order INTEGER DEFAULT 0`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN is_public INTEGER DEFAULT 0`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS share_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE,
			event_ids TEXT,
			year TEXT,
			expires_at TEXT
		)`)
	case 2:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS persons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			avatar_url TEXT DEFAULT '',
			bio TEXT DEFAULT '',
			birth_date TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS gotify_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			token TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO gotify_settings (id, url, token, enabled) VALUES (1, '', '', 0)`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN person_id INTEGER`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN latitude REAL`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN longitude REAL`)
	case 3:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled INTEGER DEFAULT 1,
			days_window INTEGER DEFAULT 3,
			email_enabled INTEGER DEFAULT 0,
			last_sent_date TEXT DEFAULT ''
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			smtp_host TEXT DEFAULT '',
			smtp_port INTEGER DEFAULT 587,
			smtp_user TEXT DEFAULT '',
			smtp_pass TEXT DEFAULT '',
			from_addr TEXT DEFAULT '',
			to_addr TEXT DEFAULT ''
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)`)
	case 4:
		for _, col := range []string{"person_id", "latitude", "longitude", "media_caption", "tags", "sort_order", "is_public"} {
			_, err := db.Exec(fmt.Sprintf("ALTER TABLE timeline_events ADD COLUMN %s", col))
			if err == nil {
				log.Printf("[DB] Added missing column: %s", col)
			}
		}
	case 5:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN recurring TEXT DEFAULT ''`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN weather_data TEXT DEFAULT ''`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN user_id INTEGER DEFAULT 0`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE,
			display_name TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			avatar_url TEXT DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO users (id, username, display_name, color) VALUES (1, 'default', 'Default User', '#7c3aed')`)
	case 6:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS ollama_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT 'http://localhost:11434',
			model TEXT DEFAULT 'llama3.2',
			enabled INTEGER DEFAULT 0
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)`)
	}
}

func seedEvents() {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
	if count > 0 {
		return
	}

	nyLat, nyLng := 40.7580, -73.9855
	cpLat, cpLng := 40.7829, -73.9654
	events := []TimelineEvent{
		{
			Title:       "New Year Celebration",
			Description: "Welcome to the new year with fireworks and festivities!",
			Date:        "2026-01-01",
			Location:    "Times Square, NYC",
			MediaType:   "image",
			MediaURL:    "/media/newyear.jpg",
			Latitude:    &nyLat,
			Longitude:   &nyLng,
		},
		{
			Title:       "Summer Music Festival",
			Description: "Amazing performances under the stars",
			Date:        "2026-07-15",
			Location:    "Central Park",
			MediaType:   "video",
			MediaURL:    "/media/festival.mp4",
			Latitude:    &cpLat,
			Longitude:   &cpLng,
		},
	}

	for _, e := range events {
		db.Exec("INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, latitude, longitude) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Latitude, e.Longitude)

		parts := strings.Split(e.MediaURL, "/")
		if len(parts) > 2 {
			filePath := filepath.Join(basePath, parts[1], parts[2])
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				if f, err := os.Create(filePath); err == nil {
					f.Close()
				}
			}
		}
	}
}

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func sendGotifyNotification(title, message string) {
	if !gotifyEnabled && (gotifyURL == "" || gotifyToken == "") {
		return
	}
	if gotifyURL == "" || gotifyToken == "" {
		return
	}

	payload := map[string]interface{}{
		"title":    title,
		"message":  message,
		"priority": 5,
	}

	body, _ := json.Marshal(payload)
	url := strings.TrimSuffix(gotifyURL, "/") + "/message"

	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			log.Printf("[GOTIFY] Notification failed: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Gotify-Key", gotifyToken)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[GOTIFY] Notification failed: %v", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			log.Printf("[GOTIFY] Notification failed with status: %d", resp.StatusCode)
		} else {
			log.Printf("[GOTIFY] Notification sent: %s", title)
		}
	}()
}

func EscapeHtml(text string) string {
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	text = strings.ReplaceAll(text, `"`, "&quot;")
	text = strings.ReplaceAll(text, "'", "&#039;")
	return text
}

func GetMediaIcon(mediaType string) string {
	switch mediaType {
	case "video":
		return "fa-solid fa-video"
	case "audio":
		return "fa-solid fa-music"
	default:
		return "fa-solid fa-image"
	}
}

func FormatDate(dateStr string) string {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return date.Format("Jan 2")
}
