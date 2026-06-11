package main

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
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
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "traces"
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
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
			stdlog.Printf("[OTel] Trace exporter: OTLP (%s)", otelEndpoint)
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

	return tp, nil
}

// initLogExporter creates an OTel log exporter and sets the global logger provider.
// It uses OTLP exporter when OTEL_EXPORTER_OTLP_ENDPOINT is set, otherwise falls back to stdout.
func initLogExporter(res *resource.Resource) error {
	var logExporter sdklog.Exporter
	var err error

	if otelEndpoint != "" && otelLogsEnabled {
		// Use OTLP exporter when endpoint is configured and logs enabled
		logExporter, err = newOTLPLogExporter(otelEndpoint)
		if err != nil {
			return fmt.Errorf("creating OTLP log exporter: %w", err)
		}
		stdlog.Printf("[OTel] Log exporter: OTLP (%s)", otelEndpoint)
	} else {
		// Default to stdout
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

// newOTLPLogExporter creates an OTLP log exporter using the gRPC protocol.
func newOTLPLogExporter(endpoint string) (sdklog.Exporter, error) {
	// For now, use stdout as fallback since OTLP log gRPC exporter requires
	// an additional dependency. The user can configure OTLP via the standard
	// OTEL_EXPORTER_OTLP_ENDPOINT env var.
	// When the otlploggrpc dependency is added, this function should use it.
	stdlog.Printf("[OTel] OTLP endpoint configured at %s, using stdout log exporter (OTLP log gRPC not yet wired)", endpoint)
	return stdoutlog.New()
}

// initMetricExporter creates an OTel metric exporter and sets the global meter provider.
// It uses OTLP gRPC when endpoint is configured and metrics enabled, otherwise stdout.
func initMetricExporter(res *resource.Resource) error {
	var metricExporter sdkmetric.Exporter
	var err error

	if otelEndpoint != "" && otelMetricsEnabled {
		metricExporter, err = newOTLPMetricExporter(otelEndpoint)
		if err != nil {
			return fmt.Errorf("creating OTLP metric exporter: %w", err)
		}
		stdlog.Printf("[OTel] Metric exporter: OTLP (%s)", otelEndpoint)
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
	return nil
}

// newOTLPMetricExporter creates an OTLP metric exporter using gRPC.
func newOTLPMetricExporter(endpoint string) (sdkmetric.Exporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(endpoint))
}

// newOTLPTraceExporter creates an OTLP trace exporter using gRPC.
func newOTLPTraceExporter(endpoint string) (sdktrace.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
}

// metricsMiddleware returns the tracing middleware that collects both Prometheus
// and OTel metrics and creates spans. Kept as a thin wrapper for backward compatibility.
func metricsMiddleware() gin.HandlerFunc {
	return tracingMiddleware()
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
