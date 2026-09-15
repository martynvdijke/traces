package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/models"
)

func TestOllamaConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS ollama_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT 'http://localhost:11434',
		model TEXT DEFAULT 'llama3.2',
		enabled INTEGER DEFAULT 0
	)`)
	db.Exec("INSERT OR IGNORE INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)")

	t.Run("default_config", func(t *testing.T) {
		var url, model string
		var enabled int
		err := db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&url, &model, &enabled)
		if err != nil {
			t.Fatal(err)
		}
		if url != "http://localhost:11434" {
			t.Errorf("url = %q", url)
		}
		if model != "llama3.2" {
			t.Errorf("model = %q", model)
		}
		if enabled != 0 {
			t.Errorf("enabled = %d", enabled)
		}
	})

	t.Run("update_config", func(t *testing.T) {
		db.Exec("UPDATE ollama_settings SET url=?, model=?, enabled=? WHERE id=1", "http://ollama:11434", "mistral", 1)
		var url, model string
		var enabled int
		db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&url, &model, &enabled)
		if url != "http://ollama:11434" {
			t.Errorf("url = %q", url)
		}
		if enabled != 1 {
			t.Errorf("enabled = %d", enabled)
		}
	})
}

func TestImmichConfigRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origImmichURL := immichURL
	origImmichAPIKey := immichAPIKey
	origImmichEnabled := immichEnabled
	t.Cleanup(func() {
		immichURL = origImmichURL
		immichAPIKey = origImmichAPIKey
		immichEnabled = origImmichEnabled
	})

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS immich_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT '',
		api_key TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0
	)`)
	db.Exec(`INSERT OR IGNORE INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)`)

	var url, apiKey string
	var enabledInt int
	err := db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&url, &apiKey, &enabledInt)
	if err != nil {
		t.Fatal(err)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
	if apiKey != "" {
		t.Errorf("api_key = %q, want empty", apiKey)
	}
	if enabledInt != 0 {
		t.Errorf("enabled = %d, want 0", enabledInt)
	}

	immichURL = "https://immich.example.com"
	immichAPIKey = "test-api-key-123"
	immichEnabled = true

	db.Exec("UPDATE immich_settings SET url=?, api_key=?, enabled=? WHERE id=1", immichURL, immichAPIKey, 1)
	err = db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&url, &apiKey, &enabledInt)
	if err != nil {
		t.Fatal(err)
	}
	if url != immichURL {
		t.Errorf("url = %q, want %q", url, immichURL)
	}
	if apiKey != immichAPIKey {
		t.Errorf("api_key = %q, want %q", apiKey, immichAPIKey)
	}
	if enabledInt != 1 {
		t.Errorf("enabled = %d, want 1", enabledInt)
	}

	immichURL = "https://immich.vandijke.xyz"
	immichAPIKey = "another-key"
	immichEnabled = false

	db.Exec("UPDATE immich_settings SET url=?, api_key=?, enabled=? WHERE id=1", immichURL, immichAPIKey, 0)
	err = db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&url, &apiKey, &enabledInt)
	if err != nil {
		t.Fatal(err)
	}
	if url != immichURL {
		t.Errorf("url = %q, want %q after update", url, immichURL)
	}
	if apiKey != immichAPIKey {
		t.Errorf("api_key = %q, want %q after update", apiKey, immichAPIKey)
	}
	if enabledInt != 0 {
		t.Errorf("enabled = %d, want 0 after update", enabledInt)
	}
}

func TestImmichMemoryAssetSerialization(t *testing.T) {
	tests := []struct {
		name   string
		asset  models.ImmichMemoryAsset
		expect string
	}{
		{
			name: "basic_asset",
			asset: models.ImmichMemoryAsset{
				ID:               "abc-123",
				OriginalFileName: "IMG_2023.jpg",
				Type:             "IMAGE",
				ThumbnailURL:     "https://immich.example.com/api/assets/abc-123/thumbnail",
				AssetCount:       3,
				MemoryDate:       "1 year ago",
				Latitude:         52.3676,
				Longitude:        4.9041,
				Description:      "photo from immich",
			},
			expect: `"id":"abc-123"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.asset)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			if !strings.Contains(string(data), tt.expect) {
				t.Errorf("json output %q should contain %q", string(data), tt.expect)
			}
		})
	}
}

func TestImmichTimelineResponseParsing(t *testing.T) {
	responseJSON := `[
		{
			"title": "1 year ago",
			"assets": [
				{
					"id": "asset-001",
					"originalFileName": "photo1.jpg",
					"type": "IMAGE",
					"exifInfo": {
						"dateTimeOriginal": "2023-05-07T10:30:00.000Z",
						"latitude": 52.3676,
						"longitude": 4.9041,
						"city": "Amsterdam",
						"country": "Netherlands"
					}
				}
			]
		}
	]`

	var timeline []immichTimelineResponse
	if err := json.Unmarshal([]byte(responseJSON), &timeline); err != nil {
		t.Fatalf("failed to parse timeline response: %v", err)
	}

	if len(timeline) != 1 {
		t.Fatalf("expected 1 timeline group, got %d", len(timeline))
	}

	if timeline[0].Title != "1 year ago" {
		t.Errorf("title = %q, want %q", timeline[0].Title, "1 year ago")
	}

	if len(timeline[0].Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(timeline[0].Assets))
	}

	asset := timeline[0].Assets[0]
	if asset.ID != "asset-001" {
		t.Errorf("asset.ID = %q, want %q", asset.ID, "asset-001")
	}
	if asset.OriginalFileName != "photo1.jpg" {
		t.Errorf("asset.OriginalFileName = %q, want %q", asset.OriginalFileName, "photo1.jpg")
	}
	if asset.Type != "IMAGE" {
		t.Errorf("asset.Type = %q, want %q", asset.Type, "IMAGE")
	}
	if asset.ExifInfo == nil {
		t.Fatal("asset.ExifInfo is nil")
	}
	if *asset.ExifInfo.City != "Amsterdam" {
		t.Errorf("exif.City = %q, want %q", *asset.ExifInfo.City, "Amsterdam")
	}
}

func TestImmichConfigHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origImmichURL := immichURL
	origImmichAPIKey := immichAPIKey
	origImmichEnabled := immichEnabled
	t.Cleanup(func() {
		immichURL = origImmichURL
		immichAPIKey = origImmichAPIKey
		immichEnabled = origImmichEnabled
	})

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS immich_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		url TEXT DEFAULT '',
		api_key TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0
	)`)
	db.Exec(`INSERT OR IGNORE INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)`)
	immichURL = "https://immich.vandijke.xyz"
	immichAPIKey = "key-123"
	immichEnabled = true

	r := gin.New()
	r.GET("/api/immich/config", getImmichConfig)
	r.POST("/api/immich/config", saveImmichConfig)

	t.Run("get_config_empty", func(t *testing.T) {
		immichURL = ""
		immichAPIKey = ""
		immichEnabled = false

		req := httptest.NewRequest("GET", "/api/immich/config", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		var cfg models.ImmichConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Enabled {
			t.Errorf("Enabled should be false for empty config")
		}
	})

	t.Run("save_and_read_config", func(t *testing.T) {
		cfg := models.ImmichConfig{
			URL:     "https://immich.vandijke.xyz",
			APIKey:  "test-api-key-456",
			Enabled: true,
		}
		body, _ := json.Marshal(cfg)

		req := httptest.NewRequest("POST", "/api/immich/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("save status = %d, want 200: %s", w.Code, w.Body.String())
		}

		var result map[string]string
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["status"] != "ok" {
			t.Errorf("status = %q, want ok", result["status"])
		}

		if immichURL != cfg.URL {
			t.Errorf("immichURL global = %q, want %q", immichURL, cfg.URL)
		}
		if immichAPIKey != cfg.APIKey {
			t.Errorf("immichAPIKey global = %q, want %q", immichAPIKey, cfg.APIKey)
		}
		if !immichEnabled {
			t.Error("immichEnabled should be true")
		}

		req2 := httptest.NewRequest("GET", "/api/immich/config", nil)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("read status = %d, want 200", w2.Code)
		}

		var readCfg models.ImmichConfig
		json.Unmarshal(w2.Body.Bytes(), &readCfg)
		if readCfg.URL != cfg.URL {
			t.Errorf("URL = %q, want %q", readCfg.URL, cfg.URL)
		}
		if readCfg.APIKey != cfg.APIKey {
			t.Errorf("APIKey = %q, want %q", readCfg.APIKey, cfg.APIKey)
		}
		if !readCfg.Enabled {
			t.Error("Enabled should be true")
		}
	})

	t.Run("save_config_disabled", func(t *testing.T) {
		cfg := models.ImmichConfig{
			URL:     "https://immich.vandijke.xyz",
			APIKey:  "key-disabled",
			Enabled: false,
		}
		body, _ := json.Marshal(cfg)

		req := httptest.NewRequest("POST", "/api/immich/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("save status = %d, want 200", w.Code)
		}
		if immichEnabled {
			t.Error("immichEnabled should be false after saving disabled config")
		}
	})
}

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

	pruneBackups()

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

	oldTime := time.Now().AddDate(0, 0, -15)
	for i := range 3 {
		name := fmt.Sprintf("traces-backup-%s-%d.db", oldTime.Format("2006-01-02-150405"), i)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("test"), 0644)
		os.Chtimes(path, oldTime, oldTime)
	}

	pruneBackups()

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
	r.GET("/api/backup/config", getBackupConfig)
	r.POST("/api/backup/config", saveBackupConfig)

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
