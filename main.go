// TRACES API
//
// A timeline API for managing events with multimedia (images, videos, audio) throughout the year.
//
//	Schemes: http
//	Host: localhost:6270
//	BasePath: /api
//	Version: 1.8.12
//	Contact: API Support
//
//	Consumes:
//	- application/json
//	- multipart/form-data
//
//	Produces:
//	- application/json
//
//	SecurityDefinitions:
//	SessionCookie:
//	  type: apiKey
//	  in: cookie
//	  name: session
//
// swagger:meta
package main

import (
	"bytes"
	"context"
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
	"io/fs"
	"log"
	"maps"
	"math"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/yuin/goldmark"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/image/draw"
	"golang.org/x/net/webdav"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	swaggerFiles "github.com/swaggo/files/v2"
	ginSwagger "github.com/swaggo/gin-swagger"

	"traces/internal/models"
)

type webdavFS struct{ fsFS fs.FS }

func (w webdavFS) Mkdir(ctx context.Context, _ string, _ os.FileMode) error { return os.ErrPermission }
func (w webdavFS) RemoveAll(ctx context.Context, _ string) error            { return os.ErrPermission }
func (w webdavFS) Rename(ctx context.Context, _, _ string) error            { return os.ErrPermission }
func (w webdavFS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	if flag != os.O_RDONLY {
		return nil, os.ErrPermission
	}
	f, err := w.fsFS.Open(name)
	if err != nil {
		return nil, err
	}
	return &webdavFile{File: f}, nil
}
func (w webdavFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return fs.Stat(w.fsFS, name)
}

type webdavFile struct{ fs.File }

func (webdavFile) Write([]byte) (int, error)          { return 0, os.ErrPermission }
func (webdavFile) Readdir(int) ([]os.FileInfo, error) { return nil, nil }
func (f webdavFile) Seek(offset int64, whence int) (int64, error) {
	return f.File.(io.Seeker).Seek(offset, whence)
}

func init() {
	image.RegisterFormat("png", "png", png.Decode, png.DecodeConfig)
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
}

const defaultColor = "#7c3aed"
const currentSchemaVersion = 18
const currentVersion = "1.25.9"

var (
	publicMode    bool = false
	gotifyEnabled bool
	immichEnabled bool
)

var (
	db                 *sql.DB
	sessionStore       = make(map[string]int64)
	csrfTokens         = make(map[string]string)
	sessionMu          sync.RWMutex
	basePath           = "/app"
	dbPath             = "/db/traces.db"
	mediaPath          = "/app/media"
	backupPath         = "/db/backups"
	gotifyURL          = ""
	gotifyToken        = ""
	umamiURL           = ""
	umamiSiteID        = ""
	immichURL          = ""
	immichAPIKey       = ""
	otelEndpoint       = ""
	otelTracesEnabled  bool
	otelMetricsEnabled bool
	otelLogsEnabled    bool
	currentUserID      int
	logService         *LogService
)

func main() {
	if os.Getenv("DOCKER") != "true" {
		basePath = "."
		dbPath = "./traces.db"
		mediaPath = filepath.Join(basePath, "media")
		backupPath = filepath.Join(basePath, "backups")
	}

	gotifyURL = os.Getenv("GOTIFY_URL")
	gotifyToken = os.Getenv("GOTIFY_TOKEN")
	if os.Getenv("GOTIFY_ENABLED") == "true" {
		gotifyEnabled = true
	}

	umamiURL = os.Getenv("UMAMI_URL")
	umamiSiteID = os.Getenv("UMAMI_SITE_ID")

	immichURL = os.Getenv("IMMICH_URL")
	immichAPIKey = os.Getenv("IMMICH_API_KEY")
	if os.Getenv("IMMICH_ENABLED") == "true" {
		immichEnabled = true
	}

	if err := os.MkdirAll(mediaPath, 0755); err != nil {
		log.Printf("Warning: could not create media directory: %v", err)
	}
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		log.Printf("Warning: could not create backup directory: %v", err)
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
	initTemplates()

	// Initialize the logging service
	logService = &LogService{db: db}
	if err := logService.Init(); err != nil {
		log.Printf("[LogService] Failed to initialize: %v", err)
	}

	if immichURL == "" {
		var cfg models.ImmichConfig
		var enabledInt int
		if err := db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&cfg.URL, &cfg.APIKey, &enabledInt); err == nil {
			immichURL = cfg.URL
			immichAPIKey = cfg.APIKey
			immichEnabled = enabledInt == 1
		}
	}

	if umamiURL == "" {
		var cfg models.UmamiConfig
		var enabledInt int
		if err := db.QueryRow("SELECT url, site_id, enabled FROM umami_settings WHERE id = 1").Scan(&cfg.URL, &cfg.SiteID, &enabledInt); err == nil {
			umamiURL = cfg.URL
			umamiSiteID = cfg.SiteID
		}
	}

	// Load OTel settings from DB (fallback if env not set)
	var otelCfg models.OtelConfig
	var tEnabled, mEnabled, lEnabled int
	if err := db.QueryRow("SELECT endpoint, traces_enabled, metrics_enabled, logs_enabled FROM otel_settings WHERE id = 1").Scan(&otelEndpoint, &tEnabled, &mEnabled, &lEnabled); err == nil {
		otelTracesEnabled = tEnabled == 1
		otelMetricsEnabled = mEnabled == 1
		otelLogsEnabled = lEnabled == 1
	}
	_ = otelCfg

	if os.Getenv("BACKUP_RETENTION_DAYS") != "" {
		if days, err := strconv.Atoi(os.Getenv("BACKUP_RETENTION_DAYS")); err == nil && days > 0 {
			db.Exec("UPDATE backup_settings SET retention_days=? WHERE id=1", days)
		}
	}

	r := gin.Default()
	r.MaxMultipartMemory = 32 << 20

	tp, err := initTelemetry()
	if err != nil {
		log.Printf("Failed to initialize telemetry: %v", err)
	} else {
		r.Use(metricsMiddleware())
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Printf("Error shutting down tracer provider: %v", err)
			}
		}()
	}

	r.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(self)")
		if c.Request.TLS != nil {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if c.Request.Method == "POST" || c.Request.Method == "PUT" {
			if !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
				c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
			}
		}
		c.Next()
	})

	api := r.Group("/api")
	{
		api.GET("/version", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"version": models.CurrentVersion})
		})
		api.GET("/check-setup", handleCheckSetup)
		api.POST("/login", handleLogin)
		api.POST("/logout", handleLogout)
		api.GET("/public", getPublicEvents)
		api.GET("/share", getShareLink)
		api.GET("/config", getPublicConfig)
		api.GET("/manifest.json", serveManifest)
		api.GET("/health", handleHealth)
		r.GET("/metrics", gin.WrapH(promhttp.Handler()))
		api.GET("/sw.js", serveServiceWorker)

		auth := api.Group("")
		auth.Use(authMiddlewareGin(), csrfMiddleware())
		{
			auth.GET("/events", getEvents)
			auth.GET("/events/full", getEventsFull)
			auth.GET("/events/search", searchEvents)
			auth.GET("/events/search/global", globalSearchEvents)
			auth.GET("/events/export", exportEvents)
			auth.GET("/events/ics", getEventsICS)
			auth.GET("/contributions", getContributions)
			auth.GET("/stats", getEventStats)
			auth.GET("/stats/distribution", getStatsDistribution)
			auth.GET("/tags", getTags)
			auth.POST("/tags/rename", renameTag)
			auth.POST("/tags/delete", deleteTag)
			auth.POST("/tags/merge", mergeTags)
			auth.GET("/map", getMapData)
			auth.GET("/persons", getPersons)
			auth.GET("/autocomplete", getAutocomplete)
			auth.GET("/calendar", getCalendar)
			auth.GET("/users", getUsers)
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
			auth.GET("/immich/config", getImmichConfig)
			auth.POST("/immich/config", saveImmichConfig)
			auth.POST("/immich/test", testImmich)
			auth.GET("/immich/memories", fetchImmichMemories)
			auth.POST("/immich/import", importImmichMemories)
			auth.GET("/umami/config", getUmamiConfig)
			auth.POST("/umami/config", saveUmamiConfig)
			auth.GET("/otel/config", getOtelConfig)
			auth.POST("/otel/config", saveOtelConfig)
			auth.POST("/backup", handleBackup)
			auth.GET("/backups", handleListBackups)
			auth.GET("/backup/config", getBackupConfig)
			auth.POST("/backup/config", saveBackupConfig)
			auth.GET("/events/trash", getTrashEvents)
			auth.POST("/events/restore", restoreEvents)
			auth.POST("/events/empty-trash", emptyTrash)
			auth.POST("/events/favorite", toggleFavorite)
			auth.POST("/events/batch", batchEvents)
			auth.GET("/collections", getCollections)
			auth.POST("/collections", saveCollection)
			auth.DELETE("/collections", deleteCollection)
			auth.GET("/collections/:id/events", getCollectionEvents)
			auth.POST("/collections/:id/events", addEventToCollection)
			auth.DELETE("/collections/:id/events", removeEventFromCollection)
			auth.GET("/templates", getTemplates)
			auth.POST("/templates", saveTemplate)
			auth.DELETE("/templates", deleteTemplate)
			auth.POST("/templates/apply", applyTemplate)
			auth.GET("/wrapped", getWrapped)
			auth.GET("/csrf-token", getCSRFToken)
			// Log endpoints
			auth.GET("/logs", handleGetLogs)
			auth.GET("/logs/count", handleGetLogCount)
			auth.DELETE("/logs", handleClearLogs)
			auth.GET("/logs/settings", handleGetLogSettings)
			auth.POST("/logs/settings", handleUpdateLogSettings)
			auth.GET("/logs/sources", handleGetLogSources)
		}
	}

	registerHTMXRoutes(r)

	r.GET("/sw.js", serveServiceWorker)

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
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})

	r.GET("/swagger/*any", ginSwagger.WrapHandler(&webdav.Handler{FileSystem: webdavFS{fsFS: swaggerFiles.FS}}))

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

	// Session cleanup goroutine
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			sessionMu.Lock()
			now := time.Now().Unix()
			for k, v := range sessionStore {
				if now > v {
					delete(sessionStore, k)
					delete(csrfTokens, k)
				}
			}
			sessionMu.Unlock()
		}
	}()

	// Weekly backup goroutine
	go func() {
		for {
			now := time.Now()
			next := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
			weekday := next.Weekday()
			if weekday != time.Sunday {
				next = next.Add(time.Duration((7-weekday)%7) * 24 * time.Hour)
			}
			next = next.Add(3 * time.Hour)
			time.Sleep(time.Until(next))
			ticker := time.NewTicker(7 * 24 * time.Hour)
			defer ticker.Stop()
			for {
				backupDatabase()
				pruneBackups()
				<-ticker.C
			}
		}
	}()

	port := os.Getenv("PORT")
	if port == "" {
		port = "6270"
	}

	log.Printf("Server starting on port %s...", port)
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
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
			delete(csrfTokens, cookie)
			sessionMu.Unlock()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			return
		}
		c.Set("session_id", cookie)
		c.Next()
	}
}

func csrfMiddleware() gin.HandlerFunc {
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
		sessionMu.RLock()
		stored, ok := csrfTokens[cookie]
		sessionMu.RUnlock()
		if !ok || token != stored {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Invalid CSRF token"})
			return
		}
		c.Next()
	}
}

// @Summary Get CSRF token
// @Description Returns a new CSRF token for the current session
// @Tags Authentication
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /csrf-token [get]
func getCSRFToken(c *gin.Context) {
	cookie, _ := c.Cookie("session")
	if cookie == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}
	sessionMu.Lock()
	token, ok := csrfTokens[cookie]
	if !ok {
		token = fmt.Sprintf("%x", sha256.Sum256([]byte(cookie+"-csrf")))
		csrfTokens[cookie] = token
	}
	sessionMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// startSpan creates a child span from the request context and returns the context + span.
// Use it in handlers to add trace instrumentation.
func startSpan(c *gin.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracer.Start(c.Request.Context(), name, opts...)
}

func serverError(c *gin.Context, err error) {
	log.Printf("[ERROR] %v", err)
	if span := trace.SpanFromContext(c.Request.Context()); span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
}

// @Summary Get public config
// @Description Returns public configuration (umami analytics settings)
// @Tags Info
// @Produce json
// @Success 200 {object} map[string]string
// @Router /config [get]
func getPublicConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"umami_url":  umamiURL,
		"umami_site": umamiSiteID,
	})
}

// @Summary Check setup status
// @Description Check if admin has been configured
// @Tags Info
// @Produce json
// @Success 200 {object} map[string]bool
// @Router /check-setup [get]
func handleCheckSetup(c *gin.Context) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	c.JSON(http.StatusOK, gin.H{"setup": count > 0})
}

// @Summary Health check
// @Description Returns server health status and version
// @Tags System
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": models.CurrentVersion,
	})
}

// @Summary Login admin user
// @Description Authenticate admin user or perform initial setup
// @Tags Authentication
// @Accept json
// @Produce json
// @Param credentials body object true "Login credentials" SchemaProperties({username:{type:string}, password:{type:string}, setup:{type:boolean}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /login [post]
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
		if len(input.Password) < 8 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
			return
		}
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

		db.Exec("INSERT OR IGNORE INTO users (id, username, display_name, email, color) VALUES (1, ?, ?, '', ?)", input.Username, input.Username, defaultColor)

		sessionID, err := generateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session"})
			return
		}
		sessionMu.Lock()
		sessionStore[sessionID] = time.Now().Add(24 * time.Hour).Unix()
		csrfTokens[sessionID] = fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"-csrf")))
		sessionMu.Unlock()
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

	var user models.AdminUser
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
	csrfTokens[sessionID] = fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"-csrf")))
	sessionMu.Unlock()
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

// @Summary Logout admin user
// @Description Destroy admin session
// @Tags Authentication
// @Produce json
// @Success 200 {object} map[string]string
// @Router /logout [post]
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
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Get timeline events
// @Description Returns events with optional year, month, tag, limit, sort, and user_id filters
// @Tags Events
// @Produce json
// @Param year query string false "Filter by year"
// @Param month query string false "Filter by month (01-12)"
// @Param tag query string false "Filter by tag"
// @Param limit query int false "Limit results"
// @Param sort query string false "Sort order" Enums(asc, desc)
// @Param user_id query int false "Filter by user ID"
// @Success 200 {array} object "timeline events"
// @Router /events [get]
func getEvents(c *gin.Context) {
	ctx, span := startSpan(c, "getEvents")
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

	_qStart := time.Now()
	rows, err := db.Query(query, args...)
	RecordDBQuery("getEvents", time.Since(_qStart))
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := ScanEvents(rows)
	span.SetAttributes(attribute.Int("event_count", len(events)))
	c.JSON(http.StatusOK, events)
	_ = ctx
}

// @Summary Get all events with full fields
// @Description Returns all events with complete field data, ordered by date ascending
// @Tags Events
// @Produce json
// @Success 200 {array} object "timeline events"
// @Router /events/full [get]
func getEventsFull(c *gin.Context) {
	query, _ := BuildEventQuery(EventFilters{Sort: "asc"})
	rows, err := db.Query(query)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := ScanEvents(rows)
	c.JSON(http.StatusOK, events)
}

// @Summary Get public events
// @Description Returns events accessible via share token or public mode
// @Tags Events
// @Produce json
// @Param share query string false "Share token"
// @Param year query string false "Filter by year"
// @Param month query string false "Filter by month (01-12)"
// @Success 200 {array} object "timeline events"
// @Router /public [get]
func getPublicEvents(c *gin.Context) {
	shareToken := c.Query("share")
	year := c.Query("year")
	month := c.Query("month")

	var eventIDs string
	var shareYear string

	if shareToken != "" {
		db.QueryRow("SELECT event_ids, year FROM share_tokens WHERE token = ?", shareToken).Scan(&eventIDs, &shareYear)
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
	} else if publicMode {
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

	rows, err := db.Query(query, args...)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

// @Summary Get contribution data
// @Description Returns a map of dates to event counts for contribution graph
// @Tags Events
// @Produce json
// @Param year query string false "Filter by year"
// @Success 200 {object} map[string]int
// @Router /contributions [get]
func getContributions(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	rows, err := db.Query(`SELECT event_date FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date) = ?`, year)
	if err != nil {
		serverError(c, err)
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

// @Summary Create or update event
// @Description Creates a new event or updates an existing one
// @Tags Events
// @Accept json
// @Produce json
// @Param event body object true "Event data" SchemaProperties(id:{type:integer}, title:{type:string}, description:{type:string}, date:{type:string}, location:{type:string}, media_type:{type:string}, media_url:{type:string}, thumbnail:{type:string}, tags:{type:string}, is_public:{type:boolean}, person_id:{type:integer}, latitude:{type:number}, longitude:{type:number})
// @Success 200 {object} object "saved event"
// @Failure 400 {object} map[string]string
// @Router /events [post]
func saveEvent(c *gin.Context) {
	_, span := startSpan(c, "saveEvent")
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

	span.SetAttributes(
		attribute.Int("event.id", e.ID),
		attribute.String("event.title", e.Title),
		attribute.String("event.date", e.Date),
	)

	log.Printf("[EVENT] Saving event: ID=%d, Title=%s, Date=%s", e.ID, e.Title, e.Date)

	action := "created"
	_qStart := time.Now()
	if e.ID == 0 {
		result, err := db.Exec(`INSERT INTO timeline_events 
			(title, description, event_date, location, media_type, media_url, thumbnail, media_caption, tags, sort_order, is_public, is_favorite, person_id, latitude, longitude, recurring, weather_data, event_start_time, event_end_time, user_id) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.SortOrder, e.IsPublic, e.IsFavorite, e.PersonID, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID)
		RecordDBQuery("saveEvent-insert", time.Since(_qStart))
		if err != nil {
			serverError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		e.ID = int(id)
	} else {
		_, err := db.Exec(`UPDATE timeline_events SET 
			title=?, description=?, event_date=?, location=?, media_type=?, media_url=?, thumbnail=?, media_caption=?, tags=?, sort_order=?, is_public=?, is_favorite=?, person_id=?, latitude=?, longitude=?, recurring=?, weather_data=?, event_start_time=?, event_end_time=?, user_id=?
			WHERE id=?`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.SortOrder, e.IsPublic, e.IsFavorite, e.PersonID, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID, e.ID)
		RecordDBQuery("saveEvent-update", time.Since(_qStart))
		if err != nil {
			serverError(c, err)
			return
		}
		action = "updated"
	}

	RecordEventOperation(action)
	span.SetAttributes(attribute.String("action", action))

	sendGotifyNotification(fmt.Sprintf("Event %s: %s (%s)", action, e.Title, e.Date), e.Description)
	c.JSON(http.StatusOK, e)
}

// @Summary Delete event
// @Description Deletes an event by ID
// @Tags Events
// @Produce json
// @Param id query int true "Event ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /events [delete]
func deleteEvent(c *gin.Context) {
	_, span := startSpan(c, "deleteEvent")
	defer span.End()

	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	span.SetAttributes(attribute.Int("event.id", id))

	var title string
	db.QueryRow("SELECT title FROM timeline_events WHERE id=?", id).Scan(&title)

	_qStart := time.Now()
	_, err = db.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=?", id)
	RecordDBQuery("deleteEvent", time.Since(_qStart))
	if err != nil {
		serverError(c, err)
		return
	}

	RecordEventOperation("delete")
	sendGotifyNotification(fmt.Sprintf("Event deleted: %s", title), "")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Get trashed events
// @Description List soft-deleted events in the recycle bin
// @Tags Events
// @Produce json
// @Success 200 {array} object "trashed events"
// @Router /events/trash [get]
func getTrashEvents(c *gin.Context) {
	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time, e.deleted_at,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE e.deleted_at != '' AND e.deleted_at IS NOT NULL ORDER BY e.deleted_at DESC`)
	if err != nil {
		serverError(c, err)
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

// @Summary Restore events from trash
// @Description Restore soft-deleted events
// @Tags Events
// @Accept json
// @Produce json
// @Param ids body object true "Event IDs to restore" SchemaProperties({ids:{type:array,items:{type:integer}}})
// @Success 200 {object} map[string]interface{}
// @Router /events/restore [post]
func restoreEvents(c *gin.Context) {
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

	_, err := db.Exec("UPDATE timeline_events SET deleted_at='' WHERE id IN ("+inClause+")", args...)
	if err != nil {
		serverError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "restored": len(input.IDs)})
}

// @Summary Empty trash
// @Description Permanently delete all trashed events
// @Tags Events
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /events/empty-trash [post]
func emptyTrash(c *gin.Context) {
	res, err := db.Exec("DELETE FROM timeline_events WHERE deleted_at != '' AND deleted_at IS NOT NULL")
	if err != nil {
		serverError(c, err)
		return
	}
	count, _ := res.RowsAffected()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "permanently_deleted": count})
}

// @Summary Upload media file
// @Description Upload an image, video, or audio file. Returns the URL and optional thumbnail URL.
// @Tags Media
// @Accept mpfd
// @Produce json
// @Param image formData file false "Image file"
// @Param video formData file false "Video file"
// @Param audio formData file false "Audio file"
// @Param media_type formData string true "Media type" Enums(image, video, audio)
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /upload [post]
func handleUpload(c *gin.Context) {
	_, span := startSpan(c, "handleUpload")
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

	// MIME type verification
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
			if size.X > 10000 || size.Y > 10000 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Image dimensions too large (max 10000x10000)"})
				return
			}
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

	// EXIF GPS extraction
	var exifLat, exifLng *float64
	if mediaType == "image" {
		exifLat, exifLng = extractEXIFGPS(data)
	}

	sendGotifyNotification(fmt.Sprintf("New media uploaded: %s (%s)", filename, mediaType), url)

	resp := gin.H{
		"url":        url,
		"media_type": mediaType,
		"thumbnail":  thumbnailURL,
	}
	if exifLat != nil && exifLng != nil {
		resp["latitude"] = *exifLat
		resp["longitude"] = *exifLng
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

// @Summary Search events
// @Description Full-text search across events with multiple filters
// @Tags Events
// @Produce json
// @Param q query string false "Search query"
// @Param year query string false "Filter by year"
// @Param tag query string false "Filter by tag"
// @Param person query string false "Filter by person name"
// @Param media_type query string false "Filter by media type"
// @Param location query string false "Filter by location"
// @Param month query string false "Filter by month"
// @Param user_id query int false "Filter by user ID"
// @Success 200 {array} object "timeline events"
// @Router /events/search [get]
func searchEvents(c *gin.Context) {
	query := c.Query("q")
	filters := EventFilters{
		Year: c.Query("year"), Month: c.Query("month"), Tag: c.Query("tag"),
		Person: c.Query("person"), PersonID: c.Query("person_id"),
		MediaType: c.Query("media_type"), Location: c.Query("location"),
		UserID: c.Query("user_id"),
	}

	sqlStr := BuildEventQueryPrefix()
	args := []any{}

	if query != "" {
		ftsOK := true
		ftsQuery := SanitizeFTSQuery(query)
		var ftsCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH ?", ftsQuery).Scan(&ftsCount); err != nil {
			ftsOK = false
		}
		if ftsOK && ftsCount > 0 {
			sqlStr += " AND e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)"
			args = append(args, ftsQuery)
		} else {
			filters.Query = query
		}
	}

	sqlStr, args = appendEventFilters(sqlStr, args, filters)
	sqlStr += buildEventOrder("asc")

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := ScanEvents(rows)
	c.JSON(http.StatusOK, events)
}

// @Summary Autocomplete suggestions
// @Description Returns autocomplete suggestions for location, person, tag, media, or user fields
// @Tags Events
// @Produce json
// @Param field query string true "Field to autocomplete" Enums(location, person, tag, media, user)
// @Param q query string true "Search prefix"
// @Success 200 {array} string
// @Router /autocomplete [get]
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
				for t := range strings.SplitSeq(v, ",") {
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

func globalSearchEvents(c *gin.Context) {
	query := c.Query("q")
	limit := c.Query("limit")

	if query == "" {
		c.JSON(http.StatusOK, []any{})
		return
	}

	l := 10
	if limit != "" {
		if v, err := strconv.Atoi(limit); err == nil && v > 0 {
			l = v
		}
	}

	sqlStr := BuildEventQueryPrefix()
	args := []any{}

	ftsQuery := SanitizeFTSQuery(query)
	var ftsCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH ?", ftsQuery).Scan(&ftsCount); err != nil || ftsCount == 0 {
		sqlStr += " AND (e.title LIKE ? OR e.description LIKE ? OR e.location LIKE ? OR p.name LIKE ?)"
		like := "%" + query + "%"
		args = append(args, like, like, like, like)
	} else {
		sqlStr += " AND e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)"
		args = append(args, ftsQuery)
	}

	sqlStr += " ORDER BY e.event_date DESC LIMIT ?"
	args = append(args, l)

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := ScanEvents(rows)
	c.JSON(http.StatusOK, events)
}

type StatsDistribution struct {
	ByMonth        map[string]int  `json:"by_month"`
	ByWeekday      map[string]int  `json:"by_weekday"`
	ByTag          []TagCount      `json:"by_tag"`
	ByPerson       []PersonCount   `json:"by_person"`
	ByUser         []UserCount     `json:"by_user"`
	ByLocation     []LocationCount `json:"by_location"`
	GeoSpread      float64         `json:"geo_spread"`
	EventCount     int             `json:"event_count"`
	MediaBreakdown map[string]int  `json:"media_breakdown"`
	DailyAvg       float64         `json:"daily_avg"`
	MonthlyAvg     float64         `json:"monthly_avg"`
	TopDay         string          `json:"top_day"`
}

type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type PersonCount struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type UserCount struct {
	ID          int    `json:"id"`
	DisplayName string `json:"display_name"`
	Count       int    `json:"count"`
}

type LocationCount struct {
	Location string  `json:"location"`
	Count    int     `json:"count"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

func getStatsDistribution(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	dist := StatsDistribution{
		ByMonth:        make(map[string]int),
		ByWeekday:      make(map[string]int),
		MediaBreakdown: make(map[string]int),
		ByTag:          make([]TagCount, 0),
		ByPerson:       make([]PersonCount, 0),
		ByUser:         make([]UserCount, 0),
		ByLocation:     make([]LocationCount, 0),
	}

	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", year).Scan(&dist.EventCount)

	daysInYear := 365
	if isLeapYear(year) {
		daysInYear = 366
	}
	if dist.EventCount > 0 {
		dist.DailyAvg = float64(dist.EventCount) / float64(daysInYear)
		dist.MonthlyAvg = float64(dist.EventCount) / 12.0
	}

	dist.ByMonth = QueryMonthlyCounts(db, year)

	wdCounts := QueryWeekdayCounts(db, year)
	maps.Copy(dist.ByWeekday, wdCounts)

	tagResult := QueryTagFrequency(db, year)
	if tagResult != nil {
		dist.ByTag = tagResult
	}

	dist.ByPerson = QueryPersonEventCounts(db, year)
	dist.ByUser = QueryUserEventCounts(db, year)
	dist.ByLocation = QueryLocationCounts(db, year, 20)
	dist.MediaBreakdown = QueryMediaBreakdown(db, year)
	dist.TopDay = QueryTopDay(db, year)

	if len(dist.ByLocation) >= 2 {
		totalDist := 0.0
		pairs := 0
		for i := 0; i < len(dist.ByLocation); i++ {
			for j := i + 1; j < len(dist.ByLocation); j++ {
				totalDist += haversine(dist.ByLocation[i].Lat, dist.ByLocation[i].Lng,
					dist.ByLocation[j].Lat, dist.ByLocation[j].Lng)
				pairs++
			}
		}
		if pairs > 0 {
			dist.GeoSpread = totalDist / float64(pairs)
		}
	}

	c.JSON(http.StatusOK, dist)
}

func isLeapYear(year string) bool {
	y, err := strconv.Atoi(year)
	if err != nil {
		return false
	}
	return (y%4 == 0 && y%100 != 0) || y%400 == 0
}

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLng := (lng2 - lng1) * (math.Pi / 180.0)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// @Summary Get events for a person
// @Description Returns all events associated with a person
// @Tags Persons
// @Produce json
// @Param id path int true "Person ID"
// @Success 200 {array} object "timeline events"
// @Failure 400 {object} map[string]string
// @Router /persons/{id}/events [get]
func getPersonEvents(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
		return
	}

	query, args := BuildEventQuery(EventFilters{PersonID: idStr, Sort: "asc"})
	rows, err := db.Query(query, args...)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := ScanEvents(rows)
	c.JSON(http.StatusOK, events)
	_ = id
}

// @Summary Clone event
// @Description Clone an existing event to a new date
// @Tags Events
// @Accept json
// @Produce json
// @Param body body object true "Clone request" SchemaProperties({id:{type:integer}, date:{type:string}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /events/clone [post]
func cloneEvent(c *gin.Context) {
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
	err := db.QueryRow(`SELECT title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, event_start_time, event_end_time, user_id FROM timeline_events WHERE id = ?`, input.ID).
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

	_, err = db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, event_start_time, event_end_time, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID)
	if err != nil {
		serverError(c, err)
		return
	}

	sendGotifyNotification(fmt.Sprintf("Event cloned: %s", e.Title), "")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Import events
// @Description Import events from JSON or CSV format
// @Tags Events
// @Accept json
// @Accept mpfd
// @Produce json
// @Param format query string false "Import format" Enums(json, csv)
// @Param file formData file false "CSV file (for CSV format)"
// @Param events body array false "Events array (for JSON format)"
// @Success 200 {object} map[string]int
// @Failure 400 {object} map[string]string
// @Router /events/import [post]
func importEvents(c *gin.Context) {
	format := c.Query("format")
	if format == "" {
		format = "json"
	}

	var events []models.TimelineEvent
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
			serverError(c, err)
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
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, latitude, longitude, recurring, weather_data, event_start_time, event_end_time, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.SortOrder, e.Latitude, e.Longitude, e.Recurring, e.WeatherData, e.StartTime, e.EndTime, e.UserID)
		if err == nil {
			count++
		}
	}

	sendGotifyNotification(fmt.Sprintf("Imported %d events", count), "")
	c.JSON(http.StatusOK, gin.H{"imported": count})
}

// @Summary Export events
// @Description Export events as JSON or CSV
// @Tags Events
// @Produce json
// @Produce text/csv
// @Param year query string false "Filter by year"
// @Param format query string false "Export format" Enums(json, csv)
// @Success 200 {object} object "events"
// @Router /events/export [get]
func exportEvents(c *gin.Context) {
	year := c.Query("year")
	format := c.Query("format")
	if format == "" {
		format = "json"
	}

	sqlStr := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id`
	args := []any{}

	if year != "" {
		sqlStr += " WHERE strftime('%Y', e.event_date) = ?"
		args = append(args, year)
	}
	sqlStr += " ORDER BY e.event_date ASC"

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		serverError(c, err)
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

// @Summary Export events as iCalendar
// @Description Export events in iCalendar (.ics) format
// @Tags Events
// @Produce text/calendar
// @Param year query string false "Filter by year"
// @Success 200 {string} string "iCalendar data"
// @Router /events/ics [get]
func getEventsICS(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	sqlStr := `SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time
		FROM timeline_events e
		WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND strftime('%Y', e.event_date) = ?
		ORDER BY e.event_date ASC`
	rows, err := db.Query(sqlStr, year)
	if err != nil {
		serverError(c, err)
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

	eventCount := 0
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

		eventCount++
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

// @Summary Toggle favorite status
// @Description Toggle the favorite status of an event
// @Tags Events
// @Accept json
// @Produce json
// @Param body body object true "Event ID" SchemaProperties(id:{type:integer})
// @Success 200 {object} map[string]interface{}
// @Router /events/favorite [post]
func toggleFavorite(c *gin.Context) {
	var input struct {
		ID int `json:"id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var current bool
	db.QueryRow("SELECT is_favorite FROM timeline_events WHERE id=?", input.ID).Scan(&current)
	_, err := db.Exec("UPDATE timeline_events SET is_favorite=? WHERE id=?", !current, input.ID)
	if err != nil {
		serverError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "is_favorite": !current})
}

// @Summary Batch operations on events
// @Description Batch edit, delete, or export events
// @Tags Events
// @Accept json
// @Produce json
// @Param body body object true "Batch operation" SchemaProperties(ids:{type:array,items:{type:integer}}, action:{type:string}, tags:{type:string}, person_id:{type:integer}, user_id:{type:integer})
// @Success 200 {object} map[string]interface{}
// @Router /events/batch [post]
func batchEvents(c *gin.Context) {
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
		_, err := db.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id IN ("+inClause+")", args...)
		if err != nil {
			serverError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": len(input.IDs)})
	case "permanent_delete":
		_, err := db.Exec("DELETE FROM timeline_events WHERE id IN ("+inClause+")", args...)
		if err != nil {
			serverError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": len(input.IDs)})
	case "add_tags":
		for _, id := range input.IDs {
			var existing string
			db.QueryRow("SELECT COALESCE(tags,'') FROM timeline_events WHERE id=?", id).Scan(&existing)
			newTags := existing
			if input.Tags != "" {
				if existing != "" {
					newTags = existing + ", " + input.Tags
				} else {
					newTags = input.Tags
				}
			}
			db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(input.IDs)})
	case "set_person":
		if input.PersonID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "person_id required"})
			return
		}
		_, err := db.Exec("UPDATE timeline_events SET person_id=? WHERE id IN ("+inClause+")", append([]any{*input.PersonID}, args...)...)
		if err != nil {
			serverError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(input.IDs)})
	case "set_user":
		if input.UserID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user_id required"})
			return
		}
		_, err := db.Exec("UPDATE timeline_events SET user_id=? WHERE id IN ("+inClause+")", append([]any{*input.UserID}, args...)...)
		if err != nil {
			serverError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": len(input.IDs)})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown action"})
	}
}

// @Summary Get collections
// @Description Returns all collections with event counts
// @Tags models.Collections
// @Produce json
// @Success 200 {array} models.Collection
// @Router /collections [get]
func getCollections(c *gin.Context) {
	rows, err := db.Query(`SELECT c.id, c.name, c.description, c.color, c.created_at,
		(SELECT COUNT(*) FROM collection_events ce WHERE ce.collection_id = c.id) as event_count
		FROM collections c ORDER BY c.name`)
	if err != nil {
		serverError(c, err)
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

// @Summary Create or update collection
// @Description Creates a new collection or updates an existing one
// @Tags models.Collections
// @Accept json
// @Produce json
// @Param collection body object true "models.Collection data"
// @Success 200 {object} models.Collection
// @Router /collections [post]
func saveCollection(c *gin.Context) {
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
		col.Color = defaultColor
	}

	if col.ID == 0 {
		result, err := db.Exec("INSERT INTO collections (name, description, color) VALUES (?, ?, ?)", col.Name, col.Description, col.Color)
		if err != nil {
			serverError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		col.ID = int(id)
	} else {
		_, err := db.Exec("UPDATE collections SET name=?, description=?, color=? WHERE id=?", col.Name, col.Description, col.Color, col.ID)
		if err != nil {
			serverError(c, err)
			return
		}
	}
	col.CreatedAt = time.Now().Format(time.RFC3339)
	c.JSON(http.StatusOK, col)
}

// @Summary Delete collection
// @Description Deletes a collection by ID
// @Tags models.Collections
// @Produce json
// @Param id query int true "models.Collection ID"
// @Success 200 {object} map[string]string
// @Router /collections [delete]
func deleteCollection(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid collection ID"})
		return
	}
	db.Exec("DELETE FROM collection_events WHERE collection_id=?", id)
	_, err = db.Exec("DELETE FROM collections WHERE id=?", id)
	if err != nil {
		serverError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Get events in a collection
// @Description Returns all events for a given collection
// @Tags models.Collections
// @Produce json
// @Param id path int true "models.Collection ID"
// @Success 200 {array} models.TimelineEvent
// @Router /collections/{id}/events [get]
func getCollectionEvents(c *gin.Context) {
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
		WHERE ce.collection_id = ?
		ORDER BY e.event_date ASC`

	rows, err := db.Query(query, id)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

// @Summary Add event to collection
// @Description Add an event to a collection
// @Tags models.Collections
// @Accept json
// @Produce json
// @Param id path int true "models.Collection ID"
// @Param body body object true "Event ID" SchemaProperties(event_id:{type:integer})
// @Success 200 {object} map[string]string
// @Router /collections/{id}/events [post]
func addEventToCollection(c *gin.Context) {
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
	_, err = db.Exec("INSERT OR IGNORE INTO collection_events (collection_id, event_id) VALUES (?, ?)", colID, input.EventID)
	if err != nil {
		serverError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Remove event from collection
// @Description Remove an event from a collection
// @Tags models.Collections
// @Produce json
// @Param id path int true "models.Collection ID"
// @Param event_id query int true "Event ID"
// @Success 200 {object} map[string]string
// @Router /collections/{id}/events [delete]
func removeEventFromCollection(c *gin.Context) {
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
	_, err = db.Exec("DELETE FROM collection_events WHERE collection_id=? AND event_id=?", colID, eventID)
	if err != nil {
		serverError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Get event templates
// @Description Returns all event templates
// @Tags Templates
// @Produce json
// @Success 200 {array} models.EventTemplate
// @Router /templates [get]
func getTemplates(c *gin.Context) {
	rows, err := db.Query("SELECT id, title, description, tags, person_id, user_id, location, media_type, created_at FROM event_templates ORDER BY title")
	if err != nil {
		serverError(c, err)
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

// @Summary Create or update template
// @Description Creates a new template or updates an existing one
// @Tags Templates
// @Accept json
// @Produce json
// @Param template body object true "Template data"
// @Success 200 {object} models.EventTemplate
// @Router /templates [post]
func saveTemplate(c *gin.Context) {
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
		result, err := db.Exec("INSERT INTO event_templates (title, description, tags, person_id, user_id, location, media_type) VALUES (?, ?, ?, ?, ?, ?, ?)",
			t.Title, t.Description, t.Tags, pid, t.UserID, t.Location, t.MediaType)
		if err != nil {
			serverError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		t.ID = int(id)
	} else {
		_, err := db.Exec("UPDATE event_templates SET title=?, description=?, tags=?, person_id=?, user_id=?, location=?, media_type=? WHERE id=?",
			t.Title, t.Description, t.Tags, pid, t.UserID, t.Location, t.MediaType, t.ID)
		if err != nil {
			serverError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, t)
}

// @Summary Delete template
// @Description Deletes a template by ID
// @Tags Templates
// @Produce json
// @Param id query int true "Template ID"
// @Success 200 {object} map[string]string
// @Router /templates [delete]
func deleteTemplate(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
		return
	}
	_, err = db.Exec("DELETE FROM event_templates WHERE id=?", id)
	if err != nil {
		serverError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Apply template
// @Description Creates an event from a template with a specified date
// @Tags Templates
// @Accept json
// @Produce json
// @Param body body object true "Apply template" SchemaProperties(template_id:{type:integer}, date:{type:string})
// @Success 200 {object} models.TimelineEvent
// @Router /templates/apply [post]
func applyTemplate(c *gin.Context) {
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
	err := db.QueryRow("SELECT id, title, description, tags, person_id, user_id, location, media_type FROM event_templates WHERE id=?", input.TemplateID).
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

	result, err := db.Exec(`INSERT INTO timeline_events 
		(title, description, event_date, location, media_type, tags, user_id, person_id) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Title, event.Description, event.Date, event.Location, event.MediaType, event.Tags, event.UserID, event.PersonID)
	if err != nil {
		serverError(c, err)
		return
	}
	id, _ := result.LastInsertId()
	event.ID = int(id)

	sendGotifyNotification(fmt.Sprintf("Event created from template: %s", event.Title), event.Description)
	c.JSON(http.StatusOK, event)
}

// @Summary Get wrapped summary
// @Description Returns year-end wrapped summary data
// @Tags Stats
// @Produce json
// @Param year query string false "Year"
// @Success 200 {object} map[string]interface{}
// @Router /wrapped [get]
func getWrapped(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	var total int
	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date)=?", year).Scan(&total)

	var topEventTitle string
	var topEventDate string
	db.QueryRow(`SELECT title, event_date FROM timeline_events 
		WHERE strftime('%Y', event_date)=? 
		ORDER BY (LENGTH(description) - LENGTH(REPLACE(description, ' ', '')) + 1) DESC LIMIT 1`, year).Scan(&topEventTitle, &topEventDate)

	var longestStreak int
	streakRows, _ := db.Query("SELECT event_date FROM timeline_events WHERE strftime('%Y', event_date)=? GROUP BY event_date ORDER BY event_date ASC", year)
	var prevDate time.Time
	currentStreak := 0
	if streakRows != nil {
		for streakRows.Next() {
			var dateStr string
			streakRows.Scan(&dateStr)
			d, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			if !prevDate.IsZero() {
				diff := d.Sub(prevDate).Hours() / 24
				if diff <= 1 {
					currentStreak++
				} else {
					if currentStreak > longestStreak {
						longestStreak = currentStreak
					}
					currentStreak = 1
				}
			} else {
				currentStreak = 1
			}
			prevDate = d
		}
		if currentStreak > longestStreak {
			longestStreak = currentStreak
		}
		streakRows.Close()
	}

	var mostTagsTitle, mostTags string
	db.QueryRow(`SELECT title, tags FROM timeline_events 
		WHERE strftime('%Y', event_date)=? AND tags != '' 
		ORDER BY (LENGTH(tags) - LENGTH(REPLACE(tags, ',', '')) + 1) DESC LIMIT 1`, year).Scan(&mostTagsTitle, &mostTags)

	tagCount := 0
	if mostTags != "" {
		tagCount = len(strings.Split(mostTags, ","))
	}

	var favCount int
	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date)=? AND is_favorite=1", year).Scan(&favCount)

	var topMonth string
	var topMonthCount int
	db.QueryRow(`SELECT CASE CAST(strftime('%m', event_date) AS INTEGER)
		WHEN 1 THEN 'January' WHEN 2 THEN 'February' WHEN 3 THEN 'March'
		WHEN 4 THEN 'April' WHEN 5 THEN 'May' WHEN 6 THEN 'June'
		WHEN 7 THEN 'July' WHEN 8 THEN 'August' WHEN 9 THEN 'September'
		WHEN 10 THEN 'October' WHEN 11 THEN 'November' WHEN 12 THEN 'December' END,
		COUNT(*) as cnt FROM timeline_events
		WHERE strftime('%Y', event_date)=?
		GROUP BY strftime('%m', event_date) ORDER BY cnt DESC LIMIT 1`, year).Scan(&topMonth, &topMonthCount)

	var totalMedia int
	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date)=? AND media_url != ''", year).Scan(&totalMedia)

	monthlyRows, _ := db.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events
		WHERE strftime('%Y', event_date)=? GROUP BY strftime('%m', event_date)`, year)
	byMonth := make(map[string]int)
	if monthlyRows != nil {
		for monthlyRows.Next() {
			var m string
			var c int
			monthlyRows.Scan(&m, &c)
			byMonth[m] = c
		}
		monthlyRows.Close()
	}

	c.JSON(http.StatusOK, gin.H{
		"year":                year,
		"total_events":        total,
		"top_event":           topEventTitle,
		"top_event_date":      topEventDate,
		"longest_streak":      longestStreak,
		"most_tags_title":     mostTagsTitle,
		"most_tags_count":     tagCount,
		"favorite_count":      favCount,
		"busiest_month":       topMonth,
		"busiest_month_count": topMonthCount,
		"total_media":         totalMedia,
		"by_month":            byMonth,
	})
}

// @Summary Get event statistics
// @Description Returns statistics for events in a given year
// @Tags Events
// @Produce json
// @Param year query string false "Filter by year"
// @Success 200 {object} object "event statistics"
// @Router /stats [get]
func getEventStats(c *gin.Context) {
	_, span := startSpan(c, "getEventStats")
	defer span.End()

	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	span.SetAttributes(attribute.String("year", year))

	stats := QueryYearStats(db, year)
	stats.TotalYears = len(stats.ByYear)

	c.JSON(http.StatusOK, stats)
}

// @Summary Create share link
// @Description Creates a shareable link for selected events
// @Tags Sharing
// @Accept json
// @Produce json
// @Param body body object true "Share request" SchemaProperties({event_ids:{type:array,items:{type:integer}}, year:{type:string}, days:{type:integer}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /share/create [post]
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

	var eventIDsStr strings.Builder
	for idx, idVal := range input.EventIDs {
		if idx > 0 {
			eventIDsStr.WriteString(",")
		}
		eventIDsStr.WriteString(strconv.Itoa(idVal))
	}

	expires := time.Now().Add(time.Duration(input.Days) * 24 * time.Hour)

	_, err = db.Exec(`INSERT INTO share_tokens (token, event_ids, year, expires_at) VALUES (?, ?, ?, ?)`,
		token, eventIDsStr.String(), input.Year, expires.Format("2006-01-02"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create share link"})
		return
	}

	sendGotifyNotification("New share link created", fmt.Sprintf("Expires: %s", expires.Format("2006-01-02")))
	c.JSON(http.StatusOK, gin.H{"token": token, "expires": expires.Format("2006-01-02")})
}

// @Summary Get share link
// @Description Redirect to shared view using a share token
// @Tags Sharing
// @Produce json
// @Param token query string true "Share token"
// @Success 302 {string} string "redirect"
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /share [get]
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

// @Summary Get tags
// @Description Returns all unique tags with their counts, optionally filtered by year
// @Tags Events
// @Produce json
// @Param year query string false "Filter by year"
// @Success 200 {array} object "tags with counts"
// @Router /tags [get]
func getTags(c *gin.Context) {
	year := c.Query("year")
	query := "SELECT tags FROM timeline_events WHERE tags != ''"
	args := []any{}

	if year != "" {
		query += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	tagCounts := make(map[string]int)
	for rows.Next() {
		var tagsStr string
		rows.Scan(&tagsStr)
		for t := range strings.SplitSeq(tagsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tagCounts[t]++
			}
		}
	}

	type TagItem struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	result := make([]TagItem, 0, len(tagCounts))
	for name, count := range tagCounts {
		result = append(result, TagItem{Name: name, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	c.JSON(http.StatusOK, result)
}

// @Summary Rename tag
// @Description Rename a tag across all events
// @Tags Tags
// @Accept json
// @Produce json
// @Param body body object true "Rename tag" SchemaProperties({old_name:{type:string}, new_name:{type:string}})
// @Success 200 {object} map[string]string
// @Router /tags/rename [post]
func renameTag(c *gin.Context) {
	var input struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.OldName == "" || input.NewName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both old_name and new_name are required"})
		return
	}

	rows, err := db.Query("SELECT id, tags FROM timeline_events WHERE tags LIKE ?", "%"+input.OldName+"%")
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	updated := 0
	for rows.Next() {
		var id int
		var tags string
		if err := rows.Scan(&id, &tags); err != nil {
			continue
		}
		parts := strings.Split(tags, ",")
		newParts := make([]string, 0, len(parts))
		changed := false
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == input.OldName {
				newParts = append(newParts, input.NewName)
				changed = true
			} else {
				newParts = append(newParts, p)
			}
		}
		if changed {
			newTags := strings.Join(newParts, ", ")
			db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
			updated++
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": updated})
}

// @Summary Delete tag
// @Description Remove a tag from all events
// @Tags Tags
// @Accept json
// @Produce json
// @Param body body object true "Delete tag" SchemaProperties({name:{type:string}})
// @Success 200 {object} map[string]string
// @Router /tags/delete [post]
func deleteTag(c *gin.Context) {
	var input struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tag name is required"})
		return
	}

	rows, err := db.Query("SELECT id, tags FROM timeline_events WHERE tags LIKE ?", "%"+input.Name+"%")
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	updated := 0
	for rows.Next() {
		var id int
		var tags string
		if err := rows.Scan(&id, &tags); err != nil {
			continue
		}
		parts := strings.Split(tags, ",")
		newParts := make([]string, 0, len(parts))
		changed := false
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == input.Name {
				changed = true
			} else {
				newParts = append(newParts, p)
			}
		}
		if changed {
			newTags := strings.Join(newParts, ", ")
			db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
			updated++
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": updated})
}

// @Summary Merge tags
// @Description Replace all occurrences of source tag with target tag
// @Tags Tags
// @Accept json
// @Produce json
// @Param body body object true "Merge tags" SchemaProperties({source:{type:string}, target:{type:string}})
// @Success 200 {object} map[string]string
// @Router /tags/merge [post]
func mergeTags(c *gin.Context) {
	var input struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Source == "" || input.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both source and target are required"})
		return
	}

	rows, err := db.Query("SELECT id, tags FROM timeline_events WHERE tags LIKE ?", "%"+input.Source+"%")
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	updated := 0
	for rows.Next() {
		var id int
		var tags string
		if err := rows.Scan(&id, &tags); err != nil {
			continue
		}
		parts := strings.Split(tags, ",")
		newParts := make([]string, 0, len(parts))
		changed := false
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == input.Source {
				if input.Target != "" && !contains(newParts, input.Target) {
					newParts = append(newParts, input.Target)
				}
				changed = true
			} else if p != "" {
				newParts = append(newParts, p)
			}
		}
		if changed {
			newTags := strings.Join(newParts, ", ")
			db.Exec("UPDATE timeline_events SET tags=? WHERE id=?", newTags, id)
			updated++
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "updated": updated})
}

func contains(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

// @Summary List persons
// @Description Returns all persons with optional search filter
// @Tags Persons
// @Produce json
// @Param q query string false "Search by name"
// @Success 200 {array} object "persons"
// @Router /persons [get]
func getPersons(c *gin.Context) {
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

	rows, err := db.Query(query, args...)
	if err != nil {
		serverError(c, err)
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

// @Summary Create or update person
// @Description Creates a new person or updates an existing one
// @Tags Persons
// @Accept json
// @Produce json
// @Param person body object true "Person data" SchemaProperties({id:{type:integer}, name:{type:string}, avatar_url:{type:string}, bio:{type:string}, birth_date:{type:string}, color:{type:string}})
// @Success 200 {object} object "saved person"
// @Failure 400 {object} map[string]string
// @Router /persons [post]
func savePerson(c *gin.Context) {
	var p models.Person
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if p.ID == 0 {
		result, err := db.Exec("INSERT INTO persons (name, avatar_url, bio, birth_date, color) VALUES (?, ?, ?, ?, ?)",
			p.Name, p.AvatarURL, p.Bio, p.BirthDate, p.Color)
		if err != nil {
			serverError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		p.ID = int(id)
		sendGotifyNotification(fmt.Sprintf("Person created: %s", p.Name), p.Bio)
	} else {
		_, err := db.Exec("UPDATE persons SET name=?, avatar_url=?, bio=?, birth_date=?, color=? WHERE id=?",
			p.Name, p.AvatarURL, p.Bio, p.BirthDate, p.Color, p.ID)
		if err != nil {
			serverError(c, err)
			return
		}
		sendGotifyNotification(fmt.Sprintf("Person updated: %s", p.Name), p.Bio)
	}

	c.JSON(http.StatusOK, p)
}

// @Summary Delete person
// @Description Deletes a person by ID and unlinks their events
// @Tags Persons
// @Produce json
// @Param id query int true "Person ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /persons [delete]
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

// @Summary Get map data
// @Description Returns events with geo-coordinates as a feature collection, optionally filtered by year
// @Tags Events
// @Produce json
// @Param year query string false "Filter by year"
// @Success 200 {object} object "GeoJSON feature collection"
// @Router /map [get]
func getMapData(c *gin.Context) {
	year := c.Query("year")
	query := `SELECT id, title, description, event_date, location, media_type, media_url, latitude, longitude 
		FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND latitude IS NOT NULL AND longitude IS NOT NULL AND latitude != 0 AND longitude != 0`
	args := []any{}

	if year != "" {
		query += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}
	query += " ORDER BY event_date ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		serverError(c, err)
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

// @Summary Get Gotify config
// @Description Returns the current Gotify notification configuration
// @Tags Notifications
// @Produce json
// @Success 200 {object} object "Gotify config"
// @Router /gotify/config [get]
func getGotifyConfig(c *gin.Context) {
	var cfg models.GotifyConfig
	var enabledInt int
	err := db.QueryRow("SELECT url, token, enabled FROM gotify_settings WHERE id = 1").Scan(&cfg.URL, &cfg.Token, &enabledInt)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

// @Summary Save Gotify config
// @Description Saves the Gotify notification configuration
// @Tags Notifications
// @Accept json
// @Produce json
// @Param config body object true "Gotify config" SchemaProperties({url:{type:string}, token:{type:string}, enabled:{type:boolean}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /gotify/config [post]
func saveGotifyConfig(c *gin.Context) {
	var cfg models.GotifyConfig
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
		serverError(c, err)
		return
	}

	gotifyURL = cfg.URL
	gotifyToken = cfg.Token
	gotifyEnabled = cfg.Enabled

	if logService != nil {
		logService.Log("info", "gotify", "Gotify settings saved", nil)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Test Gotify notification
// @Description Sends a test notification via Gotify
// @Tags Notifications
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /gotify/test [post]
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
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Gotify"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Notification sent successfully"})
	} else {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Gotify returned %d", resp.StatusCode)})
	}
}

func getImmichConfig(c *gin.Context) {
	var cfg models.ImmichConfig
	var enabledInt int
	err := db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&cfg.URL, &cfg.APIKey, &enabledInt)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

func saveImmichConfig(c *gin.Context) {
	var cfg models.ImmichConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}

	_, err := db.Exec(`UPDATE immich_settings SET url=?, api_key=?, enabled=? WHERE id=1`, cfg.URL, cfg.APIKey, enabledInt)
	if err != nil {
		serverError(c, err)
		return
	}

	immichURL = cfg.URL
	immichAPIKey = cfg.APIKey
	immichEnabled = cfg.Enabled

	if logService != nil {
		logService.Log("info", "immich", "Immich settings saved", nil)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getUmamiConfig(c *gin.Context) {
	var cfg models.UmamiConfig
	var enabledInt int
	err := db.QueryRow("SELECT url, site_id, enabled FROM umami_settings WHERE id = 1").Scan(&cfg.URL, &cfg.SiteID, &enabledInt)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

func saveUmamiConfig(c *gin.Context) {
	var cfg models.UmamiConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}

	_, err := db.Exec(`UPDATE umami_settings SET url=?, site_id=?, enabled=? WHERE id=1`, cfg.URL, cfg.SiteID, enabledInt)
	if err != nil {
		serverError(c, err)
		return
	}

	umamiURL = cfg.URL
	umamiSiteID = cfg.SiteID

	if logService != nil {
		logService.Log("info", "umami", "Umami settings saved", nil)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getOtelConfig(c *gin.Context) {
	var cfg models.OtelConfig
	var tEnabled, mEnabled, lEnabled int
	err := db.QueryRow("SELECT endpoint, traces_enabled, metrics_enabled, logs_enabled FROM otel_settings WHERE id = 1").Scan(&cfg.Endpoint, &tEnabled, &mEnabled, &lEnabled)
	if err == nil {
		cfg.TracesEnabled = tEnabled == 1
		cfg.MetricsEnabled = mEnabled == 1
		cfg.LogsEnabled = lEnabled == 1
	}
	c.JSON(http.StatusOK, cfg)
}

func saveOtelConfig(c *gin.Context) {
	var cfg models.OtelConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tEnabled := 0
	if cfg.TracesEnabled {
		tEnabled = 1
	}
	mEnabled := 0
	if cfg.MetricsEnabled {
		mEnabled = 1
	}
	lEnabled := 0
	if cfg.LogsEnabled {
		lEnabled = 1
	}

	_, err := db.Exec(`UPDATE otel_settings SET endpoint=?, traces_enabled=?, metrics_enabled=?, logs_enabled=? WHERE id=1`,
		cfg.Endpoint, tEnabled, mEnabled, lEnabled)
	if err != nil {
		serverError(c, err)
		return
	}

	otelEndpoint = cfg.Endpoint
	otelTracesEnabled = cfg.TracesEnabled
	otelMetricsEnabled = cfg.MetricsEnabled
	otelLogsEnabled = cfg.LogsEnabled

	if logService != nil {
		logService.Log("info", "otel", "OTel settings saved", nil)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func testImmich(c *gin.Context) {
	if immichURL == "" || immichAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Immich URL and API key not configured"})
		return
	}

	req, err := http.NewRequest("GET", strings.TrimRight(immichURL, "/")+"/api/server-info/about", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}
	req.Header.Set("x-api-key", immichAPIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Immich: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if logService != nil {
			logService.Log("info", "immich", "Immich connection test successful", nil)
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Connected to Immich successfully"})
	} else {
		body, _ := io.ReadAll(resp.Body)
		if logService != nil {
			logService.Log("error", "immich", "Immich connection test failed: "+string(body), nil)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Immich returned %d: %s", resp.StatusCode, string(body))})
	}
}

type immichTimelineResponse struct {
	Title  string        `json:"title"`
	Assets []immichAsset `json:"assets"`
}

type immichAsset struct {
	ID               string      `json:"id"`
	OriginalFileName string      `json:"originalFileName"`
	Type             string      `json:"type"`
	ExifInfo         *immichExif `json:"exifInfo"`
}

type immichExif struct {
	DateTimeOriginal *string  `json:"dateTimeOriginal"`
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	City             *string  `json:"city"`
	Country          *string  `json:"country"`
}

func fetchImmichMemories(c *gin.Context) {
	if immichURL == "" || immichAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Immich not configured"})
		return
	}

	req, err := http.NewRequest("GET", strings.TrimRight(immichURL, "/")+"/api/timeline/memory", nil)
	if err != nil {
		serverError(c, err)
		return
	}
	req.Header.Set("x-api-key", immichAPIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch memories from Immich: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Immich returned %d: %s", resp.StatusCode, string(body))})
		return
	}

	var timeline []immichTimelineResponse
	if err := json.NewDecoder(resp.Body).Decode(&timeline); err != nil {
		serverError(c, err)
		return
	}

	memories := make([]models.ImmichMemoryAsset, 0)
	for _, group := range timeline {
		for _, asset := range group.Assets {
			lat := 0.0
			lng := 0.0
			if asset.ExifInfo != nil {
				if asset.ExifInfo.Latitude != nil {
					lat = *asset.ExifInfo.Latitude
				}
				if asset.ExifInfo.Longitude != nil {
					lng = *asset.ExifInfo.Longitude
				}
			}
			memories = append(memories, models.ImmichMemoryAsset{
				ID:               asset.ID,
				OriginalFileName: asset.OriginalFileName,
				Type:             asset.Type,
				ThumbnailURL:     strings.TrimRight(immichURL, "/") + "/api/assets/" + asset.ID + "/thumbnail",
				AssetCount:       len(group.Assets),
				MemoryDate:       group.Title,
				Latitude:         lat,
				Longitude:        lng,
				Description:      asset.OriginalFileName,
			})
		}
	}

	c.JSON(http.StatusOK, memories)
}

func importImmichMemories(c *gin.Context) {
	var assetIDs []string
	if err := c.ShouldBindJSON(&assetIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if immichURL == "" || immichAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Immich not configured"})
		return
	}

	if len(assetIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No assets selected"})
		return
	}

	today := time.Now().Format("2006-01-02")
	count := 0

	for _, assetID := range assetIDs {
		req, err := http.NewRequest("GET", strings.TrimRight(immichURL, "/")+"/api/assets/"+assetID, nil)
		if err != nil {
			continue
		}
		req.Header.Set("x-api-key", immichAPIKey)
		req.Header.Set("Accept", "application/json")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		var asset immichAsset
		if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		mediaType := "image"
		if asset.Type == "VIDEO" {
			mediaType = "video"
		} else if asset.Type == "AUDIO" {
			mediaType = "audio"
		}

		var lat, lng *float64
		location := ""
		eventDate := today
		if asset.ExifInfo != nil {
			if asset.ExifInfo.Latitude != nil {
				v := *asset.ExifInfo.Latitude
				lat = &v
			}
			if asset.ExifInfo.Longitude != nil {
				v := *asset.ExifInfo.Longitude
				lng = &v
			}
			if asset.ExifInfo.City != nil && *asset.ExifInfo.City != "" {
				location = *asset.ExifInfo.City
				if asset.ExifInfo.Country != nil && *asset.ExifInfo.Country != "" {
					location += ", " + *asset.ExifInfo.Country
				}
			}
			if asset.ExifInfo.DateTimeOriginal != nil && *asset.ExifInfo.DateTimeOriginal != "" {
				if t, err := time.Parse(time.RFC3339, *asset.ExifInfo.DateTimeOriginal); err == nil {
					eventDate = t.Format("2006-01-02")
				}
			}
		}

		thumbnailURL := strings.TrimRight(immichURL, "/") + "/api/assets/" + asset.ID + "/thumbnail"
		mediaURL := strings.TrimRight(immichURL, "/") + "/api/assets/" + asset.ID + "/original"

		_, err = db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, latitude, longitude, recurring, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			asset.OriginalFileName, asset.OriginalFileName, eventDate, location, mediaType, mediaURL, thumbnailURL, "immich-import", 0, lat, lng, "", "", 0)
		if err == nil {
			count++
		}
	}

	sendGotifyNotification(fmt.Sprintf("Imported %d memories from Immich", count), "")
	c.JSON(http.StatusOK, gin.H{"imported": count, "message": fmt.Sprintf("Successfully imported %d memories from Immich", count)})
}

// @Summary Get memory events
// @Description Returns events from past years that fall within the configured memory window
// @Tags Memories
// @Produce json
// @Success 200 {array} object "memory events"
// @Router /memories [get]
func getMemories(c *gin.Context) {
	_, span := startSpan(c, "getMemories")
	defer span.End()

	var cfg models.MemoriesConfig
	var enabledInt int
	err := db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &cfg.DaysWindow, &cfg.EmailEnabled)
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
		rows, err = db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND strftime('%m-%d', e.event_date) BETWEEN ? AND ?
			ORDER BY e.event_date DESC`, startMD, endMD)
	} else {
		rows, err = db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
			CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago
			FROM timeline_events e
			WHERE e.event_date != ''
			AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER)
			AND (strftime('%m-%d', e.event_date) >= ? OR strftime('%m-%d', e.event_date) <= ?)
			ORDER BY e.event_date DESC`, startMD, endMD)
	}

	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	type MemoryEvent struct {
		models.TimelineEvent
		YearsAgo int `json:"years_ago"`
	}

	memories := make([]MemoryEvent, 0)
	pMap := make(map[int]models.Person)
	pRows, _ := db.Query("SELECT id, name, avatar_url, bio, birth_date, color, created_at FROM persons")
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

// @Summary Get memories config
// @Description Returns the memories/on-this-day configuration
// @Tags Memories
// @Produce json
// @Success 200 {object} object "Memories config"
// @Router /memories/config [get]
func getMemoriesConfig(c *gin.Context) {
	var cfg models.MemoriesConfig
	var enabledInt int
	err := db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &cfg.DaysWindow, &cfg.EmailEnabled)
	if err != nil {
		c.JSON(http.StatusOK, models.MemoriesConfig{Enabled: true, DaysWindow: 3, EmailEnabled: false})
		return
	}
	cfg.Enabled = enabledInt == 1
	c.JSON(http.StatusOK, cfg)
}

// @Summary Save memories config
// @Description Saves the memories/on-this-day configuration
// @Tags Memories
// @Accept json
// @Produce json
// @Param config body object true "Memories config" SchemaProperties({enabled:{type:boolean}, days_window:{type:integer}, email_enabled:{type:boolean}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /memories/config [post]
func saveMemoriesConfig(c *gin.Context) {
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
	_, err := db.Exec(`UPDATE memories_settings SET enabled=?, days_window=?, email_enabled=? WHERE id=1`, enabledInt, cfg.DaysWindow, emailInt)
	if err != nil {
		serverError(c, err)
		return
	}
	if logService != nil {
		logService.Log("info", "memories", "Memories settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Get email config
// @Description Returns the current email/SMTP configuration
// @Tags Email
// @Produce json
// @Success 200 {object} object "Email config"
// @Router /email/config [get]
func getEmailConfig(c *gin.Context) {
	var cfg models.EmailConfig
	var port int
	err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&cfg.SMTPHost, &port, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err != nil {
		c.JSON(http.StatusOK, models.EmailConfig{SMTPPort: 587})
		return
	}
	cfg.SMTPPort = port
	c.JSON(http.StatusOK, cfg)
}

// @Summary Save email config
// @Description Saves the email/SMTP configuration
// @Tags Email
// @Accept json
// @Produce json
// @Param config body object true "Email config" SchemaProperties({smtp_host:{type:string}, smtp_port:{type:integer}, smtp_user:{type:string}, smtp_pass:{type:string}, from_addr:{type:string}, to_addr:{type:string}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /email/config [post]
func saveEmailConfig(c *gin.Context) {
	var cfg models.EmailConfig
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
		serverError(c, err)
		return
	}
	if logService != nil {
		logService.Log("info", "email", "Email settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// @Summary Test email
// @Description Sends a test email using the configured SMTP settings
// @Tags Email
// @Produce json
// @Success 200 {object} map[string]string
// @Router /email/test [post]
func testEmail(c *gin.Context) {
	var cfg models.EmailConfig
	var port int
	err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&cfg.SMTPHost, &port, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err != nil || cfg.SMTPHost == "" || cfg.ToAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email not configured"})
		return
	}
	cfg.SMTPPort = port

	subject := "TRACES Test Email"
	body := "This is a test email from TRACES. If you receive this, your email settings are working correctly."
	if err := sendEmail(cfg, cfg.ToAddr, subject, body); err != nil {
		if logService != nil {
			logService.Log("error", "email", "Email test failed: "+err.Error(), nil)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to send email"})
		return
	}
	if logService != nil {
		logService.Log("info", "email", "Email test sent successfully", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Test email sent successfully"})
}

func sendEmail(cfg models.EmailConfig, toAddr, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	msg := fmt.Appendf(nil, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", cfg.FromAddr, toAddr, subject, body)

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
		if err = client.Rcpt(toAddr); err != nil {
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

	return smtp.SendMail(addr, auth, cfg.FromAddr, []string{toAddr}, msg)
}

// @Summary Send memories email
// @Description Manually trigger sending of memories/on-this-day email
// @Tags Memories
// @Produce json
// @Success 200 {object} map[string]string
// @Router /memories/send [post]
func sendMemoriesEmailHandler(c *gin.Context) {
	var memCfg models.MemoriesConfig
	var enabledInt int
	db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &memCfg.DaysWindow, &memCfg.EmailEnabled)
	memCfg.Enabled = enabledInt == 1

	if !memCfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Memories are disabled"})
		return
	}

	var emailCfg models.EmailConfig
	var port int
	err := db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&emailCfg.SMTPHost, &port, &emailCfg.SMTPUser, &emailCfg.SMTPPass, &emailCfg.FromAddr, &emailCfg.ToAddr)
	if err != nil || emailCfg.SMTPHost == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email not configured"})
		return
	}
	emailCfg.SMTPPort = port

	// Collect recipient emails: per-user emails first, fall back to global to_addr
	var recipients []string
	userRows, err := db.Query("SELECT email FROM users WHERE email != '' AND email IS NOT NULL")
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
		serverError(c, err)
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
		if err := sendEmail(emailCfg, to, subject, body); err != nil {
			lastErr = err
			log.Printf("[MEMORIES] Failed to send email to %s: %v", to, err)
			continue
		}
		sent++
	}

	if sent == 0 {
		if logService != nil {
			logService.Log("error", "memories", "Failed to send memories email: "+lastErr.Error(), nil)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to send email"})
		return
	}

	if logService != nil {
		logService.Log("info", "memories", fmt.Sprintf("Sent %d memories via email to %d recipient(s)", len(memories), sent), nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": fmt.Sprintf("Sent %d memories via email to %d recipient(s)", len(memories), sent)})
}

func scanEventsWithPerson(rows *sql.Rows) []models.TimelineEvent {
	return ScanEvents(rows)
}

// @Summary Get calendar data
// @Description Returns events grouped by date for calendar view
// @Tags Events
// @Produce json
// @Success 200 {object} object "calendar data"
// @Router /calendar [get]
func getCalendar(c *gin.Context) {
	year := c.Query("year")
	month := c.Query("month")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	if month == "" {
		month = fmt.Sprintf("%02d", time.Now().Month())
	}

	y, errY := strconv.Atoi(year)
	m, errM := strconv.Atoi(month)
	if errY != nil || errM != nil || m < 1 || m > 12 || y < 1900 || y > 2100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid year or month"})
		return
	}

	startDate := fmt.Sprintf("%04d-%02d-01", y, m)
	firstDay, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
		return
	}
	lastDay := firstDay.AddDate(0, 1, -1)

	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id
		WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND e.event_date BETWEEN ? AND ?
		ORDER BY e.event_date ASC, e.id ASC`, startDate, lastDay.Format("2006-01-02"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)

	daysMap := make(map[string][]models.TimelineEvent)
	for _, e := range events {
		daysMap[e.Date] = append(daysMap[e.Date], e)
	}

	var calendar []models.CalendarDay
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		dayEvents := daysMap[dateStr]
		sort.Slice(dayEvents, func(i, j int) bool {
			if dayEvents[i].SortOrder != dayEvents[j].SortOrder {
				return dayEvents[i].SortOrder < dayEvents[j].SortOrder
			}
			return dayEvents[i].ID < dayEvents[j].ID
		})
		calendar = append(calendar, models.CalendarDay{
			Date:   dateStr,
			Events: dayEvents,
			Count:  len(dayEvents),
		})
	}

	c.JSON(http.StatusOK, calendar)
}

// @Summary Fetch weather data
// @Description Fetches and stores weather data for an event based on date and location
// @Tags Events
// @Accept json
// @Produce json
// @Param body body object true "Weather request" SchemaProperties({id:{type:integer}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /weather/fetch [post]
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

	eventDate, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
		return
	}

	var apiURL string
	if time.Since(eventDate) < 16*24*time.Hour {
		apiURL = fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
			input.Latitude, input.Longitude, input.Date, input.Date)
	} else {
		apiURL = fmt.Sprintf("https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
			input.Latitude, input.Longitude, input.Date, input.Date)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", apiURL, nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to create request"})
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch weather data"})
		return
	}
	defer resp.Body.Close()

	var weatherResp struct {
		Daily struct {
			Time           []string  `json:"time"`
			TemperatureMax []float64 `json:"temperature_2m_max"`
			TemperatureMin []float64 `json:"temperature_2m_min"`
			WeatherCode    []int     `json:"weathercode"`
			WindSpeedMax   []float64 `json:"wind_speed_10m_max"`
			Humidity       []float64 `json:"relative_humidity_2m_mean"`
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

	weatherCode := 0
	if len(weatherResp.Daily.WeatherCode) > 0 {
		weatherCode = weatherResp.Daily.WeatherCode[0]
	}

	tempMax := 0.0
	if len(weatherResp.Daily.TemperatureMax) > 0 {
		tempMax = weatherResp.Daily.TemperatureMax[0]
	}
	tempMin := 0.0
	if len(weatherResp.Daily.TemperatureMin) > 0 {
		tempMin = weatherResp.Daily.TemperatureMin[0]
	}

	windSpeed := 0.0
	if len(weatherResp.Daily.WindSpeedMax) > 0 {
		windSpeed = weatherResp.Daily.WindSpeedMax[0]
	}

	humidity := 0.0
	if len(weatherResp.Daily.Humidity) > 0 {
		humidity = weatherResp.Daily.Humidity[0]
	}

	condition := weatherCodeToCondition(weatherCode)
	icon := weatherCodeToIcon(weatherCode)

	weather := models.WeatherData{
		Temperature: (tempMax + tempMin) / 2,
		Condition:   condition,
		Icon:        icon,
		WindSpeed:   windSpeed,
		Humidity:    humidity,
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

// @Summary Auto-tag event
// @Description Automatically generates tags for an event using AI/Ollama
// @Tags Events
// @Accept json
// @Produce json
// @Param body body object true "Auto-tag request" SchemaProperties({id:{type:integer}})
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auto-tag [post]
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

	if len(input.Title) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title too long"})
		return
	}
	if len(input.Description) > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Description too long"})
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

	sanitizePrompt := func(s string) string {
		s = strings.ReplaceAll(s, "\n", " ")
		s = strings.ReplaceAll(s, "\r", " ")
		if len(s) > 200 {
			s = s[:200]
		}
		return s
	}

	prompt := fmt.Sprintf(`Suggest 3-5 single-word tags for this event (comma-separated):
Title: %s
Description: %s
Location: %s
Tags:`, sanitizePrompt(input.Title), sanitizePrompt(input.Description), sanitizePrompt(input.Location))

	reqBody, _ := json.Marshal(map[string]any{
		"model":  ollamaModel,
		"prompt": prompt,
		"stream": false,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", ollamaURL+"/api/generate", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Ollama"})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Ollama"})
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
		if t != "" && !strings.HasPrefix(t, "Tags:") && len(t) < 50 {
			cleanTags = append(cleanTags, t)
		}
		if len(cleanTags) >= 10 {
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"tags": cleanTags})
}

// @Summary List users
// @Description Returns all registered users
// @Tags Users
// @Produce json
// @Success 200 {array} object "users"
// @Router /users [get]
func getUsers(c *gin.Context) {
	rows, err := db.Query(`SELECT id, username, display_name, email, color, avatar_url, created_at,
		(SELECT COUNT(*) FROM timeline_events WHERE user_id = users.id) as event_count
		FROM users ORDER BY display_name ASC`)
	if err != nil {
		serverError(c, err)
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

// @Summary Create or update user
// @Description Creates a new user or updates an existing one
// @Tags Users
// @Accept json
// @Produce json
// @Param user body object true "User data"
// @Success 200 {object} object "saved user"
// @Failure 400 {object} map[string]string
// @Router /users [post]
func saveUser(c *gin.Context) {
	var u models.User
	if err := c.ShouldBindJSON(&u); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if u.ID == 0 {
		result, err := db.Exec("INSERT INTO users (username, display_name, email, color, avatar_url) VALUES (?, ?, ?, ?, ?)",
			u.Username, u.DisplayName, u.Email, u.Color, u.AvatarURL)
		if err != nil {
			serverError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		u.ID = int(id)
	} else {
		_, err := db.Exec("UPDATE users SET username=?, display_name=?, email=?, color=?, avatar_url=? WHERE id=?",
			u.Username, u.DisplayName, u.Email, u.Color, u.AvatarURL, u.ID)
		if err != nil {
			serverError(c, err)
			return
		}
	}

	c.JSON(http.StatusOK, u)
}

// @Summary Delete user
// @Description Deletes a user by ID
// @Tags Users
// @Produce json
// @Param id query int true "User ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /users [delete]
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

// @Summary Get events for a user
// @Description Returns all events associated with a specific user
// @Tags Users
// @Produce json
// @Param id path int true "User ID"
// @Success 200 {array} object "timeline events"
// @Failure 400 {object} map[string]string
// @Router /users/{id}/events [get]
func getUserEvents(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND e.user_id = ? ORDER BY e.event_date ASC`, id)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()

	events := scanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

// @Summary Generate recurring events
// @Description Generates events from recurring event templates
// @Tags Events
// @Produce json
// @Success 200 {object} map[string]string
// @Router /events/recurring/generate [post]
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

	var e models.TimelineEvent
	var thumbnail, mediaURL, tags, recurring, weatherData sql.NullString
	err := db.QueryRow(`SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, recurring, weather_data, event_start_time, event_end_time, user_id FROM timeline_events WHERE id = ?`, input.EventID).
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

	tx, err := db.Begin()
	if err != nil {
		serverError(c, err)
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
		serverError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"generated": generated})
}

// @Summary Get Ollama config
// @Description Returns the current Ollama AI configuration
// @Tags AI
// @Produce json
// @Success 200 {object} object "Ollama config"
// @Router /ollama/config [get]
func getOllamaConfig(c *gin.Context) {
	var cfg models.OllamaConfig
	var enabledInt int
	err := db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&cfg.URL, &cfg.Model, &enabledInt)
	if err != nil {
		c.JSON(http.StatusOK, models.OllamaConfig{URL: "http://localhost:11434", Model: "llama3.2", Enabled: false})
		return
	}
	cfg.Enabled = enabledInt == 1
	c.JSON(http.StatusOK, cfg)
}

// @Summary Save Ollama config
// @Description Saves the Ollama AI configuration
// @Tags AI
// @Accept json
// @Produce json
// @Param config body object true "Ollama config"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /ollama/config [post]
func saveOllamaConfig(c *gin.Context) {
	var cfg models.OllamaConfig
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
		serverError(c, err)
		return
	}
	if logService != nil {
		logService.Log("info", "ollama", "Ollama settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func backupDatabase() {
	name := fmt.Sprintf("traces-backup-%s.db", time.Now().Format("2006-01-02-150405"))
	dst := filepath.Join(backupPath, name)
	src, err := os.Open(dbPath)
	if err != nil {
		log.Printf("[Backup] Failed to open source database: %v", err)
		return
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		log.Printf("[Backup] Failed to create backup file: %v", err)
		return
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	if err != nil {
		log.Printf("[Backup] Failed to copy database: %v", err)
		os.Remove(dst)
		return
	}
	log.Printf("[Backup] Database backed up to %s", dst)
}

// @Summary Create backup
// @Description Creates a database backup
// @Tags System
// @Produce json
// @Success 200 {object} map[string]string
// @Router /backup [post]
func handleBackup(c *gin.Context) {
	backupDatabase()
	pruneBackups()
	if logService != nil {
		logService.Log("info", "backup", "Backup created", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type BackupInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Date string `json:"date"`
}

// @Summary List backups
// @Description Lists all database backups
// @Tags System
// @Produce json
// @Success 200 {array} object "backups"
// @Router /backups [get]
func handleListBackups(c *gin.Context) {
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		c.JSON(http.StatusOK, []BackupInfo{})
		return
	}
	var backups []BackupInfo
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "traces-backup-") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			backups = append(backups, BackupInfo{
				Name: e.Name(),
				Size: info.Size(),
				Date: info.ModTime().Format(time.RFC3339),
			})
		}
	}
	if backups == nil {
		backups = []BackupInfo{}
	}
	c.JSON(http.StatusOK, backups)
}

func pruneBackups() {
	var cfg models.BackupConfig
	var autoPruneInt int
	err := db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&cfg.RetentionDays, &autoPruneInt)
	if err != nil {
		log.Printf("[Backup] No backup config found, skipping prune")
		return
	}
	cfg.AutoPrune = autoPruneInt == 1
	if !cfg.AutoPrune {
		return
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 7
	}
	threshold := time.Now().AddDate(0, 0, -cfg.RetentionDays)
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		log.Printf("[Backup] Failed to read backup directory: %v", err)
		return
	}
	pruned := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "traces-backup-") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(threshold) {
				path := filepath.Join(backupPath, e.Name())
				if err := os.Remove(path); err != nil {
					log.Printf("[Backup] Failed to prune backup %s: %v", e.Name(), err)
				} else {
					log.Printf("[Backup] Pruned old backup: %s", e.Name())
					pruned++
				}
			}
		}
	}
	if pruned > 0 {
		log.Printf("[Backup] Pruned %d old backup(s)", pruned)
	}
}

// @Summary Get backup config
// @Description Gets the backup configuration
// @Tags System
// @Produce json
// @Success 200 {object} models.BackupConfig
// @Router /backup/config [get]
func getBackupConfig(c *gin.Context) {
	var cfg models.BackupConfig
	var autoPruneInt int
	err := db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&cfg.RetentionDays, &autoPruneInt)
	if err != nil {
		c.JSON(http.StatusOK, models.BackupConfig{RetentionDays: 7, AutoPrune: true})
		return
	}
	cfg.AutoPrune = autoPruneInt == 1
	c.JSON(http.StatusOK, cfg)
}

// @Summary Save backup config
// @Description Saves the backup configuration
// @Tags System
// @Accept json
// @Produce json
// @Param config body object true "Backup config"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /backup/config [post]
func saveBackupConfig(c *gin.Context) {
	var cfg models.BackupConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if cfg.RetentionDays < 1 {
		cfg.RetentionDays = 7
	}
	autoPruneInt := 0
	if cfg.AutoPrune {
		autoPruneInt = 1
	}
	_, err := db.Exec(`UPDATE backup_settings SET retention_days=?, auto_prune=? WHERE id=1`, cfg.RetentionDays, autoPruneInt)
	if err != nil {
		serverError(c, err)
		return
	}
	if logService != nil {
		logService.Log("info", "backup", "Backup settings saved", nil)
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
		"theme_color": "`+defaultColor+`",
		"icons": [
			{"src": "/static/favicon.svg", "sizes": "any", "type": "image/svg+xml"},
			{"src": "/static/logo.svg", "sizes": "any", "type": "image/svg+xml"}
		]
	}`)
}

func serveServiceWorker(c *gin.Context) {
	c.Header("Content-Type", "application/javascript")
	c.String(http.StatusOK, `const CACHE = 'traces-v1';
self.addEventListener('install', e => { e.waitUntil(caches.open(CACHE).then(c => c.addAll(['/','/static/style.css','/static/js/index.js']))); self.skipWaiting(); });
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

	log.Printf("[DB] Current schema version: %d, target: %d", version, models.CurrentSchemaVersion)

	for version < models.CurrentSchemaVersion {
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
			is_favorite INTEGER DEFAULT 0,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			person_id INTEGER,
			latitude REAL,
			longitude REAL,
			recurring TEXT DEFAULT '',
			weather_data TEXT DEFAULT '',
			event_start_time TEXT DEFAULT '',
			event_end_time TEXT DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS immich_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			api_key TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS umami_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			site_id TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS backup_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			retention_days INTEGER DEFAULT 7,
			auto_prune INTEGER DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS otel_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			endpoint TEXT DEFAULT '',
			traces_enabled INTEGER DEFAULT 0,
			metrics_enabled INTEGER DEFAULT 0,
			logs_enabled INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS event_templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			person_id INTEGER DEFAULT 0,
			user_id INTEGER DEFAULT 0,
			location TEXT DEFAULT '',
			media_type TEXT DEFAULT 'image',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS collection_events (
			collection_id INTEGER NOT NULL,
			event_id INTEGER NOT NULL,
			added_at TEXT DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (collection_id, event_id)
		)`,
		`CREATE TABLE IF NOT EXISTS app_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			severity TEXT NOT NULL DEFAULT 'info',
			source TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			metadata TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS log_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			min_severity TEXT NOT NULL DEFAULT 'warn'
		)`,
	}

	for _, q := range queries {
		_, err := db.Exec(q)
		if err != nil {
			log.Fatal(err)
		}
	}

	createFTS5Table()

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

	var immichCount int
	db.QueryRow("SELECT COUNT(*) FROM immich_settings").Scan(&immichCount)
	if immichCount == 0 {
		db.Exec("INSERT INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)")
	}

	var umamiCount int
	db.QueryRow("SELECT COUNT(*) FROM umami_settings").Scan(&umamiCount)
	if umamiCount == 0 {
		db.Exec("INSERT INTO umami_settings (id, url, site_id, enabled) VALUES (1, '', '', 0)")
	}

	var backupCount int
	db.QueryRow("SELECT COUNT(*) FROM backup_settings").Scan(&backupCount)
	if backupCount == 0 {
		db.Exec("INSERT INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")
	}

	var otelCount int
	db.QueryRow("SELECT COUNT(*) FROM otel_settings").Scan(&otelCount)
	if otelCount == 0 {
		db.Exec("INSERT INTO otel_settings (id, endpoint, traces_enabled, metrics_enabled, logs_enabled) VALUES (1, '', 0, 0, 0)")
	}

}

func createFTS5Table() {
	_, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
		title, description, location, tags,
		content='timeline_events',
		content_rowid='id'
	)`)
	if err != nil {
		log.Printf("[DB] Warning: FTS5 not available, full-text search disabled: %v", err)
		return
	}
	db.Exec(`CREATE TRIGGER IF NOT EXISTS events_fts_ai AFTER INSERT ON timeline_events BEGIN
		INSERT INTO events_fts(rowid, title, description, location, tags)
		VALUES (new.id, new.title, new.description, new.location, COALESCE(new.tags, ''));
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS events_fts_ad AFTER DELETE ON timeline_events BEGIN
		INSERT INTO events_fts(events_fts, rowid, title, description, location, tags)
		VALUES('delete', old.id, old.title, old.description, old.location, COALESCE(old.tags, ''));
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS events_fts_au AFTER UPDATE ON timeline_events BEGIN
		INSERT INTO events_fts(events_fts, rowid, title, description, location, tags)
		VALUES('delete', old.id, old.title, old.description, old.location, COALESCE(old.tags, ''));
		INSERT INTO events_fts(rowid, title, description, location, tags)
		VALUES (new.id, new.title, new.description, new.location, COALESCE(new.tags, ''));
	END`)

	var ftsCount int
	db.QueryRow("SELECT COUNT(*) FROM events_fts").Scan(&ftsCount)
	if ftsCount == 0 {
		db.Exec(`INSERT INTO events_fts(rowid, title, description, location, tags)
			SELECT id, title, description, location, COALESCE(tags, '') FROM timeline_events`)
		log.Printf("[DB] Indexed %d events for full-text search", ftsCount)
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
	case 7:
		createFTS5Table()
	case 8:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN is_favorite INTEGER DEFAULT 0`)
	case 9:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS event_templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			person_id INTEGER DEFAULT 0,
			user_id INTEGER DEFAULT 0,
			location TEXT DEFAULT '',
			media_type TEXT DEFAULT 'image',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
	case 10:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			color TEXT DEFAULT '#7c3aed',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS collection_events (
			collection_id INTEGER NOT NULL,
			event_id INTEGER NOT NULL,
			added_at TEXT DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (collection_id, event_id)
		)`)
	case 11:
	case 12:
	case 13:
	case 14:
	case 15:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN event_start_time TEXT DEFAULT ''`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN event_end_time TEXT DEFAULT ''`)
	case 16:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN deleted_at TEXT DEFAULT ''`)
	case 17:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS umami_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			url TEXT DEFAULT '',
			site_id TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0
		)`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO umami_settings (id, url, site_id, enabled) VALUES (1, '', '', 0)`)
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
	events := []models.TimelineEvent{
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
		db.Exec("INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, latitude, longitude, is_public) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)",
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
	if !gotifyEnabled || gotifyURL == "" || gotifyToken == "" {
		return
	}

	payload := map[string]any{
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

var mdRenderer = goldmark.New()

func RenderMarkdown(text string) string {
	if text == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(text), &buf); err != nil {
		return EscapeHtml(text)
	}
	return buf.String()
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
