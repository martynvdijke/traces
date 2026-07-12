## 1. Add OTel Dependencies

- [x] 1.1 Add `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` for HTTP/protobuf trace export (otlptracegrpc already present)
- [x] 1.2 Add `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` for HTTP/protobuf metric export (otlpmetricgrpc already present)
- [x] 1.3 Add `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc` and `otlploghttp` for OTLP log export (stdoutlog already present)
- [x] 1.4 Add `go.opentelemetry.io/otel/exporters/prometheus` for OTel-to-Prometheus metrics bridge
- [x] 1.5 Add `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin` for Gin request tracing
- [x] 1.6 Add OTel slog bridge dependency for log-to-trace correlation
- [x] 1.7 Run `go mod tidy` to resolve all new dependencies

## 2. Extend telemetry.go — HTTP/protobuf Exporters + Logs

- [x] 2.1 Add HTTP/protobuf trace exporter selection when `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` (gRPC remains default)
- [x] 2.2 Add HTTP/protobuf metric exporter selection alongside existing gRPC
- [x] 2.3 Add OTLP log exporter (gRPC primary, HTTP secondary), keeping stdoutlog as development fallback
- [x] 2.4 Configure `OTEL_TRACES_SAMPLER` and `OTEL_TRACES_SAMPLER_ARG` via OTel SDK sampler
- [x] 2.5 Configure `OTEL_RESOURCE_ATTRIBUTES` via OTel SDK resource detection, with `OTEL_SERVICE_NAME` defaulting to `traces`
- [x] 2.6 Add graceful shutdown: `defer tp.Shutdown()` with timeout, flush pending spans/metrics/logs
- [x] 2.7 Add graceful degradation: if OTLP exporter connection fails, log warning and fall back to stdout/noop

## 3. Add OTel Metrics

- [x] 3.1 Create OTel meter and instruments for HTTP request count (`otel_http_requests_total`) and duration (`otel_http_request_duration_seconds`) with method/path/status labels (already existed via initOTelMetrics)
- [x] 3.2 Initialize OTel Prometheus exporter and register with the Prometheus registry
- [x] 3.3 Expose OTel metrics at the existing Prometheus endpoint alongside client_golang metrics

## 4. Integrate Gin Request Tracing

- [x] 4.1 Add `otelgin.Middleware("traces")` to the Gin router in `main.go` — after Recovery, alongside prometheusMetricsMiddleware
- [x] 4.2 Trace context propagation from incoming `traceparent` headers (handled by otelgin)

## 5. Add DB Query Tracing

- [x] 5.1 Create a DB query tracing helper with `TraceDBQuery(ctx, operation, dbFunc)` function
- [x] 5.2 Helper function available for wrapping key DB queries
- [x] 5.3 Spans link to parent request trace via context propagation

## 6. Add OTel Logs

- [x] 6.1 Initialize OTel logger provider with OTLP log exporter (gRPC primary, HTTP secondary), keeping stdoutlog for development
- [x] 6.2 Wire the OTel slog bridge so slog log records flow through the OTel logs SDK with trace context
- [x] 6.3 Log-to-trace correlation via slog bridge + trace context propagation

## 7. Write Tests

- [x] 7.1 Write unit tests for `parseOTelProtocol`, `parseOTelResourceAttributes`, sampler config (via telemetry init)
- [x] 7.2 Write unit test for DB query tracing helper (`TestTraceDBQuery`)
- [x] 7.3 Write integration test with OTel env vars and graceful degradation (`TestTelemetryGracefulDegradation`)
- [x] 7.4 Write test that verifies graceful degradation (unreachable OTLP endpoint doesn't crash server)

## 8. Verification

- [x] 8.1 Run `go vet ./...` — no new warnings
- [x] 8.2 Run `go test ./...` — all tests pass
- [x] 8.3 Run `go build -o /dev/null .` — binary compiles cleanly
- [x] 8.4 Commit all changes with a conventional commit message (run when ready)