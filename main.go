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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"traces/internal/database"
	"traces/internal/events"
	"traces/internal/httpx"
	"traces/internal/integrations"
	"traces/internal/logging"
	"traces/internal/media"
	"traces/internal/models"
	"traces/internal/telemetry"
)

func init() {
	image.RegisterFormat("png", "png", png.Decode, png.DecodeConfig)
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
}

var (
	publicMode bool = false
)

// sessionInfo carries the identity and expiry of an authenticated session.
// userID 0 means the admin (legacy behaviour); any other value is a users.id
// from a family login.
type sessionInfo struct {
	userID    int64
	expiresAt int64
}

var (
	db                 *sql.DB
	sessionStore       = make(map[string]sessionInfo)
	csrfTokens         = make(map[string]string)
	sessionMu          sync.RWMutex
	basePath           = "/app"
	dbPath             = "/db/traces.db"
	mediaPath          = "/app/media"
	backupPath         = "/db/backups"
	otelEndpoint       = ""
	otelTracesEnabled  bool
	otelMetricsEnabled bool
	otelLogsEnabled    bool
	otelServiceName    = "traces"
	logService         *logging.LogService
	tel                *telemetry.Telemetry
	mediaSvc           *media.Media
	htmxRenderer       *httpx.Renderer
	integrationsSvc    *integrations.Service
	eventsSvc          *events.Service
)

func currentTracer() trace.Tracer {
	if tel != nil {
		return tel.Tracer()
	}
	return nil
}

func main() {
	if os.Getenv("DOCKER") != "true" {
		basePath = "."
		dbPath = "./traces.db"
		mediaPath = filepath.Join(basePath, "media")
		backupPath = filepath.Join(basePath, "backups")
	}

	// Capture provider env so composition root can seed integrations.Service after DB init.
	envGotifyURL := os.Getenv("GOTIFY_URL")
	envGotifyToken := os.Getenv("GOTIFY_TOKEN")
	envGotifyEnabled := os.Getenv("GOTIFY_ENABLED") == "true"
	envUmamiURL := os.Getenv("UMAMI_URL")
	envUmamiSiteID := os.Getenv("UMAMI_SITE_ID")
	envImmichURL := os.Getenv("IMMICH_URL")
	envImmichAPIKey := os.Getenv("IMMICH_API_KEY")
	envImmichEnabled := os.Getenv("IMMICH_ENABLED") == "true"
	envBGGUsername := os.Getenv("BGG_USERNAME")
	envBGGEnabled := os.Getenv("BGG_ENABLED") == "true"

	if err := os.MkdirAll(mediaPath, 0755); err != nil {
		log.Printf("Warning: could not create media directory: %v", err)
	}
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		log.Printf("Warning: could not create backup directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Printf("Warning: could not create database directory: %v", err)
	}

	mediaSvc = media.New(media.LoadConfig(mediaPath))

	var err error
	db, err = database.Init(database.Config{DBPath: dbPath, MediaPath: mediaPath})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	database.Migrate(db)
	publicMode = os.Getenv("PUBLIC_MODE") == "true"
	database.SeedEvents(db, basePath)
	var err2 error
	htmxRenderer, err2 = httpx.NewRenderer()
	if err2 != nil {
		log.Fatalf("[HTML] Failed to parse templates: %v", err2)
	}

	// Initialize the logging service
	logService = logging.New(db, func() bool { return otelLogsEnabled })
	if err := logService.Init(); err != nil {
		log.Printf("[LogService] Failed to initialize: %v", err)
	}

	integrationsSvc = integrations.New(db, logService, func(t, m, l bool) { otelTracesEnabled, otelMetricsEnabled, otelLogsEnabled = t, m, l })
	integrationsSvc.SetGotify(envGotifyURL, envGotifyToken, envGotifyEnabled)
	integrationsSvc.SetUmami(envUmamiURL, envUmamiSiteID, false)
	integrationsSvc.SetImmich(envImmichURL, envImmichAPIKey, envImmichEnabled)
	integrationsSvc.SetBGG(envBGGUsername, "", envBGGEnabled)
	integrationsSvc.SetPublicMode(func() bool { return publicMode })
	eventsSvc = events.New(events.Deps{DB: db, Log: logService, Renderer: htmxRenderer, Integrations: integrationsSvc, Media: mediaSvc, PublicMode: func() bool { return publicMode }, Tracer: currentTracer})

	if envImmichURL == "" {
		var cfg models.ImmichConfig
		var enabledInt int
		if err := db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&cfg.URL, &cfg.APIKey, &enabledInt); err == nil {
			integrationsSvc.SetImmich(cfg.URL, cfg.APIKey, enabledInt == 1)
		}
	}

	{
		var cfg models.BGGConfig
		var enabledInt int
		if err := db.QueryRow("SELECT username, enabled, last_sync FROM bgg_settings WHERE id = 1").Scan(&cfg.Username, &enabledInt, &cfg.LastSync); err == nil {
			integrationsSvc.SetBGG(cfg.Username, cfg.LastSync, enabledInt == 1)
		}
	}

	if envUmamiURL == "" {
		var cfg models.UmamiConfig
		var enabledInt int
		if err := db.QueryRow("SELECT url, site_id, enabled FROM umami_settings WHERE id = 1").Scan(&cfg.URL, &cfg.SiteID, &enabledInt); err == nil {
			integrationsSvc.SetUmami(cfg.URL, cfg.SiteID, enabledInt == 1)
		}
	}

	// Load OTel settings from DB, then let standard OTel env vars take
	// precedence (OTEL_EXPORTER_OTLP_ENDPOINT / *_EXPORTER=none).
	var tEnabled, mEnabled, lEnabled int
	if err := db.QueryRow("SELECT endpoint, traces_enabled, metrics_enabled, logs_enabled FROM otel_settings WHERE id = 1").Scan(&otelEndpoint, &tEnabled, &mEnabled, &lEnabled); err == nil {
		otelTracesEnabled = tEnabled == 1
		otelMetricsEnabled = mEnabled == 1
		otelLogsEnabled = lEnabled == 1
	}
	if envEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); envEndpoint != "" {
		otelEndpoint = envEndpoint
		otelTracesEnabled = os.Getenv("OTEL_TRACES_EXPORTER") != "none"
		otelMetricsEnabled = os.Getenv("OTEL_METRICS_EXPORTER") != "none"
		otelLogsEnabled = os.Getenv("OTEL_LOGS_EXPORTER") != "none"
	}

	if os.Getenv("BACKUP_RETENTION_DAYS") != "" {
		if days, err := strconv.Atoi(os.Getenv("BACKUP_RETENTION_DAYS")); err == nil && days > 0 {
			db.Exec("UPDATE backup_settings SET retention_days=? WHERE id=1", days)
		}
	}

	initOIDCFromEnv()

	r := gin.Default()
	r.MaxMultipartMemory = 32 << 20

	var tpErr error
	tel, tpErr = telemetry.New(telemetry.Config{Endpoint: otelEndpoint, ServiceName: otelServiceName, TracesEnabled: otelTracesEnabled, MetricsEnabled: otelMetricsEnabled, LogsEnabled: otelLogsEnabled})
	if tpErr != nil {
		log.Printf("Failed to initialize telemetry: %v", tpErr)
	} else {
		r.Use(otelgin.Middleware(tel.ServiceName()))
		r.Use(tel.MetricsMiddleware())
	}
	integrationsSvc.SetTracer(currentTracer())
	integrationsSvc.SetOtel(otelEndpoint, otelTracesEnabled, otelMetricsEnabled, otelLogsEnabled)
	shutdownTelemetry := func() {
		if tel != nil {
			tel.Shutdown()
		}
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
		api.GET("/auth/oidc/login", handleOIDCLogin)
		api.GET("/auth/oidc/callback", handleOIDCCallback)
		api.GET("/auth/oidc/logout", handleOIDCLogout)
		api.GET("/public", eventsSvc.GetPublicEvents)
		api.GET("/share", eventsSvc.GetShareLink)
		api.GET("/config", getPublicConfig)
		api.GET("/manifest.json", serveManifest)
		api.GET("/health", handleHealth)
		api.GET("/trmnl/summary", integrationsSvc.GetTRMNLSummary)
		r.GET("/metrics", gin.WrapH(promhttp.Handler()))
		api.GET("/sw.js", serveServiceWorker)

		auth := api.Group("")
		auth.Use(authMiddlewareGin(), csrfMiddleware())
		{
			auth.GET("/events", eventsSvc.GetEvents)
			auth.GET("/events/full", eventsSvc.GetEventsFull)
			auth.GET("/events/search", eventsSvc.SearchEvents)
			auth.GET("/events/search/global", eventsSvc.GlobalSearchEvents)
			auth.GET("/events/export", eventsSvc.ExportEvents)
			auth.GET("/events/ics", eventsSvc.GetEventsICS)
			auth.GET("/contributions", eventsSvc.GetContributions)
			auth.GET("/stats", eventsSvc.GetEventStats)
			auth.GET("/stats/distribution", eventsSvc.GetStatsDistribution)
			auth.GET("/tags", eventsSvc.GetTags)
			auth.POST("/tags/rename", eventsSvc.RenameTag)
			auth.POST("/tags/delete", eventsSvc.DeleteTag)
			auth.POST("/tags/merge", eventsSvc.MergeTags)
			auth.GET("/map", eventsSvc.GetMapData)
			auth.GET("/persons", eventsSvc.GetPersons)
			auth.GET("/autocomplete", eventsSvc.GetAutocomplete)
			auth.GET("/calendar", eventsSvc.GetCalendar)
			auth.GET("/users", getUsers)
			auth.POST("/events", eventsSvc.SaveEvent)
			auth.DELETE("/events", eventsSvc.DeleteEvent)
			auth.POST("/upload", eventsSvc.HandleUpload)
			auth.POST("/events/clone", eventsSvc.CloneEvent)
			auth.POST("/events/import", eventsSvc.ImportEvents)
			auth.POST("/share/create", eventsSvc.CreateShareLink)
			auth.POST("/persons", eventsSvc.SavePerson)
			auth.DELETE("/persons", eventsSvc.DeletePerson)
			auth.GET("/persons/:id/events", eventsSvc.GetPersonEvents)
			auth.GET("/gotify/config", integrationsSvc.GetGotifyConfig)
			auth.POST("/gotify/config", integrationsSvc.SaveGotifyConfig)
			auth.POST("/gotify/test", integrationsSvc.TestGotify)
			auth.GET("/memories", integrationsSvc.GetMemories)
			auth.GET("/memories/config", integrationsSvc.GetMemoriesConfig)
			auth.POST("/memories/config", integrationsSvc.SaveMemoriesConfig)
			auth.POST("/memories/send", integrationsSvc.SendMemoriesEmailHandler)
			auth.GET("/email/config", integrationsSvc.GetEmailConfig)
			auth.POST("/email/config", integrationsSvc.SaveEmailConfig)
			auth.POST("/email/test", integrationsSvc.TestEmail)
			auth.POST("/weather/fetch", eventsSvc.FetchWeather)
			auth.POST("/auto-tag", integrationsSvc.AutoTagEvent)
			auth.POST("/users", saveUser)
			auth.DELETE("/users", deleteUser)
			auth.GET("/users/:id/events", getUserEvents)
			auth.POST("/events/recurring/generate", eventsSvc.GenerateRecurringEvents)
			auth.GET("/ollama/config", integrationsSvc.GetOllamaConfig)
			auth.POST("/ollama/config", integrationsSvc.SaveOllamaConfig)
			auth.GET("/immich/config", integrationsSvc.GetImmichConfig)
			auth.POST("/immich/config", integrationsSvc.SaveImmichConfig)
			auth.POST("/immich/test", integrationsSvc.TestImmich)
			auth.GET("/immich/memories", integrationsSvc.FetchImmichMemories)
			auth.POST("/immich/import", integrationsSvc.ImportImmichMemories)
			auth.GET("/bgg/config", integrationsSvc.GetBGGConfig)
			auth.POST("/bgg/config", integrationsSvc.SaveBGGConfig)
			auth.POST("/bgg/test", integrationsSvc.TestBGG)
			auth.POST("/bgg/sync", integrationsSvc.SyncBGGHandler)
			auth.POST("/bgg/seed", integrationsSvc.SeedBGGForTest)
			auth.GET("/umami/config", integrationsSvc.GetUmamiConfig)
			auth.POST("/umami/config", integrationsSvc.SaveUmamiConfig)
			auth.GET("/otel/config", integrationsSvc.GetOtelConfig)
			auth.POST("/otel/config", integrationsSvc.SaveOtelConfig)
			auth.POST("/backup", handleBackup)
			auth.GET("/backups", handleListBackups)
			auth.GET("/backup/config", getBackupConfig)
			auth.POST("/backup/config", saveBackupConfig)
			auth.GET("/events/trash", eventsSvc.GetTrashEvents)
			auth.POST("/events/restore", eventsSvc.RestoreEvents)
			auth.POST("/events/empty-trash", eventsSvc.EmptyTrash)
			auth.POST("/events/favorite", eventsSvc.ToggleFavorite)
			auth.POST("/events/batch", eventsSvc.BatchEvents)
			auth.GET("/collections", eventsSvc.GetCollections)
			auth.POST("/collections", eventsSvc.SaveCollection)
			auth.DELETE("/collections", eventsSvc.DeleteCollection)
			auth.GET("/collections/:id/events", eventsSvc.GetCollectionEvents)
			auth.POST("/collections/:id/events", eventsSvc.AddEventToCollection)
			auth.DELETE("/collections/:id/events", eventsSvc.RemoveEventFromCollection)
			auth.GET("/templates", eventsSvc.GetTemplates)
			auth.POST("/templates", eventsSvc.SaveTemplate)
			auth.DELETE("/templates", eventsSvc.DeleteTemplate)
			auth.POST("/templates/apply", eventsSvc.ApplyTemplate)
			auth.GET("/wrapped", eventsSvc.GetWrapped)
			auth.GET("/csrf-token", getCSRFToken)
			// Log endpoints
			auth.GET("/logs", logService.HandleGetLogs)
			auth.GET("/logs/count", logService.HandleGetLogCount)
			auth.DELETE("/logs", logService.HandleClearLogs)
			auth.GET("/logs/settings", logService.HandleGetLogSettings)
			auth.POST("/logs/settings", logService.HandleUpdateLogSettings)
			auth.GET("/logs/sources", logService.HandleGetLogSources)
		}
	}

	registerHTMXRoutes(r)

	r.GET("/sw.js", serveServiceWorker)

	r.GET("/admin.html", func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err == nil {
			sessionMu.RLock()
			sess, ok := sessionStore[cookie]
			sessionMu.RUnlock()
			if ok && time.Now().Unix() <= sess.expiresAt {
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
			sess, ok := sessionStore[cookie]
			sessionMu.RUnlock()
			if ok && time.Now().Unix() <= sess.expiresAt {
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

	r.GET("/api-docs", func(c *gin.Context) {
		c.File(filepath.Join(basePath, "docs/swagger.json"))
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
				if now > v.expiresAt {
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

	// Run until interrupted, then flush telemetry before exiting.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe() }()

	select {
	case err := <-srvErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Server error: %v", err)
		}
	case <-ctx.Done():
		log.Println("Shutdown signal received, draining...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	shutdownTelemetry()
}

// currentUser is the resolved identity of the logged-in account.
// ID 0 is the admin; any other value is a users.id from a family login.
type currentUser struct {
	ID    int64
	Name  string
	Color string
}

const (
	ctxKeyUserID    = "current_user_id"
	ctxKeyUserName  = "current_user_name"
	ctxKeyUserColor = "current_user_color"
)

// getCurrentUser returns the identity resolved by authMiddlewareGin.
// Falls back to the admin identity when middleware did not run (tests).
func getCurrentUser(c *gin.Context) currentUser {
	id, _ := c.Get(ctxKeyUserID)
	uid, _ := id.(int64)
	name, _ := c.Get(ctxKeyUserName)
	uname, _ := name.(string)
	color, _ := c.Get(ctxKeyUserColor)
	ucolor, _ := color.(string)
	return currentUser{ID: uid, Name: uname, Color: ucolor}
}

// resolveSessionUser maps a session's userID to a display identity.
// userID 0 (or an unknown/deleted user) resolves to the admin identity,
// preserving legacy behaviour for expiry-only sessions.
func resolveSessionUser(userID int64) currentUser {
	if userID != 0 {
		var name, color string
		err := db.QueryRow("SELECT COALESCE(NULLIF(display_name,''), username), COALESCE(color, ?) FROM users WHERE id = ?", models.DefaultColor, userID).Scan(&name, &color)
		if err == nil {
			return currentUser{ID: userID, Name: name, Color: color}
		}
	}
	return currentUser{ID: 0, Name: "Admin", Color: models.DefaultColor}
}

func authMiddlewareGin() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		sessionMu.RLock()
		sess, ok := sessionStore[cookie]
		sessionMu.RUnlock()
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			return
		}
		if time.Now().Unix() > sess.expiresAt {
			sessionMu.Lock()
			delete(sessionStore, cookie)
			delete(csrfTokens, cookie)
			sessionMu.Unlock()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			return
		}
		cu := resolveSessionUser(sess.userID)
		c.Set(ctxKeyUserID, cu.ID)
		c.Set(ctxKeyUserName, cu.Name)
		c.Set(ctxKeyUserColor, cu.Color)
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

// @Summary Get public config
// @Description Returns public configuration (umami analytics settings)
// @Tags Info
// @Produce json
// @Success 200 {object} map[string]string
// @Router /config [get]
func getPublicConfig(c *gin.Context) {
	uURL, uSite, uEnabled := integrationsSvc.UmamiSettings()
	c.JSON(http.StatusOK, gin.H{
		"umami_url":     uURL,
		"umami_site":    uSite,
		"umami_enabled": uEnabled,
		"oidc_enabled":  oidcReady(),
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

		db.Exec("INSERT OR IGNORE INTO users (id, username, display_name, email, color) VALUES (1, ?, ?, '', ?)", input.Username, input.Username, models.DefaultColor)

		sessionID, err := generateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session"})
			return
		}
		sessionMu.Lock()
		sessionStore[sessionID] = sessionInfo{userID: 0, expiresAt: time.Now().Add(24 * time.Hour).Unix()}
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

	// Admin credentials are checked first; family accounts (users rows with a
	// bcrypt password hash) are accepted through the same login flow.
	var adminID int
	var adminHash string
	err := db.QueryRow("SELECT id, password FROM admin_users WHERE username = ?", input.Username).Scan(&adminID, &adminHash)
	if err == nil {
		if bcryptErr := bcrypt.CompareHashAndPassword([]byte(adminHash), []byte(input.Password)); bcryptErr != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
			return
		}
		createSession(c, 0)
		return
	}

	var familyID int64
	var familyHash string
	famErr := db.QueryRow("SELECT id, COALESCE(password_hash, '') FROM users WHERE username = ?", input.Username).Scan(&familyID, &familyHash)
	if famErr != nil || familyHash == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(familyHash), []byte(input.Password)); bcryptErr != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	createSession(c, familyID)
}

// createSession mints an identity-stamped session, sets the cookie and CSRF token.
func createSession(c *gin.Context, userID int64) {
	sessionID, err := generateSessionID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate session"})
		return
	}
	sessionMu.Lock()
	sessionStore[sessionID] = sessionInfo{userID: userID, expiresAt: time.Now().Add(24 * time.Hour).Unix()}
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

// @Summary Create or update user
// @Description Creates a new user or updates an existing one. Providing a password creates or replaces the family member's login credentials (bcrypt-hashed).
// @Tags Users
// @Accept json
// @Produce json
// @Param user body object true "User data" SchemaProperties(id:{type:integer}, username:{type:string}, display_name:{type:string}, email:{type:string}, color:{type:string}, avatar_url:{type:string}, password:{type:string})
// @Success 200 {object} object "saved user"
// @Failure 400 {object} map[string]string
// @Router /users [post]
func saveUser(c *gin.Context) {
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
		// Family usernames must not shadow the admin login.
		var adminCount int
		db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE username = ?", u.Username).Scan(&adminCount)
		if adminCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username already taken"})
			return
		}
		var userCount int
		db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", u.Username).Scan(&userCount)
		if userCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username already taken"})
			return
		}

		result, err := db.Exec("INSERT INTO users (username, display_name, email, color, avatar_url, password_hash) VALUES (?, ?, ?, ?, ?, ?)",
			u.Username, u.DisplayName, u.Email, u.Color, u.AvatarURL, passwordHash)
		if err != nil {
			httpx.ServerError(c, err)
			return
		}
		id, _ := result.LastInsertId()
		u.ID = int(id)
	} else {
		if passwordHash != "" {
			_, err := db.Exec("UPDATE users SET username=?, display_name=?, email=?, color=?, avatar_url=?, password_hash=? WHERE id=?",
				u.Username, u.DisplayName, u.Email, u.Color, u.AvatarURL, passwordHash, u.ID)
			if err != nil {
				httpx.ServerError(c, err)
				return
			}
		} else {
			_, err := db.Exec("UPDATE users SET username=?, display_name=?, email=?, color=?, avatar_url=? WHERE id=?",
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
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	events := events.ScanEventsWithPerson(rows)
	c.JSON(http.StatusOK, events)
}

// @Summary Generate recurring events
// @Description Generates events from recurring event templates
// @Tags Events
// @Produce json
// @Success 200 {object} map[string]string
// @Router /events/recurring/generate [post]
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
		httpx.ServerError(c, err)
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
		"theme_color": "`+models.DefaultColor+`",
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

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
