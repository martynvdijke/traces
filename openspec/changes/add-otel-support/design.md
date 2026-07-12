## Context

traces is a Go 1.26 application using the **Gin** router and **SQLite** (`mattn/go-sqlite3`). It is the most OTel-complete project in the portfolio. The `telemetry.go` file already has:
- OTLP gRPC trace export (`otlptracegrpc`) — already aligned with gRPC primary convention
- OTLP gRPC metric export (`otlpmetricgrpc`) — already aligned
- stdout exporters for all three pillars (`stdouttrace`, `stdoutmetric`, `stdoutlog`)
- OTel logs SDK (`otel/log v0.20.0`, `sdk/log v0.20.0`)
- Prometheus `client_golang` metrics
- Service name from `OTEL_SERVICE_NAME` env var, defaulting to "traces"

Gaps: no `otelgin` middleware (no automatic Gin request tracing), no DB query tracing, only stdout log export (no OTLP log exporter — `otlploggrpc`/`otlploghttp` missing), no HTTP/protobuf secondary exporters for traces/metrics, no slog bridge for log-to-trace correlation. The stable OTel SDK is at v1.44.0.

## Goals / Non-Goals

**Goals:**
- Add HTTP/protobuf secondary exporters for traces, metrics, and logs
- Gin request tracing via `otelgin` middleware — automatic spans per request
- OTel-native HTTP metrics (request count, duration) exposed alongside existing Prometheus metrics via the OTel Prometheus exporter
- DB query tracing — wrap SQLite queries with OTel spans
- OTLP log export (`otlploggrpc`/`otlploghttp`) — replace/augment stdout-only log export
- Slog bridge for log-to-trace correlation
- Standard OTel env var support: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACES_SAMPLER`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_SERVICE_NAME`
- Graceful degradation: no OTel config → noop, partial failure → warn + fallback
- Unit tests for telemetry init and integration test for exporter configuration
- All existing tests pass, CI stays green

**Non-Goals:**
- Not replacing the existing Prometheus `client_golang` metrics — OTel metrics are additive
- Not instrumenting every individual handler (Gin middleware covers the request lifecycle; DB tracing covers queries)
- Not adding OTel auto-instrumentation agents or sidecars
- Not changing the Dockerfile — OTel config is env-var driven
- Not removing stdout exporters — they remain useful for local development

## Decisions

**Decision 1: Add HTTP/protobuf secondary exporters alongside existing gRPC**

Both gRPC (already present for traces/metrics) and HTTP/protobuf will be supported for all three pillars. The protocol is selected via `OTEL_EXPORTER_OTLP_PROTOCOL` (default: `grpc`).

Rationale: gRPC is already the primary exporter. Adding HTTP/protobuf provides fallback for environments where gRPC is blocked. Aligns with the shared convention.

Alternative considered: Keep gRPC-only. Rejected: HTTP/protobuf secondary is part of the shared alignment convention.

**Decision 2: otelgin middleware for request tracing**

Use `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin` middleware.

Rationale: otelgin automatically creates spans with HTTP semantic convention attributes, handles trace context propagation via `traceparent` headers, and sets span status on errors. traces currently has no automatic request tracing despite having trace export.

**Decision 3: OTel Prometheus exporter for metrics bridge, additive to existing client_golang**

Use `go.opentelemetry.io/otel/exporters/prometheus` to expose OTel metric instruments at the existing Prometheus endpoint alongside current `client_golang` metrics.

Rationale: The existing Prometheus metrics must continue working. The OTel Prometheus exporter converts OTel metrics to Prometheus text format automatically, so we instrument once with OTel and both sources are served from the same endpoint.

Alternative considered: Replace Prometheus client_golang entirely. Rejected: existing metrics have custom patterns. Additive approach is safer.

**Decision 4: DB query tracing via helper function wrapper**

Create a DB query tracing helper with a `TraceDBQuery(ctx, operation, dbFunc)` function that wraps a SQLite query in an OTel span.

Rationale: A wrapper function allows per-query opt-in without touching every call site at once. Key queries will be wrapped first.

**Decision 5: Config via standard OTel env vars only**

The app relies on the Go OTel SDK's automatic env var detection. Do NOT duplicate OTEL_* vars in app config.

Rationale: The OTel SDK already reads all standard env vars. Duplicating this is unnecessary and risks drift from the spec.

**Decision 6: Extend telemetry.go**

- `telemetry.go`: extend to add HTTP/protobuf exporters, otelgin integration, OTLP log export, metric instruments, slog bridge
- `main.go`: add `otelgin.Middleware()` to the Gin router, update shutdown

Rationale: The existing telemetry.go is the natural home for all OTel initialization. Extending it keeps concerns centralized.

**Decision 7: OTLP log export replaces stdout as default for logs**

Add `otlploggrpc`/`otlploghttp` for production log export. Keep `stdoutlog` as a development fallback when no OTLP endpoint is configured.

Rationale: stdout-only logs are insufficient for production. OTLP log export enables centralized log collection with trace correlation. Keeping stdout for development preserves the current local dev experience.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| OTLP HTTP exporter connection blocks startup | Move exporter connection to background goroutine with timeout; server starts with stdout fallback |
| OTel Prometheus exporter duplicates existing Prometheus output | Namespace OTel metrics with `otel_` prefix to avoid collision |
| `otelgin` middleware version compatibility | Pin to same minor version as OTel SDK (v1.44.x / contrib v0.69.x) using go.mod |
| DB query tracing adds overhead to every query | No overhead when no exporter is registered; sampling reduces overhead in production |
| Breaking existing Prometheus scrapers | Additive only — existing Prometheus metrics are untouched |
| Logs SDK is still v0.20.0 (unstable) | Pin version explicitly; API may change in future |

## Open Questions

- Should we add a health check endpoint for the OTel exporter? — Deferred
- Should stdout exporters be removed entirely once OTLP is verified? — Keep for development; make configurable