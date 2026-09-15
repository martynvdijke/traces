package telemetry

import (
	"context"
	"fmt"
	stdlog "log"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	eventOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "event_operations_total",
		Help: "Total number of event CRUD operations",
	}, []string{"operation"})

	dbQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "Database query duration in seconds",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"query"})

	logEntriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "log_entries_total",
		Help: "Total number of log entries written",
	})
)

// testSeams allow tests to inject failing constructors before New is called.
var (
	testTraceExporterFn  func(string, string) (sdktrace.SpanExporter, error)
	testMetricExporterFn func(string, string) (sdkmetric.Exporter, error)
	testLogExporterFn    func(string, string) (sdklog.Exporter, error)
)

// Config holds the injected OTel configuration.
type Config struct {
	Endpoint       string
	ServiceName    string
	TracesEnabled  bool
	MetricsEnabled bool
	LogsEnabled    bool
}

// Telemetry holds resolved config and OTel state.
type Telemetry struct {
	cfg Config

	protocol    string
	serviceName string
	tracer      trace.Tracer
	tp          *sdktrace.TracerProvider

	otelEventOpsCounter  metric.Int64Counter
	otelDbQueryHistogram metric.Float64Histogram
	otelActiveRequests   metric.Int64UpDownCounter
	otelRequestDuration  metric.Float64Histogram

	traceExporterKind  string
	metricExporterKind string
	logExporterKind    string

	NewTraceExporter  func(string) (sdktrace.SpanExporter, error)
	NewMetricExporter func(string) (sdkmetric.Exporter, error)
	NewLogExporter    func(string) (sdklog.Exporter, error)
}

// New creates Telemetry reading OTEL_* env vars exactly as the original initTelemetry.
func New(cfg Config) (*Telemetry, error) {
	t := &Telemetry{
		cfg:               cfg,
		NewTraceExporter:  func(endpoint string) (sdktrace.SpanExporter, error) { return newOTLPTraceExporter(endpoint, "grpc") },
		NewMetricExporter: func(endpoint string) (sdkmetric.Exporter, error) { return newOTLPMetricExporter(endpoint, "grpc") },
		NewLogExporter:    func(endpoint string) (sdklog.Exporter, error) { return newOTLPLogExporter(endpoint, "grpc") },
	}
	// Parse protocol from env
	t.parseProtocol()
	// Wire constructor closures to use t.protocol dynamically
	t.NewTraceExporter = func(endpoint string) (sdktrace.SpanExporter, error) {
		if testTraceExporterFn != nil {
			return testTraceExporterFn(endpoint, t.protocol)
		}
		return newOTLPTraceExporter(endpoint, t.protocol)
	}
	t.NewMetricExporter = func(endpoint string) (sdkmetric.Exporter, error) {
		if testMetricExporterFn != nil {
			return testMetricExporterFn(endpoint, t.protocol)
		}
		return newOTLPMetricExporter(endpoint, t.protocol)
	}
	t.NewLogExporter = func(endpoint string) (sdklog.Exporter, error) {
		if testLogExporterFn != nil {
			return testLogExporterFn(endpoint, t.protocol)
		}
		return newOTLPLogExporter(endpoint, t.protocol)
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = cfg.ServiceName
		if serviceName == "" {
			serviceName = "traces"
		}
	}
	t.serviceName = serviceName

	res, err := resource.New(context.Background(),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithContainer(),
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
	)
	if err != nil {
		stdlog.Printf("[OTel] Resource detection warning: %v", err)
	}

	var samplerOpt sdktrace.TracerProviderOption
	switch os.Getenv("OTEL_TRACES_SAMPLER") {
	case "always_on":
		samplerOpt = sdktrace.WithSampler(sdktrace.AlwaysSample())
	case "always_off":
		samplerOpt = sdktrace.WithSampler(sdktrace.NeverSample())
	case "traceidratio":
		ratio := 0.1
		if arg := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); arg != "" {
			fmt.Sscanf(arg, "%f", &ratio)
		}
		samplerOpt = sdktrace.WithSampler(sdktrace.TraceIDRatioBased(ratio))
	case "parentbased_always_on":
		samplerOpt = sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample()))
	case "parentbased_always_off":
		samplerOpt = sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.NeverSample()))
	case "parentbased_traceidratio":
		ratio := 0.1
		if arg := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); arg != "" {
			fmt.Sscanf(arg, "%f", &ratio)
		}
		samplerOpt = sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)))
	default:
		samplerOpt = sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample()))
	}

	var traceExporter sdktrace.SpanExporter
	if cfg.Endpoint != "" && cfg.TracesEnabled {
		traceExporter, err = t.NewTraceExporter(cfg.Endpoint)
		if err != nil {
			stdlog.Printf("[OTel] Failed to create OTLP trace exporter: %v, falling back to stdout", err)
			traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
			if err != nil {
				return nil, fmt.Errorf("creating stdout trace exporter: %w", err)
			}
			t.traceExporterKind = "stdout"
		} else {
			if t.protocol == "http/protobuf" {
				t.traceExporterKind = "otlp-http"
			} else {
				t.traceExporterKind = "otlp-grpc"
			}
			stdlog.Printf("[OTel] Trace exporter: OTLP %s (%s)", t.protocol, cfg.Endpoint)
		}
	} else {
		traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("creating stdout trace exporter: %w", err)
		}
		if !cfg.TracesEnabled {
			t.traceExporterKind = "disabled"
		} else {
			t.traceExporterKind = "stdout"
		}
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		samplerOpt,
	)
	otel.SetTracerProvider(tp)
	t.tp = tp
	t.tracer = tp.Tracer("traces-server")

	if err := t.initLogExporter(res); err != nil {
		stdlog.Printf("[OTel] Warning: failed to initialize log exporter: %v", err)
	}
	if err := t.initMetricExporter(res); err != nil {
		stdlog.Printf("[OTel] Warning: failed to initialize metric exporter: %v", err)
	}
	t.initOTelMetrics()

	otelSlogHandler := otelslog.NewHandler(t.serviceName, otelslog.WithLoggerProvider(global.GetLoggerProvider()))
	slog.SetDefault(slog.New(otelSlogHandler))
	stdlog.Printf("[OTel] Slog bridge initialized for log-to-trace correlation")

	return t, nil
}

func (t *Telemetry) parseProtocol() {
	p := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	switch p {
	case "http/protobuf":
		t.protocol = "http/protobuf"
	default:
		t.protocol = "grpc"
	}
}

func (t *Telemetry) initOTelMetrics() {
	meter := otel.Meter("traces-server")
	var err error
	t.otelEventOpsCounter, err = meter.Int64Counter("traces.event.operations",
		metric.WithDescription("Count of event CRUD operations"),
		metric.WithUnit("{operation}"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create event ops counter: %v", err)
	}
	t.otelDbQueryHistogram, err = meter.Float64Histogram("traces.db.query.duration",
		metric.WithDescription("Database query duration"),
		metric.WithUnit("s"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create DB query histogram: %v", err)
	}
	t.otelActiveRequests, err = meter.Int64UpDownCounter("traces.http.active_requests",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("{request}"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create active requests counter: %v", err)
	}
	t.otelRequestDuration, err = meter.Float64Histogram("traces.http.request.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create request duration histogram: %v", err)
	}
}

func (t *Telemetry) initLogExporter(res *resource.Resource) error {
	var logExporter sdklog.Exporter
	var err error
	if t.cfg.Endpoint != "" && t.cfg.LogsEnabled {
		logExporter, err = t.NewLogExporter(t.cfg.Endpoint)
		if err != nil {
			stdlog.Printf("[OTel] Failed to create OTLP log exporter: %v, falling back to stdout", err)
			logExporter, err = stdoutlog.New()
			if err != nil {
				return fmt.Errorf("creating stdout log exporter: %w", err)
			}
			t.logExporterKind = "stdout"
		} else {
			if t.protocol == "http/protobuf" {
				t.logExporterKind = "otlp-http"
			} else {
				t.logExporterKind = "otlp-grpc"
			}
			stdlog.Printf("[OTel] Log exporter: OTLP %s (%s)", t.protocol, t.cfg.Endpoint)
		}
	} else {
		logExporter, err = stdoutlog.New()
		if err != nil {
			return fmt.Errorf("creating stdout log exporter: %w", err)
		}
		if !t.cfg.LogsEnabled {
			t.logExporterKind = "disabled"
		} else {
			t.logExporterKind = "stdout"
		}
		stdlog.Println("[OTel] Log exporter: stdout")
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	global.SetLoggerProvider(lp)
	return nil
}

func newOTLPLogExporter(endpoint string, protocol string) (sdklog.Exporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if protocol == "http/protobuf" {
		return otlploghttp.New(ctx, otlploghttp.WithEndpointURL(endpoint))
	}
	return otlploggrpc.New(ctx, otlploggrpc.WithEndpointURL(endpoint))
}

func (t *Telemetry) initMetricExporter(res *resource.Resource) error {
	return t.initMetricExporterWithRegisterer(res, prometheus.DefaultRegisterer)
}

func (t *Telemetry) initMetricExporterWithRegisterer(res *resource.Resource, reg prometheus.Registerer) error {
	var metricExporter sdkmetric.Exporter
	var err error
	if t.cfg.Endpoint != "" && t.cfg.MetricsEnabled {
		metricExporter, err = t.NewMetricExporter(t.cfg.Endpoint)
		if err != nil {
			stdlog.Printf("[OTel] Failed to create OTLP metric exporter: %v, falling back to stdout", err)
			metricExporter, err = stdoutmetric.New()
			if err != nil {
				return fmt.Errorf("creating stdout metric exporter: %w", err)
			}
			t.metricExporterKind = "stdout"
		} else {
			if t.protocol == "http/protobuf" {
				t.metricExporterKind = "otlp-http"
			} else {
				t.metricExporterKind = "otlp-grpc"
			}
			stdlog.Printf("[OTel] Metric exporter: OTLP %s (%s)", t.protocol, t.cfg.Endpoint)
		}
	} else {
		metricExporter, err = stdoutmetric.New()
		if err != nil {
			return fmt.Errorf("creating stdout metric exporter: %w", err)
		}
		if !t.cfg.MetricsEnabled {
			t.metricExporterKind = "disabled"
		} else {
			t.metricExporterKind = "stdout"
		}
		stdlog.Println("[OTel] Metric exporter: stdout")
	}
	opts := []sdkmetric.Option{
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
	}
	if promExporter, perr := otelprom.New(otelprom.WithRegisterer(reg)); perr != nil {
		stdlog.Printf("[OTel] Prometheus exporter unavailable: %v", perr)
	} else {
		opts = append(opts, sdkmetric.WithReader(promExporter))
		stdlog.Println("[OTel] Prometheus exporter registered for OTel metrics")
	}
	mp := sdkmetric.NewMeterProvider(opts...)
	otel.SetMeterProvider(mp)
	return nil
}

func newOTLPMetricExporter(endpoint string, protocol string) (sdkmetric.Exporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if protocol == "http/protobuf" {
		return otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	}
	return otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(endpoint))
}

func newOTLPTraceExporter(endpoint string, protocol string) (sdktrace.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if protocol == "http/protobuf" {
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	}
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
}

// Tracer returns the tracer for creating spans.
func (t *Telemetry) Tracer() trace.Tracer { return t.tracer }

// ServiceName returns the resolved OTel service name.
func (t *Telemetry) ServiceName() string { return t.serviceName }

// ExporterKinds returns the exporter kind strings for tests.
func (t *Telemetry) ExporterKinds() (traceKind, metricKind, logKind string) {
	return t.traceExporterKind, t.metricExporterKind, t.logExporterKind
}

// TracerProvider returns the underlying TracerProvider for shutdown wiring.
func (t *Telemetry) TracerProvider() *sdktrace.TracerProvider { return t.tp }

// MetricsMiddleware is the gin middleware that records HTTP metrics.
func (t *Telemetry) MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		method := c.Request.Method
		ctx := c.Request.Context()
		if t.otelActiveRequests != nil {
			t.otelActiveRequests.Add(ctx, 1)
			defer t.otelActiveRequests.Add(ctx, -1)
		}
		start := time.Now()
		c.Next()
		duration := time.Since(start).Seconds()
		status := c.Writer.Status()
		httpRequestsTotal.WithLabelValues(method, path, fmt.Sprintf("%d", status)).Inc()
		httpRequestDuration.WithLabelValues(method, path).Observe(duration)
		if t.otelRequestDuration != nil {
			t.otelRequestDuration.Record(ctx, duration,
				metric.WithAttributes(
					attribute.String("http.method", method),
					attribute.String("http.route", path),
					attribute.Int("http.status_code", status),
				),
			)
		}
	}
}

// RecordEventOperation increments the event operations counter.
func (t *Telemetry) RecordEventOperation(operation string) {
	eventOperationsTotal.WithLabelValues(operation).Inc()
	if t != nil && t.otelEventOpsCounter != nil {
		t.otelEventOpsCounter.Add(context.Background(), 1,
			metric.WithAttributes(attribute.String("operation", operation)))
	}
}

// RecordDBQuery records a database query duration.
func (t *Telemetry) RecordDBQuery(query string, d time.Duration) {
	sec := d.Seconds()
	dbQueryDuration.WithLabelValues(query).Observe(sec)
	if t != nil && t.otelDbQueryHistogram != nil {
		t.otelDbQueryHistogram.Record(context.Background(), sec,
			metric.WithAttributes(attribute.String("query", query)))
	}
}

// RecordLogEntry increments the log entries counter.
func (t *Telemetry) RecordLogEntry() {
	logEntriesTotal.Inc()
	_ = t
}

// TraceDBQuery wraps a DB query with a span and records its duration.
func (t *Telemetry) TraceDBQuery(ctx context.Context, operation string, fn func(context.Context) error) error {
	if t == nil {
		start := time.Now()
		err := fn(ctx)
		tRecord := time.Since(start)
		dbQueryDuration.WithLabelValues(operation).Observe(tRecord.Seconds())
		return err
	}
	var span trace.Span
	if t.tracer != nil {
		_, span = t.tracer.Start(ctx, "db.query."+operation,
			trace.WithAttributes(
				attribute.String("db.operation", operation),
				attribute.String("db.system", "sqlite"),
			),
		)
		defer span.End()
	}
	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)
	if span != nil {
		span.SetAttributes(attribute.Float64("db.duration_ms", duration.Seconds()*1000))
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		} else {
			span.SetStatus(codes.Ok, "ok")
		}
	}
	t.RecordDBQuery(operation, duration)
	return err
}

// Shutdown flushes telemetry providers.
func (t *Telemetry) Shutdown() {
	if t == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if t.tp != nil {
		if err := t.tp.Shutdown(ctx); err != nil {
			stdlog.Printf("[OTel] Error shutting down tracer provider: %v", err)
		}
	}
	if mp, ok := otel.GetMeterProvider().(interface {
		Shutdown(context.Context) error
	}); ok {
		if err := mp.Shutdown(ctx); err != nil {
			stdlog.Printf("[OTel] Error shutting down meter provider: %v", err)
		}
	}
	if lp := global.GetLoggerProvider(); lp != nil {
		if s, ok := lp.(interface {
			Shutdown(context.Context) error
		}); ok {
			if err := s.Shutdown(ctx); err != nil {
				stdlog.Printf("[OTel] Error shutting down logger provider: %v", err)
			}
		}
	}
}
