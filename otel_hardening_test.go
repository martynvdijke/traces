package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

func saveRestoreOtelGlobals(t *testing.T) {
	t.Helper()
	origEndpoint := otelEndpoint
	origTraces := otelTracesEnabled
	origMetrics := otelMetricsEnabled
	origLogs := otelLogsEnabled
	origProtocol := otelExporterProtocol
	origTraceKind := otelTraceExporterKind
	origMetricKind := otelMetricExporterKind
	origLogKind := otelLogExporterKind
	origTP := otel.GetTracerProvider()
	origMP := otel.GetMeterProvider()
	origLP := global.GetLoggerProvider()
	origTraceFn := newOTLPTraceExporterFn
	origMetricFn := newOTLPMetricExporterFn
	origLogFn := newOTLPLogExporterFn
	origReg := prometheus.DefaultRegisterer
	origGatherer := prometheus.DefaultGatherer
	// Use isolated registry for this test to avoid polluting DefaultRegisterer
	isolatedReg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = isolatedReg
	prometheus.DefaultGatherer = isolatedReg
	_ = isolatedReg.Register(httpRequestsTotal)
	_ = isolatedReg.Register(httpRequestDuration)
	_ = isolatedReg.Register(eventOperationsTotal)
	_ = isolatedReg.Register(dbQueryDuration)
	_ = isolatedReg.Register(logEntriesTotal)
	t.Cleanup(func() {
		otelEndpoint = origEndpoint
		otelTracesEnabled = origTraces
		otelMetricsEnabled = origMetrics
		otelLogsEnabled = origLogs
		otelExporterProtocol = origProtocol
		otelTraceExporterKind = origTraceKind
		otelMetricExporterKind = origMetricKind
		otelLogExporterKind = origLogKind
		otel.SetTracerProvider(origTP)
		otel.SetMeterProvider(origMP)
		global.SetLoggerProvider(origLP)
		newOTLPTraceExporterFn = origTraceFn
		newOTLPMetricExporterFn = origMetricFn
		newOTLPLogExporterFn = origLogFn
		prometheus.DefaultRegisterer = origReg
		prometheus.DefaultGatherer = origGatherer
	})
}

// 4.1 Empty endpoint => stdout for all signals
func TestOTelEmptyEndpointStdout(t *testing.T) {
	saveRestoreOtelGlobals(t)
	otelEndpoint = ""
	otelTracesEnabled = true
	otelMetricsEnabled = true
	otelLogsEnabled = true
	otelExporterProtocol = "grpc"
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-empty")

	tp, err := initTelemetry()
	if err != nil {
		t.Fatalf("initTelemetry with empty endpoint should not fail: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	defer tp.Shutdown(context.Background())

	if otelTraceExporterKind != "stdout" {
		t.Errorf("trace exporter kind = %q, want stdout", otelTraceExporterKind)
	}
	if otelMetricExporterKind != "stdout" {
		t.Errorf("metric exporter kind = %q, want stdout", otelMetricExporterKind)
	}
	if otelLogExporterKind != "stdout" {
		t.Errorf("log exporter kind = %q, want stdout", otelLogExporterKind)
	}
}

// 4.2 http/protobuf constructors
func TestOTelHTTPProtobufConstructors(t *testing.T) {
	saveRestoreOtelGlobals(t)
	otelEndpoint = "http://127.0.0.1:4318"
	otelTracesEnabled = true
	otelMetricsEnabled = true
	otelLogsEnabled = true
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-http")

	tp, err := initTelemetry()
	if err != nil {
		t.Fatalf("initTelemetry http/protobuf: %v", err)
	}
	defer tp.Shutdown(context.Background())

	if otelTraceExporterKind != "otlp-http" {
		t.Errorf("trace kind = %q, want otlp-http", otelTraceExporterKind)
	}
	if otelMetricExporterKind != "otlp-http" {
		t.Errorf("metric kind = %q, want otlp-http", otelMetricExporterKind)
	}
	if otelLogExporterKind != "otlp-http" {
		t.Errorf("log kind = %q, want otlp-http", otelLogExporterKind)
	}

	// also verify grpc branch still works
	saveRestoreOtelGlobals(t)
	otelEndpoint = "http://127.0.0.1:4317"
	otelTracesEnabled = true
	otelMetricsEnabled = true
	otelLogsEnabled = true
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-grpc")
	tp2, err := initTelemetry()
	if err != nil {
		t.Fatalf("initTelemetry grpc: %v", err)
	}
	defer tp2.Shutdown(context.Background())
	if otelTraceExporterKind != "otlp-grpc" {
		t.Errorf("trace kind grpc = %q, want otlp-grpc", otelTraceExporterKind)
	}
	if otelMetricExporterKind != "otlp-grpc" {
		t.Errorf("metric kind grpc = %q, want otlp-grpc", otelMetricExporterKind)
	}
	if otelLogExporterKind != "otlp-grpc" {
		t.Errorf("log kind grpc = %q, want otlp-grpc", otelLogExporterKind)
	}
}

// 4.3 per-signal enable/disable matrix
func TestOTelPerSignalMatrix(t *testing.T) {
	cases := []struct {
		name    string
		traces  bool
		metrics bool
		logs    bool
	}{
		{"only_traces_disabled", false, true, true},
		{"only_metrics_disabled", true, false, true},
		{"only_logs_disabled", true, true, false},
		{"all_disabled", false, false, false},
		{"all_enabled", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			saveRestoreOtelGlobals(t)
			otelEndpoint = "http://127.0.0.1:4317"
			otelTracesEnabled = tc.traces
			otelMetricsEnabled = tc.metrics
			otelLogsEnabled = tc.logs
			otelExporterProtocol = "grpc"
			t.Setenv("OTEL_SERVICE_NAME", "traces-test-matrix")

			tp, err := initTelemetry()
			if err != nil {
				t.Fatalf("initTelemetry matrix %s: %v", tc.name, err)
			}
			defer tp.Shutdown(context.Background())

			// disabled => kind == disabled, enabled => otlp-*
			check := func(kind string, enabled bool, signal string) {
				if !enabled && kind != "disabled" {
					t.Errorf("%s kind = %q, want disabled", signal, kind)
				}
				if enabled && !strings.HasPrefix(kind, "otlp-") {
					t.Errorf("%s kind = %q, want otlp-*", signal, kind)
				}
			}
			check(otelTraceExporterKind, tc.traces, "trace")
			check(otelMetricExporterKind, tc.metrics, "metric")
			check(otelLogExporterKind, tc.logs, "log")
		})
	}
}

func TestOTelPersistedSubsetHonored(t *testing.T) {
	saveRestoreOtelGlobals(t)
	// Simulate DB-loaded subset: only metrics enabled
	otelEndpoint = "http://127.0.0.1:4317"
	otelTracesEnabled = false
	otelMetricsEnabled = true
	otelLogsEnabled = false
	otelExporterProtocol = "grpc"
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-persisted")
	tp, err := initTelemetry()
	if err != nil {
		t.Fatalf("initTelemetry persisted subset: %v", err)
	}
	defer tp.Shutdown(context.Background())
	if otelTraceExporterKind != "disabled" {
		t.Errorf("trace kind = %q, want disabled", otelTraceExporterKind)
	}
	if otelMetricExporterKind != "otlp-grpc" {
		t.Errorf("metric kind = %q, want otlp-grpc", otelMetricExporterKind)
	}
	if otelLogExporterKind != "disabled" {
		t.Errorf("log kind = %q, want disabled", otelLogExporterKind)
	}
}

// 4.4 metrics and logs OTLP constructor failure fallback to stdout
func TestOTelMetricLogFallbackToStdout(t *testing.T) {
	saveRestoreOtelGlobals(t)
	otelEndpoint = "http://127.0.0.1:4317"
	otelTracesEnabled = true
	otelMetricsEnabled = true
	otelLogsEnabled = true
	otelExporterProtocol = "grpc"
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-fallback")

	// Inject failing constructors for metrics and logs
	newOTLPMetricExporterFn = func(endpoint string) (sdkmetric.Exporter, error) {
		return nil, fmt.Errorf("injected metric failure")
	}
	newOTLPLogExporterFn = func(endpoint string) (sdklog.Exporter, error) {
		return nil, fmt.Errorf("injected log failure")
	}
	// traces constructor succeeds normally; we only test metrics/logs fallback here
	tp, err := initTelemetry()
	if err != nil {
		t.Fatalf("initTelemetry with injected failures should not fail: %v", err)
	}
	defer tp.Shutdown(context.Background())

	if otelMetricExporterKind != "stdout" {
		t.Errorf("metric kind after injected failure = %q, want stdout", otelMetricExporterKind)
	}
	if otelLogExporterKind != "stdout" {
		t.Errorf("log kind after injected failure = %q, want stdout", otelLogExporterKind)
	}
	// trace should still be otlp
	if otelTraceExporterKind != "otlp-grpc" {
		t.Errorf("trace kind = %q, want otlp-grpc", otelTraceExporterKind)
	}

	// Also verify trace fallback still works via injection
	t.Run("trace_fallback", func(t *testing.T) {
		saveRestoreOtelGlobals(t)
		otelEndpoint = "http://127.0.0.1:4317"
		otelTracesEnabled = true
		otelMetricsEnabled = true
		otelLogsEnabled = true
		otelExporterProtocol = "grpc"
		t.Setenv("OTEL_SERVICE_NAME", "traces-test-trace-fallback")
		newOTLPTraceExporterFn = func(endpoint string) (sdktrace.SpanExporter, error) {
			return nil, fmt.Errorf("injected trace failure")
		}
		tp, err := initTelemetry()
		if err != nil {
			t.Fatalf("trace fallback should not fail: %v", err)
		}
		defer tp.Shutdown(context.Background())
		if otelTraceExporterKind != "stdout" {
			t.Errorf("trace kind after failure = %q, want stdout", otelTraceExporterKind)
		}
	})
}

// Direct unit tests for initMetricExporterWithRegisterer / initLogExporter fallback via resource
func TestOTelMetricExporterFallbackDirect(t *testing.T) {
	saveRestoreOtelGlobals(t)
	otelEndpoint = "http://127.0.0.1:4317"
	otelMetricsEnabled = true
	otelExporterProtocol = "grpc"
	newOTLPMetricExporterFn = func(endpoint string) (sdkmetric.Exporter, error) {
		return nil, fmt.Errorf("injected failure")
	}
	res, _ := resource.New(context.Background(), resource.WithTelemetrySDK())
	reg := prometheus.NewRegistry()
	if err := initMetricExporterWithRegisterer(res, reg); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if otelMetricExporterKind != "stdout" {
		t.Errorf("kind = %q, want stdout", otelMetricExporterKind)
	}
}

func TestOTelLogExporterFallbackDirect(t *testing.T) {
	saveRestoreOtelGlobals(t)
	otelEndpoint = "http://127.0.0.1:4317"
	otelLogsEnabled = true
	otelExporterProtocol = "grpc"
	newOTLPLogExporterFn = func(endpoint string) (sdklog.Exporter, error) {
		return nil, fmt.Errorf("injected failure")
	}
	res, _ := resource.New(context.Background(), resource.WithTelemetrySDK())
	if err := initLogExporter(res); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if otelLogExporterKind != "stdout" {
		t.Errorf("kind = %q, want stdout", otelLogExporterKind)
	}
}

// 4.5 GET/POST /api/otel/config
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
