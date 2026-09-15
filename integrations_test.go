package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/models"
	"traces/internal/web"
)

func TestPruneBackups(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origBackupPath := backupPath
	t.Cleanup(func() { backupPath = origBackupPath })

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")

	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	backupPath = tmpDir
	webSvc = web.New(web.Deps{DB: db, Log: logService, Sessions: authSessions, BasePath: basePath, DBPath: dbPath, BackupPath: tmpDir, Umami: integrationsSvc.UmamiSettings, OIDCReady: authSvc.OIDCReady})

	oldTime := time.Now().AddDate(0, 0, -15)
	for i := range 3 {
		name := fmt.Sprintf("traces-backup-%s-%d.db", oldTime.Format("2006-01-02-150405"), i)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("test"), 0644)
		os.Chtimes(path, oldTime, oldTime)
	}

	recentTime := time.Now().AddDate(0, 0, -1)
	for i := range 2 {
		name := fmt.Sprintf("traces-backup-%s-%d.db", recentTime.Format("2006-01-02-150405"), i)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("test"), 0644)
		os.Chtimes(path, recentTime, recentTime)
	}

	webSvc.PruneBackups()

	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 2 {
		t.Errorf("expected 2 backups after prune, got %d", len(entries))
	}
}

func TestPruneBackupsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origBackupPath := backupPath
	t.Cleanup(func() { backupPath = origBackupPath })

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 0)")

	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	backupPath = tmpDir
	webSvc = web.New(web.Deps{DB: db, Log: logService, Sessions: authSessions, BasePath: basePath, DBPath: dbPath, BackupPath: tmpDir, Umami: integrationsSvc.UmamiSettings, OIDCReady: authSvc.OIDCReady})

	oldTime := time.Now().AddDate(0, 0, -15)
	for i := range 3 {
		name := fmt.Sprintf("traces-backup-%s-%d.db", oldTime.Format("2006-01-02-150405"), i)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("test"), 0644)
		os.Chtimes(path, oldTime, oldTime)
	}

	webSvc.PruneBackups()

	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 3 {
		t.Errorf("expected 3 backups when prune disabled, got %d", len(entries))
	}
}

func TestBackupConfigRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")

	t.Run("default_config", func(t *testing.T) {
		var days int
		var auto int
		db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&days, &auto)
		if days != 7 {
			t.Errorf("retention_days = %d, want 7", days)
		}
		if auto != 1 {
			t.Errorf("auto_prune = %d, want 1", auto)
		}
	})

	t.Run("update_config", func(t *testing.T) {
		db.Exec("UPDATE backup_settings SET retention_days=?, auto_prune=? WHERE id=1", 14, 0)
		var days int
		var auto int
		db.QueryRow("SELECT retention_days, auto_prune FROM backup_settings WHERE id = 1").Scan(&days, &auto)
		if days != 14 {
			t.Errorf("retention_days = %d, want 14", days)
		}
		if auto != 0 {
			t.Errorf("auto_prune = %d, want 0", auto)
		}
	})
}

func TestBackupConfigAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS backup_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER DEFAULT 7,
		auto_prune INTEGER DEFAULT 1
	)`)
	db.Exec("INSERT OR IGNORE INTO backup_settings (id, retention_days, auto_prune) VALUES (1, 7, 1)")

	r := setupTestRouter()
	r.GET("/api/backup/config", webSvc.GetBackupConfig)
	r.POST("/api/backup/config", webSvc.SaveBackupConfig)

	t.Run("get_config", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/backup/config", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var cfg models.BackupConfig
		json.Unmarshal(w.Body.Bytes(), &cfg)
		if cfg.RetentionDays != 7 {
			t.Errorf("retention_days = %d, want 7", cfg.RetentionDays)
		}
		if !cfg.AutoPrune {
			t.Error("auto_prune should be true")
		}
	})

	t.Run("save_config", func(t *testing.T) {
		body, _ := json.Marshal(models.BackupConfig{RetentionDays: 30, AutoPrune: false})
		req := httptest.NewRequest("POST", "/api/backup/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		req = httptest.NewRequest("GET", "/api/backup/config", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var cfg models.BackupConfig
		json.Unmarshal(w.Body.Bytes(), &cfg)
		if cfg.RetentionDays != 30 {
			t.Errorf("retention_days = %d, want 30", cfg.RetentionDays)
		}
		if cfg.AutoPrune {
			t.Error("auto_prune should be false")
		}
	})
}
