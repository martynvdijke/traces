## Why

traces is the most OTel-complete project in the portfolio: it already has OTLP gRPC trace and metric exporters (`otlptracegrpc`, `otlpmetricgrpc`), stdout exporters for all three pillars, the OTel logs SDK (`otel/log`, `sdk/log`), and Prometheus `client_golang` metrics — all managed through `telemetry.go` with service name "traces". However, the implementation has gaps: no `otelgin` middleware for automatic Gin request tracing, no DB query tracing, only stdout log export (no OTLP log exporter), no HTTP/protobuf secondary exporters, and no slog bridge for log-to-trace correlation.

Completing the OTel support — adding `otelgin`, DB query tracing, OTLP log export, HTTP/protobuf exporters, and the slog bridge — brings traces to full observability across all three pillars with structured log correlation. Aligning on gRPC-primary with HTTP/protobuf-secondary matches the shared convention across all projects.

## What Changes

- **Add OTLP HTTP/protobuf exporters** (`otlptracehttp`, `otlpmetrichttp`, `otlploghttp`) alongside existing gRPC exporters for HTTP/protobuf secondary support
- **Add `otelgin` middleware** to automatically create spans for every HTTP request with method, path, and status code attributes
- **Add OTel metric instruments** for HTTP request count (`otel_http_requests_total`) and duration (`otel_http_request_duration_seconds`), bridged to Prometheus via the OTel Prometheus exporter alongside existing client_golang metrics
- **Add DB query tracing** — instrument SQLite queries with OTel spans to capture DB latency in traces
- **Add OTLP log export** (`otlploggrpc`/`otlploghttp`) — currently only `stdoutlog` is present
- **Wire the slog bridge** for log-to-trace correlation
- **Add configurable sampling and resource attributes** — support `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`, `OTEL_RESOURCE_ATTRIBUTES` env vars
- **Graceful degradation** — if OTel is not configured (no OTLP endpoint), fall back to no-op propagation without crashing
- **Tests** — unit tests for telemetry initialization and middleware, integration test verifying trace/metric/log export configuration

## Capabilities

### New Capabilities
- `otel-telemetry`: OpenTelemetry-based distributed tracing, metrics, and logs with configurable OTLP export (gRPC + HTTP), Gin request instrumentation, DB query tracing, and slog bridge

### Modified Capabilities
<!-- No existing capabilities are having their requirements changed -->

## Impact

- `go.mod`: add `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`, `otlpmetrichttp`, `otlploggrpc`, `otlploghttp`, `go.opentelemetry.io/otel/exporters/prometheus`, `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin`
- `telemetry.go`: extend to add HTTP/protobuf exporters, otelgin integration, OTLP log export, metric instruments, slog bridge
- `main.go`: integrate `otelgin` middleware, update telemetry initialization and shutdown
- New file for DB query tracing helper
- New env vars: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_TRACES_SAMPLER`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`
- CI: no pipeline changes needed — OTel is a pure code addition