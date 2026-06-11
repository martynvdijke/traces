package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupTestDB creates an in-memory SQLite database with required log tables.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS app_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		severity TEXT NOT NULL DEFAULT 'info',
		source TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT '',
		metadata TEXT DEFAULT ''
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS log_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		min_severity TEXT NOT NULL DEFAULT 'warn'
	)`)
	return db
}

func TestPrometheusMetricsRegistration(t *testing.T) {
	// Verify Prometheus metrics are registered and usable
	metrics := map[string]func(){
		"event_operations_total":  func() { RecordEventOperation("test") },
		"db_query_duration_seconds": func() { RecordDBQuery("test", time.Millisecond) },
		"log_entries_total":       func() { RecordLogEntry() },
	}

	for name, fn := range metrics {
		t.Run(name, func(t *testing.T) {
			// Should not panic
			fn()
		})
	}
}

func TestRecordEventOperation(t *testing.T) {
	eventOperationsTotal.Reset()

	ops := []string{"created", "updated", "deleted", "restored", "cloned", "imported"}
	for _, op := range ops {
		RecordEventOperation(op)
	}

	for _, op := range ops {
		val := testutil.ToFloat64(eventOperationsTotal.WithLabelValues(op))
		if val != 1 {
			t.Errorf("expected counter for %q to be 1, got %f", op, val)
		}
	}
}

func TestRecordDBQuery(t *testing.T) {
	dbQueryDuration.Reset()

	RecordDBQuery("select", 50*time.Millisecond)
	RecordDBQuery("insert", 100*time.Millisecond)

	count := testutil.CollectAndCount(dbQueryDuration)
	if count == 0 {
		t.Error("expected dbQueryDuration to have collected observations")
	}
}

func TestRecordLogEntry(t *testing.T) {
	before := testutil.ToFloat64(logEntriesTotal)
	RecordLogEntry()
	after := testutil.ToFloat64(logEntriesTotal)
	if after-before != 1 {
		t.Errorf("expected log counter to increase by 1, got %f", after-before)
	}
}

func TestMetricsMiddleware(t *testing.T) {
	httpRequestsTotal.Reset()
	httpRequestDuration.Reset()

	router := gin.New()
	router.Use(metricsMiddleware())
	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	counter := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/test", "200"))
	if counter != 1 {
		t.Errorf("expected http_requests_total counter to be 1, got %f", counter)
	}
}

func TestMetricsMiddlewareErrorStatus(t *testing.T) {
	httpRequestsTotal.Reset()

	router := gin.New()
	router.Use(metricsMiddleware())
	router.GET("/api/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "test error"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/error", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	counter := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/error", "500"))
	if counter != 1 {
		t.Errorf("expected http_requests_total counter for 500 to be 1, got %f", counter)
	}
}

func TestPrometheusEndpoint(t *testing.T) {
	router := gin.New()
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	expectedMetrics := []string{
		"http_requests_total",
		"http_request_duration_seconds",
		"event_operations_total",
		"db_query_duration_seconds",
		"log_entries_total",
	}

	for _, m := range expectedMetrics {
		if !strings.Contains(body, m) {
			t.Errorf("expected metric %q in /metrics output", m)
		}
	}
}

// --- Log Service Tests ---

func TestLogServiceLogging(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	if err := ls.Init(); err != nil {
		t.Fatal(err)
	}

	t.Run("log_entry_stored", func(t *testing.T) {
		ls.SetMinSeverity("info")
		ls.Log("info", "test", "test message", nil)
		entries, err := ls.Query("", "", "", 10, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 log entry, got %d", len(entries))
		}
		if entries[0].Message != "test message" {
			t.Errorf("message = %q, want %q", entries[0].Message, "test message")
		}
		if entries[0].Source != "test" {
			t.Errorf("source = %q, want %q", entries[0].Source, "test")
		}
		if entries[0].Severity != "info" {
			t.Errorf("severity = %q, want %q", entries[0].Severity, "info")
		}
	})

	t.Run("severity_filtering", func(t *testing.T) {
		// Clear and set minimum severity to warn
		ls.Clear()
		ls.SetMinSeverity("warn")
		ls.Log("debug", "test", "debug message", nil)
		ls.Log("warn", "test", "warning message", nil)

		entries, err := ls.Query("", "", "", 10, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 entry (debug should be filtered), got %d", len(entries))
		}
		if entries[0].Severity != "warn" {
			t.Errorf("latest entry severity = %q, want 'warn'", entries[0].Severity)
		}
		ls.SetMinSeverity("info")
	})

	t.Run("source_filter", func(t *testing.T) {
		entries, err := ls.Query("", "nonexistent", "", 10, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries for nonexistent source, got %d", len(entries))
		}

		entries, err = ls.Query("", "test", "", 10, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Error("expected entries for source 'test'")
		}
	})

	t.Run("text_search", func(t *testing.T) {
		ls.SetMinSeverity("info")
		ls.Log("info", "searchtest", "unique searchable message 42", nil)
		entries, err := ls.Query("", "", "searchable", 10, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Fatal("expected at least 1 entry matching 'searchable'")
		}
		if !strings.Contains(entries[0].Message, "searchable") {
			t.Errorf("entry message does not contain 'searchable': %q", entries[0].Message)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		ls.SetMinSeverity("info")
		for i := 0; i < 5; i++ {
			ls.Log("info", "pagination", fmt.Sprintf("message %d", i), nil)
		}

		entries, err := ls.Query("", "pagination", "", 3, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 3 {
			t.Errorf("expected 3 entries with limit 3, got %d", len(entries))
		}

		entries2, err := ls.Query("", "pagination", "", 3, 3, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries2) == 0 {
			t.Error("expected entries with offset 3")
		}
	})

	t.Run("log_count_and_clear", func(t *testing.T) {
		count, err := ls.Count()
		if err != nil {
			t.Fatal(err)
		}
		if count <= 0 {
			t.Errorf("expected count > 0, got %d", count)
		}

		if err := ls.Clear(); err != nil {
			t.Fatal(err)
		}
		count, _ = ls.Count()
		if count != 0 {
			t.Errorf("expected 0 entries after clear, got %d", count)
		}
	})
}

func TestLogServiceMetadata(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	ls.Init()
	ls.SetMinSeverity("info")

	metadata := map[string]interface{}{
		"user_id": float64(42),
		"action":  "login",
	}
	ls.Log("info", "auth", "user logged in", metadata)

	entries, err := ls.Query("", "auth", "", 10, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 auth entry")
	}

	if entries[0].Metadata == nil {
		t.Fatal("expected metadata to be present")
	}
	if entries[0].Metadata["user_id"] != float64(42) {
		t.Errorf("metadata.user_id = %v, want 42", entries[0].Metadata["user_id"])
	}
	if entries[0].Metadata["action"] != "login" {
		t.Errorf("metadata.action = %v, want login", entries[0].Metadata["action"])
	}
}

func TestLogServiceSeverityOrder(t *testing.T) {
	order := GetLogSeverityOrder()
	expected := []string{"debug", "info", "warn", "error"}

	if len(order) != len(expected) {
		t.Errorf("expected %d severity levels, got %d", len(expected), len(order))
	}

	for i, s := range expected {
		if order[s] != i {
			t.Errorf("severityOrder[%q] = %d, want %d", s, order[s], i)
		}
	}
}

func TestLogServiceDistinctSources(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	ls.Init()
	ls.SetMinSeverity("info")

	ls.Log("info", "source1", "msg1", nil)
	ls.Log("warn", "source2", "msg2", nil)
	ls.Log("error", "source1", "msg3", nil)

	sources, err := ls.GetDistinctSources()
	if err != nil {
		t.Fatal(err)
	}

	sourceSet := make(map[string]bool)
	for _, s := range sources {
		sourceSet[s] = true
	}

	if !sourceSet["source1"] {
		t.Error("expected 'source1' in distinct sources")
	}
	if !sourceSet["source2"] {
		t.Error("expected 'source2' in distinct sources")
	}
	if len(sourceSet) != 2 {
		t.Errorf("expected 2 distinct sources, got %d", len(sourceSet))
	}
}

func TestLogServiceMinSeverityPersistence(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	ls.Init()

	if sev := ls.GetMinSeverity(); sev != "warn" {
		t.Errorf("default min_severity = %q, want 'warn'", sev)
	}

	ls.SetMinSeverity("debug")
	if sev := ls.GetMinSeverity(); sev != "debug" {
		t.Errorf("min_severity after set = %q, want 'debug'", sev)
	}

	ls2 := &LogService{db: db}
	ls2.Init()
	if sev := ls2.GetMinSeverity(); sev != "debug" {
		t.Errorf("persisted min_severity = %q, want 'debug'", sev)
	}
}

func TestLogServicePruneLimit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	ls.Init()
	ls.SetMinSeverity("info")

	for i := 0; i < 100; i++ {
		ls.Log("info", "prunetest", fmt.Sprintf("message %d", i), nil)
	}

	count, err := ls.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count > 100 {
		t.Errorf("expected count <= 100 after pruning, got %d", count)
	}
	if count == 0 {
		t.Error("expected some entries to remain after pruning")
	}
}

// --- Log API Handler Tests ---

func TestHandleGetLogs(t *testing.T) {
	origDB := db
	origLogService := logService
	defer func() {
		db = origDB
		logService = origLogService
	}()

	db = setupTestDB(t)
	defer db.Close()

	logService = &LogService{db: db}
	logService.Init()
	logService.SetMinSeverity("info")
	logService.Log("info", "handler-test", "test log entry", nil)

	router := gin.New()
	router.GET("/api/logs", handleGetLogs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/logs?limit=10", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var entries []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 log entry")
	}
	if entries[0].Message != "test log entry" {
		t.Errorf("message = %q, want %q", entries[0].Message, "test log entry")
	}
}

func TestHandleGetLogCount(t *testing.T) {
	origDB := db
	origLogService := logService
	defer func() {
		db = origDB
		logService = origLogService
	}()

	db = setupTestDB(t)
	defer db.Close()

	logService = &LogService{db: db}
	logService.Init()
	logService.SetMinSeverity("info")
	logService.Log("info", "count-test", "test", nil)

	router := gin.New()
	router.GET("/api/logs/count", handleGetLogCount)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/logs/count", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["count"] <= 0 {
		t.Errorf("expected count > 0, got %d", resp["count"])
	}
}

func TestHandleClearLogs(t *testing.T) {
	origDB := db
	origLogService := logService
	defer func() {
		db = origDB
		logService = origLogService
	}()

	db = setupTestDB(t)
	defer db.Close()

	logService = &LogService{db: db}
	logService.Init()
	logService.SetMinSeverity("info")
	logService.Log("info", "clear-test", "to be cleared", nil)

	router := gin.New()
	router.DELETE("/api/logs", handleClearLogs)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/logs", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	count, _ := logService.Count()
	if count != 0 {
		t.Errorf("expected 0 entries after clear, got %d", count)
	}
}

func TestHandleGetLogSettings(t *testing.T) {
	origDB := db
	origLogService := logService
	defer func() {
		db = origDB
		logService = origLogService
	}()

	db = setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	ls.Init()
	ls.SetMinSeverity("debug")
	logService = ls

	router := gin.New()
	router.GET("/api/logs/settings", handleGetLogSettings)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/logs/settings", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["min_severity"] != "debug" {
		t.Errorf("min_severity = %q, want 'debug'", resp["min_severity"])
	}
}

func TestHandleUpdateLogSettings(t *testing.T) {
	origDB := db
	origLogService := logService
	defer func() {
		db = origDB
		logService = origLogService
	}()

	db = setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	ls.Init()
	logService = ls

	router := gin.New()
	router.POST("/api/logs/settings", handleUpdateLogSettings)

	body := `{"min_severity":"error"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/logs/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	if sev := ls.GetMinSeverity(); sev != "error" {
		t.Errorf("min_severity = %q, want 'error'", sev)
	}
}

func TestHandleUpdateLogSettingsInvalid(t *testing.T) {
	origDB := db
	origLogService := logService
	defer func() {
		db = origDB
		logService = origLogService
	}()

	db = setupTestDB(t)
	defer db.Close()

	ls := &LogService{db: db}
	ls.Init()
	logService = ls

	router := gin.New()
	router.POST("/api/logs/settings", handleUpdateLogSettings)

	body := `{"min_severity":"invalid"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/logs/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for invalid severity", w.Code, http.StatusBadRequest)
	}

	if sev := ls.GetMinSeverity(); sev == "invalid" {
		t.Error("min_severity should not have been updated to invalid")
	}
}

func TestHandleGetLogSources(t *testing.T) {
	origDB := db
	origLogService := logService
	defer func() {
		db = origDB
		logService = origLogService
	}()

	db = setupTestDB(t)
	defer db.Close()

	logService = &LogService{db: db}
	logService.Init()
	logService.SetMinSeverity("info")
	logService.Log("info", "source-a", "test", nil)
	logService.Log("warn", "source-b", "test", nil)

	router := gin.New()
	router.GET("/api/logs/sources", handleGetLogSources)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/logs/sources", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var sources []string
	if err := json.Unmarshal(w.Body.Bytes(), &sources); err != nil {
		t.Fatal(err)
	}

	sourceSet := make(map[string]bool)
	for _, s := range sources {
		sourceSet[s] = true
	}

	if !sourceSet["source-a"] {
		t.Error("expected 'source-a' in sources")
	}
	if !sourceSet["source-b"] {
		t.Error("expected 'source-b' in sources")
	}
}

func TestMetricsConcurrentSafety(t *testing.T) {
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			RecordEventOperation("concurrent-test")
			RecordDBQuery("concurrent-test", time.Millisecond)
			RecordLogEntry()
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
