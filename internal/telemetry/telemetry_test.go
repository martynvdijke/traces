package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func init() { gin.SetMode(gin.TestMode) }

func saveRestoreTelemetry(t *testing.T) {
	t.Helper()
	origTP := otel.GetTracerProvider()
	origMP := otel.GetMeterProvider()
	origLP := global.GetLoggerProvider()
	origReg := prometheus.DefaultRegisterer
	origGatherer := prometheus.DefaultGatherer
	origTraceFn := testTraceExporterFn
	origMetricFn := testMetricExporterFn
	origLogFn := testLogExporterFn
	isolatedReg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = isolatedReg
	prometheus.DefaultGatherer = isolatedReg
	_ = isolatedReg.Register(httpRequestsTotal)
	_ = isolatedReg.Register(httpRequestDuration)
	_ = isolatedReg.Register(eventOperationsTotal)
	_ = isolatedReg.Register(dbQueryDuration)
	_ = isolatedReg.Register(logEntriesTotal)
	t.Cleanup(func() {
		otel.SetTracerProvider(origTP)
		otel.SetMeterProvider(origMP)
		global.SetLoggerProvider(origLP)
		prometheus.DefaultRegisterer = origReg
		prometheus.DefaultGatherer = origGatherer
		testTraceExporterFn = origTraceFn
		testMetricExporterFn = origMetricFn
		testLogExporterFn = origLogFn
	})
}

func TestPrometheusMetricsRegistration(t *testing.T) {
	tel, _ := New(Config{})
	metrics := map[string]func(){
		"event_operations_total":    func() { tel.RecordEventOperation("test") },
		"db_query_duration_seconds": func() { tel.RecordDBQuery("test", time.Millisecond) },
		"log_entries_total":         func() { tel.RecordLogEntry() },
	}
	for name, fn := range metrics {
		t.Run(name, func(t *testing.T) { fn() })
	}
}

func TestRecordEventOperation(t *testing.T) {
	eventOperationsTotal.Reset()
	tel, _ := New(Config{})
	ops := []string{"created", "updated", "deleted", "restored", "cloned", "imported"}
	for _, op := range ops {
		tel.RecordEventOperation(op)
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
	tel, _ := New(Config{})
	tel.RecordDBQuery("select", 50*time.Millisecond)
	tel.RecordDBQuery("insert", 100*time.Millisecond)
	count := testutil.CollectAndCount(dbQueryDuration)
	if count == 0 {
		t.Error("expected dbQueryDuration to have collected observations")
	}
}

func TestRecordLogEntry(t *testing.T) {
	tel, _ := New(Config{})
	before := testutil.ToFloat64(logEntriesTotal)
	tel.RecordLogEntry()
	after := testutil.ToFloat64(logEntriesTotal)
	if after-before != 1 {
		t.Errorf("expected log counter to increase by 1, got %f", after-before)
	}
}

func TestMetricsMiddleware(t *testing.T) {
	httpRequestsTotal.Reset()
	httpRequestDuration.Reset()
	tel, _ := New(Config{})
	router := gin.New()
	router.Use(tel.MetricsMiddleware())
	router.GET("/api/test", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
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
	tel, _ := New(Config{})
	router := gin.New()
	router.Use(tel.MetricsMiddleware())
	router.GET("/api/error", func(c *gin.Context) { c.JSON(http.StatusInternalServerError, gin.H{"error": "test error"}) })
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

func TestParseOTelProtocol(t *testing.T) {
	tests := []struct{ env, want string }{{"", "grpc"}, {"grpc", "grpc"}, {"http/protobuf", "http/protobuf"}, {"invalid", "grpc"}}
	for _, tt := range tests {
		t.Run("protocol_"+tt.env, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", tt.env)
			tel := &Telemetry{}
			tel.parseProtocol()
			if tel.protocol != tt.want {
				t.Errorf("protocol = %q, want %q", tel.protocol, tt.want)
			}
		})
	}
}

func TestTraceDBQuery(t *testing.T) {
	tel, _ := New(Config{})
	ctx := context.Background()
	t.Run("successful_query", func(t *testing.T) {
		err := tel.TraceDBQuery(ctx, "test-operation", func(ctx context.Context) error { return nil })
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})
	t.Run("error_query", func(t *testing.T) {
		expectedErr := fmt.Errorf("query failed")
		err := tel.TraceDBQuery(ctx, "test-error", func(ctx context.Context) error { return expectedErr })
		if err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

func TestShutdown(t *testing.T) {
	t.Run("nil_provider", func(t *testing.T) {
		tel := &Telemetry{}
		tel.Shutdown()
	})
	t.Run("valid_provider", func(t *testing.T) {
		tel, _ := New(Config{})
		tel.Shutdown()
		// Also test with explicit tp
		tp := sdktrace.NewTracerProvider()
		tel2 := &Telemetry{tp: tp}
		tel2.Shutdown()
	})
}

func TestTelemetryGracefulDegradation(t *testing.T) {
	saveRestoreTelemetry(t)
	testTraceExporterFn = func(endpoint, protocol string) (sdktrace.SpanExporter, error) {
		return nil, fmt.Errorf("injected trace failure")
	}
	testMetricExporterFn = func(endpoint, protocol string) (sdkmetric.Exporter, error) {
		return nil, fmt.Errorf("injected metric failure")
	}
	testLogExporterFn = func(endpoint, protocol string) (sdklog.Exporter, error) {
		return nil, fmt.Errorf("injected log failure")
	}
	t.Setenv("OTEL_SERVICE_NAME", "traces-test")
	tel, err := New(Config{Endpoint: "http://127.0.0.1:1", TracesEnabled: true, MetricsEnabled: true, LogsEnabled: true})
	if err != nil {
		t.Fatalf("init with injected failures should not error: %v", err)
	}
	if tel.tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	defer tel.tp.Shutdown(context.Background())
	traceKind, metricKind, logKind := tel.ExporterKinds()
	if traceKind != "stdout" {
		t.Errorf("trace kind = %q, want stdout", traceKind)
	}
	if metricKind != "stdout" {
		t.Errorf("metric kind = %q, want stdout", metricKind)
	}
	if logKind != "stdout" {
		t.Errorf("log kind = %q, want stdout", logKind)
	}
}

func TestOTelMetricsExportedToPrometheus(t *testing.T) {
	res, err := resource.New(context.Background(), resource.WithTelemetrySDK())
	if err != nil {
		t.Fatalf("creating resource: %v", err)
	}
	tel := &Telemetry{cfg: Config{}}
	reg := prometheus.NewRegistry()
	if err := tel.initMetricExporterWithRegisterer(res, reg); err != nil {
		t.Fatalf("initMetricExporterWithRegisterer: %v", err)
	}
	tel.initOTelMetrics()
	tel.RecordEventOperation("prometheus-bridge-test")
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if strings.Contains(mf.GetName(), "traces_event_operations") {
			return
		}
	}
	names := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	t.Fatalf("OTel event operations metric not exported to Prometheus; got: %v", names)
}

func TestMetricsConcurrentSafety(t *testing.T) {
	tel, _ := New(Config{})
	done := make(chan bool, 10)
	for range 10 {
		go func() {
			tel.RecordEventOperation("concurrent-test")
			tel.RecordDBQuery("concurrent-test", time.Millisecond)
			tel.RecordLogEntry()
			done <- true
		}()
	}
	for range 10 {
		<-done
	}
}

func TestOTelEmptyEndpointStdout(t *testing.T) {
	saveRestoreTelemetry(t)
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-empty")
	tel, err := New(Config{Endpoint: "", TracesEnabled: true, MetricsEnabled: true, LogsEnabled: true})
	if err != nil {
		t.Fatalf("init with empty endpoint should not fail: %v", err)
	}
	if tel == nil || tel.tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	defer tel.tp.Shutdown(context.Background())
	traceKind, metricKind, logKind := tel.ExporterKinds()
	if traceKind != "stdout" {
		t.Errorf("trace kind = %q, want stdout", traceKind)
	}
	if metricKind != "stdout" {
		t.Errorf("metric kind = %q, want stdout", metricKind)
	}
	if logKind != "stdout" {
		t.Errorf("log kind = %q, want stdout", logKind)
	}
}

func TestOTelHTTPProtobufConstructors(t *testing.T) {
	saveRestoreTelemetry(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-http")
	tel, err := New(Config{Endpoint: "http://127.0.0.1:4318", TracesEnabled: true, MetricsEnabled: true, LogsEnabled: true})
	if err != nil {
		t.Fatalf("init http/protobuf: %v", err)
	}
	defer tel.tp.Shutdown(context.Background())
	traceKind, metricKind, logKind := tel.ExporterKinds()
	if traceKind != "otlp-http" {
		t.Errorf("trace kind = %q, want otlp-http", traceKind)
	}
	if metricKind != "otlp-http" {
		t.Errorf("metric kind = %q, want otlp-http", metricKind)
	}
	if logKind != "otlp-http" {
		t.Errorf("log kind = %q, want otlp-http", logKind)
	}
	saveRestoreTelemetry(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-grpc")
	tel2, err := New(Config{Endpoint: "http://127.0.0.1:4317", TracesEnabled: true, MetricsEnabled: true, LogsEnabled: true})
	if err != nil {
		t.Fatalf("init grpc: %v", err)
	}
	defer tel2.tp.Shutdown(context.Background())
	traceKind, metricKind, logKind = tel2.ExporterKinds()
	if traceKind != "otlp-grpc" {
		t.Errorf("trace kind grpc = %q, want otlp-grpc", traceKind)
	}
	if metricKind != "otlp-grpc" {
		t.Errorf("metric kind grpc = %q, want otlp-grpc", metricKind)
	}
	if logKind != "otlp-grpc" {
		t.Errorf("log kind grpc = %q, want otlp-grpc", logKind)
	}
}

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
			saveRestoreTelemetry(t)
			t.Setenv("OTEL_SERVICE_NAME", "traces-test-matrix")
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
			tel, err := New(Config{Endpoint: "http://127.0.0.1:4317", TracesEnabled: tc.traces, MetricsEnabled: tc.metrics, LogsEnabled: tc.logs})
			if err != nil {
				t.Fatalf("init matrix %s: %v", tc.name, err)
			}
			defer tel.tp.Shutdown(context.Background())
			traceKind, metricKind, logKind := tel.ExporterKinds()
			check := func(kind string, enabled bool, signal string) {
				if !enabled && kind != "disabled" {
					t.Errorf("%s kind = %q, want disabled", signal, kind)
				}
				if enabled && !strings.HasPrefix(kind, "otlp-") {
					t.Errorf("%s kind = %q, want otlp-*", signal, kind)
				}
			}
			check(traceKind, tc.traces, "trace")
			check(metricKind, tc.metrics, "metric")
			check(logKind, tc.logs, "log")
		})
	}
}

func TestOTelPersistedSubsetHonored(t *testing.T) {
	saveRestoreTelemetry(t)
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-persisted")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	tel, err := New(Config{Endpoint: "http://127.0.0.1:4317", TracesEnabled: false, MetricsEnabled: true, LogsEnabled: false})
	if err != nil {
		t.Fatalf("init persisted subset: %v", err)
	}
	defer tel.tp.Shutdown(context.Background())
	traceKind, metricKind, logKind := tel.ExporterKinds()
	if traceKind != "disabled" {
		t.Errorf("trace kind = %q, want disabled", traceKind)
	}
	if metricKind != "otlp-grpc" {
		t.Errorf("metric kind = %q, want otlp-grpc", metricKind)
	}
	if logKind != "disabled" {
		t.Errorf("log kind = %q, want disabled", logKind)
	}
}

func TestOTelMetricLogFallbackToStdout(t *testing.T) {
	saveRestoreTelemetry(t)
	t.Setenv("OTEL_SERVICE_NAME", "traces-test-fallback")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	testMetricExporterFn = func(endpoint, protocol string) (sdkmetric.Exporter, error) {
		return nil, fmt.Errorf("injected metric failure")
	}
	testLogExporterFn = func(endpoint, protocol string) (sdklog.Exporter, error) {
		return nil, fmt.Errorf("injected log failure")
	}
	tel, err := New(Config{Endpoint: "http://127.0.0.1:4317", TracesEnabled: true, MetricsEnabled: true, LogsEnabled: true})
	if err != nil {
		t.Fatalf("init with injected failures should not fail: %v", err)
	}
	defer tel.tp.Shutdown(context.Background())
	_, metricKind, logKind := tel.ExporterKinds()
	traceKind, _, _ := tel.ExporterKinds()
	if metricKind != "stdout" {
		t.Errorf("metric kind after injected failure = %q, want stdout", metricKind)
	}
	if logKind != "stdout" {
		t.Errorf("log kind after injected failure = %q, want stdout", logKind)
	}
	if traceKind != "otlp-grpc" {
		t.Errorf("trace kind = %q, want otlp-grpc", traceKind)
	}
	t.Run("trace_fallback", func(t *testing.T) {
		saveRestoreTelemetry(t)
		t.Setenv("OTEL_SERVICE_NAME", "traces-test-trace-fallback")
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
		testTraceExporterFn = func(endpoint, protocol string) (sdktrace.SpanExporter, error) {
			return nil, fmt.Errorf("injected trace failure")
		}
		tel2, err := New(Config{Endpoint: "http://127.0.0.1:4317", TracesEnabled: true, MetricsEnabled: true, LogsEnabled: true})
		if err != nil {
			t.Fatalf("trace fallback should not fail: %v", err)
		}
		defer tel2.tp.Shutdown(context.Background())
		traceKind, _, _ := tel2.ExporterKinds()
		if traceKind != "stdout" {
			t.Errorf("trace kind after failure = %q, want stdout", traceKind)
		}
	})
}

func TestOTelMetricExporterFallbackDirect(t *testing.T) {
	saveRestoreTelemetry(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	testMetricExporterFn = func(endpoint, protocol string) (sdkmetric.Exporter, error) {
		return nil, fmt.Errorf("injected failure")
	}
	tel := &Telemetry{cfg: Config{Endpoint: "http://127.0.0.1:4317", MetricsEnabled: true}, protocol: "grpc"}
	tel.NewMetricExporter = func(ep string) (sdkmetric.Exporter, error) { return testMetricExporterFn(ep, "grpc") }
	tel.NewLogExporter = func(ep string) (sdklog.Exporter, error) { return stdoutlog.New() }
	tel.NewTraceExporter = func(ep string) (sdktrace.SpanExporter, error) { s, _ := stdouttrace.New(); return s, nil }
	res, _ := resource.New(context.Background(), resource.WithTelemetrySDK())
	reg := prometheus.NewRegistry()
	if err := tel.initMetricExporterWithRegisterer(res, reg); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	_, metricKind, _ := tel.ExporterKinds()
	if metricKind != "stdout" {
		t.Errorf("kind = %q, want stdout", metricKind)
	}
}

func TestOTelLogExporterFallbackDirect(t *testing.T) {
	saveRestoreTelemetry(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	testLogExporterFn = func(endpoint, protocol string) (sdklog.Exporter, error) {
		return nil, fmt.Errorf("injected failure")
	}
	tel := &Telemetry{cfg: Config{Endpoint: "http://127.0.0.1:4317", LogsEnabled: true}, protocol: "grpc"}
	tel.NewLogExporter = func(ep string) (sdklog.Exporter, error) { return testLogExporterFn(ep, "grpc") }
	res, _ := resource.New(context.Background(), resource.WithTelemetrySDK())
	if err := tel.initLogExporter(res); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	_, _, logKind := tel.ExporterKinds()
	if logKind != "stdout" {
		t.Errorf("kind = %q, want stdout", logKind)
	}
}
