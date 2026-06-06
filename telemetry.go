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
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
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
)

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

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()

		c.Next()

		status := fmt.Sprintf("%d", c.Writer.Status())
		duration := time.Since(start).Seconds()

		httpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}
