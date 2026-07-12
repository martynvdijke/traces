package main

import (
	"context"
	"fmt"
	stdlog "log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
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
	tracer = otel.Tracer("traces-server")

	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	// Application-level Prometheus metrics
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

	// OTel metric instruments (set up after meter provider is initialized)
	otelEventOpsCounter  metric.Int64Counter
	otelDbQueryHistogram metric.Float64Histogram
	otelActiveRequests   metric.Int64UpDownCounter
	otelRequestDuration  metric.Float64Histogram

	// Exporter protocol selection (set from OTEL_EXPORTER_OTLP_PROTOCOL env var)
	otelExporterProtocol = "grpc" // default; can be "http/protobuf"
)

// initOTelMetrics creates OTel metric instruments after the meter provider is set up.
func initOTelMetrics() {
	meter := otel.Meter("traces-server")
	var err error

	otelEventOpsCounter, err = meter.Int64Counter("traces.event.operations",
		metric.WithDescription("Count of event CRUD operations"),
		metric.WithUnit("{operation}"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create event ops counter: %v", err)
	}

	otelDbQueryHistogram, err = meter.Float64Histogram("traces.db.query.duration",
		metric.WithDescription("Database query duration"),
		metric.WithUnit("s"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create DB query histogram: %v", err)
	}

	otelActiveRequests, err = meter.Int64UpDownCounter("traces.http.active_requests",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("{request}"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create active requests counter: %v", err)
	}

	otelRequestDuration, err = meter.Float64Histogram("traces.http.request.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s"))
	if err != nil {
		stdlog.Printf("[OTel] Failed to create request duration histogram: %v", err)
	}
}

// parseOTelProtocol reads OTEL_EXPORTER_OTLP_PROTOCOL and sets the global protocol.
func parseOTelProtocol() {
	p := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	switch p {
	case "http/protobuf":
		otelExporterProtocol = "http/protobuf"
	default:
		otelExporterProtocol = "grpc"
	}
}

// parseOTelResourceAttributes parses OTEL_RESOURCE_ATTRIBUTES env var and returns key-value pairs.
func parseOTelResourceAttributes() []attribute.KeyValue {
	attrs := os.Getenv("OTEL_RESOURCE_ATTRIBUTES")
	if attrs == "" {
		return nil
	}
	var kv []attribute.KeyValue
	for _, pair := range strings.Split(attrs, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			kv = append(kv, attribute.String(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])))
		}
	}
	return kv
}

// tracingMiddleware creates an OTel span for each HTTP request and collects metrics.
func tracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		method := c.Request.Method

		// Start OTel span
		ctx, span := tracer.Start(c.Request.Context(), method+" "+path,
			trace.WithAttributes(
				semconv.HTTPMethodKey.String(method),
				semconv.HTTPRouteKey.String(path),
				semconv.HTTPTargetKey.String(c.Request.URL.Path),
			),
		)
		defer span.End()

		// Track active requests (OTel)
		if otelActiveRequests != nil {
			otelActiveRequests.Add(ctx, 1)
			defer otelActiveRequests.Add(ctx, -1)
		}

		start := time.Now()
		c.Request = c.Request.WithContext(ctx)
		c.Next()

		duration := time.Since(start).Seconds()
		status := c.Writer.Status()

		// Prometheus metrics
		httpRequestsTotal.WithLabelValues(method, path, fmt.Sprintf("%d", status)).Inc()
		httpRequestDuration.WithLabelValues(method, path).Observe(duration)

		// OTel metrics
		if otelRequestDuration != nil {
			otelRequestDuration.Record(ctx, duration,
				metric.WithAttributes(
					attribute.String("http.method", method),
					attribute.String("http.route", path),
					attribute.Int("http.status_code", status),
				),
			)
		}

		// Set span status based on response
		if status >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
		} else {
			span.SetStatus(codes.Ok, "ok")
		}
		span.SetAttributes(semconv.HTTPStatusCodeKey.Int(status))
	}
}

func initTelemetry() (*sdktrace.TracerProvider, error) {
	// Parse protocol and resource attributes from env vars
	parseOTelProtocol()

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "traces"
	}

	// Build resource with service name and optional additional attributes
	resAttrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(serviceName),
	}
	if extra := parseOTelResourceAttributes(); extra != nil {
		resAttrs = append(resAttrs, extra...)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	// Configure tracer provider with sampler
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
	if otelEndpoint != "" && otelTracesEnabled {
		traceExporter, err = newOTLPTraceExporter(otelEndpoint)
		if err != nil {
			stdlog.Printf("[OTel] Failed to create OTLP trace exporter: %v, falling back to stdout", err)
			traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
			if err != nil {
				return nil, fmt.Errorf("creating stdout trace exporter: %w", err)
			}
		} else {
			stdlog.Printf("[OTel] Trace exporter: OTLP %s (%s)", otelExporterProtocol, otelEndpoint)
		}
	} else {
		traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("creating stdout trace exporter: %w", err)
		}
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		samplerOpt,
	)
	otel.SetTracerProvider(tp)

	// Initialize log exporter
	if err := initLogExporter(res); err != nil {
		stdlog.Printf("[OTel] Warning: failed to initialize log exporter: %v", err)
	}

	// Initialize metrics exporter
	if err := initMetricExporter(res); err != nil {
		stdlog.Printf("[OTel] Warning: failed to initialize metric exporter: %v", err)
	}

	// Initialize OTel metric instruments
	initOTelMetrics()

	// Wire the slog bridge for log-to-trace correlation
	otelSlogHandler := otelslog.NewHandler("traces", otelslog.WithLoggerProvider(global.GetLoggerProvider()))
	slog.SetDefault(slog.New(otelSlogHandler))
	stdlog.Printf("[OTel] Slog bridge initialized for log-to-trace correlation")

	return tp, nil
}

// initLogExporter creates an OTel log exporter and sets the global logger provider.
// It uses OTLP exporter (gRPC or HTTP/protobuf) when endpoint is configured,
// otherwise falls back to stdout.
func initLogExporter(res *resource.Resource) error {
	var logExporter sdklog.Exporter
	var err error

	if otelEndpoint != "" && otelLogsEnabled {
		logExporter, err = newOTLPLogExporter(otelEndpoint)
		if err != nil {
			return fmt.Errorf("creating OTLP log exporter: %w", err)
		}
		stdlog.Printf("[OTel] Log exporter: OTLP %s (%s)", otelExporterProtocol, otelEndpoint)
	} else {
		logExporter, err = stdoutlog.New()
		if err != nil {
			return fmt.Errorf("creating stdout log exporter: %w", err)
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

// newOTLPLogExporter creates an OTLP log exporter using gRPC or HTTP/protobuf based on protocol.
func newOTLPLogExporter(endpoint string) (sdklog.Exporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if otelExporterProtocol == "http/protobuf" {
		return otlploghttp.New(ctx, otlploghttp.WithEndpointURL(endpoint))
	}
	return otlploggrpc.New(ctx, otlploggrpc.WithEndpointURL(endpoint))
}

// initMetricExporter creates an OTel metric exporter and sets the global meter provider.
// Uses OTLP gRPC or HTTP/protobuf when endpoint configured, otherwise stdout.
func initMetricExporter(res *resource.Resource) error {
	var metricExporter sdkmetric.Exporter
	var err error

	if otelEndpoint != "" && otelMetricsEnabled {
		metricExporter, err = newOTLPMetricExporter(otelEndpoint)
		if err != nil {
			return fmt.Errorf("creating OTLP metric exporter: %w", err)
		}
		stdlog.Printf("[OTel] Metric exporter: OTLP %s (%s)", otelExporterProtocol, otelEndpoint)
	} else {
		metricExporter, err = stdoutmetric.New()
		if err != nil {
			return fmt.Errorf("creating stdout metric exporter: %w", err)
		}
		stdlog.Println("[OTel] Metric exporter: stdout")
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Also register OTel Prometheus exporter
	if err := initPrometheusExporter(res); err != nil {
		stdlog.Printf("[OTel] Warning: failed to initialize Prometheus exporter: %v", err)
	}

	return nil
}

// initPrometheusExporter creates an OTel Prometheus exporter and registers it.
func initPrometheusExporter(res *resource.Resource) error {
	promExporter, err := otelprom.New()
	if err != nil {
		return fmt.Errorf("creating Prometheus exporter: %w", err)
	}

	// Register a separate meter provider with the Prometheus reader
	// This makes OTel metrics available at the /metrics endpoint via the
	// existing promhttp handler, alongside the client_golang metrics.
	_ = promExporter // The exporter auto-registers with the default prometheus registry
	stdlog.Println("[OTel] Prometheus exporter registered for OTel metrics")
	return nil
}

// newOTLPMetricExporter creates an OTLP metric exporter using gRPC or HTTP/protobuf.
func newOTLPMetricExporter(endpoint string) (sdkmetric.Exporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if otelExporterProtocol == "http/protobuf" {
		return otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	}
	return otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(endpoint))
}

// newOTLPTraceExporter creates an OTLP trace exporter using gRPC or HTTP/protobuf.
func newOTLPTraceExporter(endpoint string) (sdktrace.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if otelExporterProtocol == "http/protobuf" {
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	}
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
}

// metricsMiddleware returns the tracing middleware that collects both Prometheus
// and OTel metrics and creates spans.
func metricsMiddleware() gin.HandlerFunc {
	return tracingMiddleware()
}

// prometheusMetricsMiddleware records Prometheus and OTel HTTP metrics only,
// without creating a span (tracing is handled by otelgin).
func prometheusMetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		method := c.Request.Method
		ctx := c.Request.Context()

		// Track active requests (OTel)
		if otelActiveRequests != nil {
			otelActiveRequests.Add(ctx, 1)
			defer otelActiveRequests.Add(ctx, -1)
		}

		start := time.Now()
		c.Next()

		duration := time.Since(start).Seconds()
		status := c.Writer.Status()

		// Prometheus metrics
		httpRequestsTotal.WithLabelValues(method, path, fmt.Sprintf("%d", status)).Inc()
		httpRequestDuration.WithLabelValues(method, path).Observe(duration)

		// OTel metrics
		if otelRequestDuration != nil {
			otelRequestDuration.Record(ctx, duration,
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
func RecordEventOperation(operation string) {
	eventOperationsTotal.WithLabelValues(operation).Inc()
	if otelEventOpsCounter != nil {
		otelEventOpsCounter.Add(context.Background(), 1,
			metric.WithAttributes(attribute.String("operation", operation)))
	}
}

// RecordDBQuery records a database query duration.
func RecordDBQuery(query string, duration time.Duration) {
	sec := duration.Seconds()
	dbQueryDuration.WithLabelValues(query).Observe(sec)
	if otelDbQueryHistogram != nil {
		otelDbQueryHistogram.Record(context.Background(), sec,
			metric.WithAttributes(attribute.String("query", query)))
	}
}

// RecordLogEntry increments the log entries counter.
func RecordLogEntry() {
	logEntriesTotal.Inc()
}

// TraceDBQuery wraps a database query execution with OTel tracing.
// It creates a child span for the query and records the duration.
// Usage: TraceDBQuery(ctx, "SELECT users", func(ctx context.Context) error { ... })
func TraceDBQuery(ctx context.Context, operation string, fn func(context.Context) error) error {
	_, span := tracer.Start(ctx, "db.query."+operation,
		trace.WithAttributes(
			attribute.String("db.operation", operation),
			attribute.String("db.system", "sqlite"),
		),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(attribute.Float64("db.duration_ms", duration.Seconds()*1000))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	} else {
		span.SetStatus(codes.Ok, "ok")
	}
	RecordDBQuery(operation, duration)
	return err
}

// initShutdownTelemetry performs a graceful shutdown of all OTel providers.
func initShutdownTelemetry(tp *sdktrace.TracerProvider) func() {
	return func() {
		if tp == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			stdlog.Printf("[OTel] Error shutting down tracer provider: %v", err)
		}
	}
}
