package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// helpers

func setupOtelDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS otel_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		endpoint TEXT DEFAULT '',
		traces_enabled INTEGER DEFAULT 0,
		metrics_enabled INTEGER DEFAULT 0,
		logs_enabled INTEGER DEFAULT 0
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS app_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		severity TEXT NOT NULL DEFAULT 'info',
		source TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT '',
		metadata TEXT DEFAULT ''
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS log_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		min_severity TEXT NOT NULL DEFAULT 'warn'
	)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO otel_settings (id, endpoint, traces_enabled, metrics_enabled, logs_enabled) VALUES (1, '', 0, 0, 0)`)
	db.Exec(`INSERT INTO log_settings (id, min_severity) VALUES (1, 'warn')`)
	// minimal tables for initTemplates/initDB not needed; handlers only need otel_settings
	return db
}

func TestGetOtelConfig(t *testing.T) {
	origDB := db
	origLogService := logService
	origEndpoint := otelEndpoint
	origTraces := otelTracesEnabled
	origMetrics := otelMetricsEnabled
	origLogs := otelLogsEnabled
	t.Cleanup(func() {
		db = origDB
		logService = origLogService
		otelEndpoint = origEndpoint
		otelTracesEnabled = origTraces
		otelMetricsEnabled = origMetrics
		otelLogsEnabled = origLogs
	})

	db = setupOtelDB(t)
	t.Cleanup(func() { db.Close() })
	logService = &LogService{db: db}
	logService.Init()

	// seed a config
	db.Exec(`UPDATE otel_settings SET endpoint=?, traces_enabled=?, metrics_enabled=?, logs_enabled=? WHERE id=1`, "http://otel:4317", 1, 0, 1)

	router := gin.New()
	router.GET("/api/otel/config", getOtelConfig)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/otel/config", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", w.Code)
	}
	var cfg map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["endpoint"] != "http://otel:4317" {
		t.Errorf("endpoint = %v, want http://otel:4317", cfg["endpoint"])
	}
	if cfg["traces_enabled"] != true {
		t.Errorf("traces_enabled = %v, want true", cfg["traces_enabled"])
	}
	if cfg["metrics_enabled"] != false {
		t.Errorf("metrics_enabled = %v, want false", cfg["metrics_enabled"])
	}
	if cfg["logs_enabled"] != true {
		t.Errorf("logs_enabled = %v, want true", cfg["logs_enabled"])
	}
}

func TestSaveOtelConfig(t *testing.T) {
	origDB := db
	origLogService := logService
	origEndpoint := otelEndpoint
	origTraces := otelTracesEnabled
	origMetrics := otelMetricsEnabled
	origLogs := otelLogsEnabled
	t.Cleanup(func() {
		db = origDB
		logService = origLogService
		otelEndpoint = origEndpoint
		otelTracesEnabled = origTraces
		otelMetricsEnabled = origMetrics
		otelLogsEnabled = origLogs
	})

	db = setupOtelDB(t)
	t.Cleanup(func() { db.Close() })
	logService = &LogService{db: db}
	logService.Init()

	router := gin.New()
	router.GET("/api/otel/config", getOtelConfig)
	router.POST("/api/otel/config", saveOtelConfig)

	payload := `{"endpoint":"http://new:4318","traces_enabled":true,"metrics_enabled":true,"logs_enabled":false}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/otel/config", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	// verify DB state
	var endpoint string
	var tE, mE, lE int
	if err := db.QueryRow("SELECT endpoint, traces_enabled, metrics_enabled, logs_enabled FROM otel_settings WHERE id=1").Scan(&endpoint, &tE, &mE, &lE); err != nil {
		t.Fatal(err)
	}
	if endpoint != "http://new:4318" {
		t.Errorf("db endpoint = %q, want http://new:4318", endpoint)
	}
	if tE != 1 || mE != 1 || lE != 0 {
		t.Errorf("db flags = %d/%d/%d, want 1/1/0", tE, mE, lE)
	}
	// verify globals updated
	if otelEndpoint != "http://new:4318" || !otelTracesEnabled || !otelMetricsEnabled || otelLogsEnabled {
		t.Errorf("globals not updated: endpoint=%q traces=%v metrics=%v logs=%v", otelEndpoint, otelTracesEnabled, otelMetricsEnabled, otelLogsEnabled)
	}

	// GET should return updated values
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/otel/config", nil)
	router.ServeHTTP(w2, req2)
	var cfg map[string]any
	json.Unmarshal(w2.Body.Bytes(), &cfg)
	if cfg["endpoint"] != "http://new:4318" {
		t.Errorf("GET after POST endpoint = %v, want http://new:4318", cfg["endpoint"])
	}
	if cfg["traces_enabled"] != true || cfg["metrics_enabled"] != true || cfg["logs_enabled"] != false {
		t.Errorf("GET after POST flags = %v", cfg)
	}
}

func TestSaveOtelConfigInvalidJSON(t *testing.T) {
	origDB := db
	t.Cleanup(func() { db = origDB })
	db = setupOtelDB(t)
	t.Cleanup(func() { db.Close() })
	router := gin.New()
	router.POST("/api/otel/config", saveOtelConfig)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/otel/config", strings.NewReader(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
