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
	"database/sql"
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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/trace"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"traces/internal/auth"
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

var (
	db                 *sql.DB
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
	authSvc            *auth.Service
	authSessions       *auth.SessionStore
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

	authSessions = auth.NewSessionStore()
	authSvc = auth.New(auth.Deps{DB: db, Log: logService, Renderer: htmxRenderer, Sessions: authSessions, Integrations: integrationsSvc, PublicMode: func() bool { return publicMode }})
	authSvc.InitOIDCFromEnv()

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
		api.POST("/login", authSvc.HandleLogin)
		api.POST("/logout", authSvc.HandleLogout)
		api.GET("/auth/oidc/login", authSvc.HandleOIDCLogin)
		api.GET("/auth/oidc/callback", authSvc.HandleOIDCCallback)
		api.GET("/auth/oidc/logout", authSvc.HandleOIDCLogout)
		api.GET("/public", eventsSvc.GetPublicEvents)
		api.GET("/share", eventsSvc.GetShareLink)
		api.GET("/config", getPublicConfig)
		api.GET("/manifest.json", serveManifest)
		api.GET("/health", handleHealth)
		api.GET("/trmnl/summary", integrationsSvc.GetTRMNLSummary)
		r.GET("/metrics", gin.WrapH(promhttp.Handler()))
		api.GET("/sw.js", serveServiceWorker)

		auth := api.Group("")
		auth.Use(authSvc.AuthMiddlewareGin(), authSvc.CSRFMiddleware())
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
			auth.GET("/users", authSvc.GetUsers)
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
			auth.POST("/users", authSvc.SaveUser)
			auth.DELETE("/users", authSvc.DeleteUser)
			auth.GET("/users/:id/events", authSvc.GetUserEvents)
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
			auth.GET("/csrf-token", authSvc.GetCSRFToken)
			// Log endpoints
			auth.GET("/logs", logService.HandleGetLogs)
			auth.GET("/logs/count", logService.HandleGetLogCount)
			auth.DELETE("/logs", logService.HandleClearLogs)
			auth.GET("/logs/settings", logService.HandleGetLogSettings)
			auth.POST("/logs/settings", logService.HandleUpdateLogSettings)
			auth.GET("/logs/sources", logService.HandleGetLogSources)
		}
	}

	adminHTMX := r.Group("/api/admin")
	adminHTMX.Use(authSvc.AuthMiddlewareGin(), authSvc.CSRFMiddleware())
	eventsSvc.RegisterHTMXRoutes(adminHTMX)

	r.GET("/sw.js", serveServiceWorker)

	r.GET("/admin.html", func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err == nil {
			if sess, ok := authSessions.Get(cookie); ok && time.Now().Unix() <= sess.ExpiresAt {
				c.File(filepath.Join(basePath, "static/admin.html"))
				return
			}
		}
		c.Redirect(http.StatusFound, "/login.html")
	})

	r.GET("/login.html", func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err == nil {
			if sess, ok := authSessions.Get(cookie); ok && time.Now().Unix() <= sess.ExpiresAt {
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
			authSessions.CleanupExpired(time.Now().Unix())
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

// @Summary Get public config
// @Description Returns public configuration (umami analytics settings)
// @Tags Info
// @Produce json
// @Success 200 {object} map[string]string
// @Router /config [get]
func getPublicConfig(c *gin.Context) {
	uURL, uSite, uEnabled := integrationsSvc.UmamiSettings()
	oidcEnabled := false
	if authSvc != nil {
		oidcEnabled = authSvc.OIDCReady()
	}
	c.JSON(http.StatusOK, gin.H{
		"umami_url":     uURL,
		"umami_site":    uSite,
		"umami_enabled": uEnabled,
		"oidc_enabled":  oidcEnabled,
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
