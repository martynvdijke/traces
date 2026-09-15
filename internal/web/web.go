package web

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"traces/internal/auth"
	"traces/internal/httpx"
	"traces/internal/logging"
	"traces/internal/models"
)

// Deps are injected by the composition root. No import of traces or internal/events.
type Deps struct {
	DB         *sql.DB
	Log        *logging.LogService
	Sessions   *auth.SessionStore
	BasePath   string
	DBPath     string
	BackupPath string
	Umami      func() (url, site string, enabled bool)
	OIDCReady  func() bool
}

// Service owns residual infra HTTP endpoints.
type Service struct {
	db         *sql.DB
	log        *logging.LogService
	sessions   *auth.SessionStore
	basePath   string
	dbPath     string
	backupPath string
	umami      func() (string, string, bool)
	oidcReady  func() bool
}

func New(d Deps) *Service {
	return &Service{
		db:         d.DB,
		log:        d.Log,
		sessions:   d.Sessions,
		basePath:   d.BasePath,
		dbPath:     d.DBPath,
		backupPath: d.BackupPath,
		umami:      d.Umami,
		oidcReady:  d.OIDCReady,
	}
}

// PublicConfig handles GET /api/config.
func (s *Service) PublicConfig(c *gin.Context) {
	var uURL, uSite string
	var uEnabled bool
	if s.umami != nil {
		uURL, uSite, uEnabled = s.umami()
	}
	oidcEnabled := false
	if s.oidcReady != nil {
		oidcEnabled = s.oidcReady()
	}
	c.JSON(http.StatusOK, gin.H{
		"umami_url":     uURL,
		"umami_site":    uSite,
		"umami_enabled": uEnabled,
		"oidc_enabled":  oidcEnabled,
	})
}

// CheckSetup handles GET /api/check-setup.
func (s *Service) CheckSetup(c *gin.Context) {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	c.JSON(http.StatusOK, gin.H{"setup": count > 0})
}

// Health handles GET /api/health.
func (s *Service) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": models.CurrentVersion,
	})
}

func (s *Service) Manifest(c *gin.Context) {
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

func (s *Service) ServiceWorker(c *gin.Context) {
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

// BackupInfo describes a backup file.
type BackupInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Date string `json:"date"`
}

func (s *Service) BackupDatabase() {
	name := fmt.Sprintf("traces-backup-%s.db", time.Now().Format("2006-01-02-150405"))
	dst := filepath.Join(s.backupPath, name)
	src, err := os.Open(s.dbPath)
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

func (s *Service) HandleBackup(c *gin.Context) {
	s.BackupDatabase()
	s.PruneBackups()
	if s.log != nil {
		s.log.Log("info", "backup", "Backup created", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) HandleListBackups(c *gin.Context) {
	entries, err := os.ReadDir(s.backupPath)
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

func (s *Service) PruneBackups() {
	var cfg models.BackupConfig
	var autoPruneInt int
	err := s.db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&cfg.RetentionDays, &autoPruneInt)
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
	entries, err := os.ReadDir(s.backupPath)
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
				path := filepath.Join(s.backupPath, e.Name())
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

func (s *Service) GetBackupConfig(c *gin.Context) {
	var cfg models.BackupConfig
	var autoPruneInt int
	err := s.db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&cfg.RetentionDays, &autoPruneInt)
	if err != nil {
		c.JSON(http.StatusOK, models.BackupConfig{RetentionDays: 7, AutoPrune: true})
		return
	}
	cfg.AutoPrune = autoPruneInt == 1
	c.JSON(http.StatusOK, cfg)
}

func (s *Service) SaveBackupConfig(c *gin.Context) {
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
	_, err := s.db.Exec(`UPDATE backup_settings SET retention_days=?, auto_prune=? WHERE id=1`, cfg.RetentionDays, autoPruneInt)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	if s.log != nil {
		s.log.Log("info", "backup", "Backup settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Page handlers

func (s *Service) AdminPage(c *gin.Context) {
	cookie, err := c.Cookie("session")
	if err == nil {
		if sess, ok := s.sessions.Get(cookie); ok && time.Now().Unix() <= sess.ExpiresAt {
			c.File(filepath.Join(s.basePath, "static/admin.html"))
			return
		}
	}
	c.Redirect(http.StatusFound, "/login.html")
}

func (s *Service) LoginPage(c *gin.Context) {
	cookie, err := c.Cookie("session")
	if err == nil {
		if sess, ok := s.sessions.Get(cookie); ok && time.Now().Unix() <= sess.ExpiresAt {
			c.Redirect(http.StatusFound, "/admin.html")
			return
		}
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if count == 0 {
		c.Redirect(http.StatusFound, "/setup.html")
		return
	}
	c.File(filepath.Join(s.basePath, "static/login.html"))
}

func (s *Service) SetupPage(c *gin.Context) {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if count > 0 {
		c.Redirect(http.StatusFound, "/login.html")
		return
	}
	c.File(filepath.Join(s.basePath, "static/setup.html"))
}

func (s *Service) MapPage(c *gin.Context) {
	c.File(filepath.Join(s.basePath, "static/map.html"))
}

func (s *Service) IndexPage(c *gin.Context) {
	c.File(filepath.Join(s.basePath, "static/index.html"))
}

func (s *Service) APIDocs(c *gin.Context) {
	c.File(filepath.Join(s.basePath, "docs/swagger.json"))
}
