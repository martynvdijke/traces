## Context

TRACES admin panel uses a single-page HTML/JS with Bootstrap tabs. Each settings section (Integrations, Memories, AI, Backup) has its own card with save handlers calling REST APIs. There is no unified logging — debugging requires server `stdout` access. Existing telemetry only covers Prometheus metrics and OTel traces (stdout exporter).

The admin panel is server-rendered HTML with vanilla JS, served as static files. Adding a log viewer means extending this same pattern with a new tab and backend API.

## Goals / Non-Goals

**Goals:**
- Provide a centralized "Logs" tab in the admin panel showing application events
- Store log entries in SQLite with severity, timestamp, source, and message
- Export logs via OpenTelemetry OTLP exporter alongside existing traces
- Let admin set the minimum severity level shown in the UI (default: `warn`)
- Onboard all settings endpoints (Gotify, Email, Ollama, Immich, Memories, Backup) to emit structured logs
- Log viewer supports: severity filter, source filter, date range, search, auto-refresh

**Non-Goals:**
- Real-time streaming or WebSocket log delivery (polling refresh is sufficient)
- Log rotation or retention policies beyond what SQLite provides (future concern)
- Multi-user audit log differentiation (single admin for now)
- Correlation IDs across log entries
- Replacing Go's `log.Printf` — app-level structured logs only

## Decisions

### 1. SQLite-backed log store (not in-memory, not file-based)
**Why**: Already using SQLite; no new infrastructure. Allows querying, filtering, pagination. Survives restarts. The log table will be append-mostly with limited rows (capped at ~10K entries, auto-prune oldest).
**Alternative considered**: Filesystem logs — harder to query. In-memory ring buffer — lost on restart.

### 2. OTel Logs via OTLP gRPC/HTTP exporter
**Why**: Project already uses OTel for traces. Adding OTel logs completes the observability story. OTLP is the standard protocol and can export to any OTel-compatible backend (Jaeger, Grafana Tempo, Grafana Loki via OTel gateway, Datadog, etc.).
**Approach**: Add `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc` or `otlploghttp`. Configure via env vars `OTEL_EXPORTER_OTLP_ENDPOINT` etc. Logs are both stored locally in SQLite AND exported via OTel.

### 3. Structured log entries with `severity`, `source`, `message`, `metadata`
**Why**: Structured logging enables filtering, searching, and OTel attribute mapping.
**Fields**: `id`, `timestamp`, `severity` (debug/info/warn/error), `source` (e.g., "gotify", "email", "ollama", "system"), `message`, `metadata` (JSON blob for extra context).

### 4. Polling auto-refresh at 10-second interval
**Why**: Simple, no WebSocket infrastructure needed. Matches the existing admin panel pattern (no live-update framework).
**Alternative considered**: SSE — more complex, adds server-side state. HTMX polling — could work but current pattern is JS fetch.

### 5. Verbosity setting stored in `log_settings` DB table
**Why**: Persists across restarts. Simple key-value pattern. Default severity threshold is `warn`.

## Risks / Trade-offs

- **Log table growth** → Mitigation: cap at 10K rows, auto-prune oldest on insert when over limit. Also expose a "clear logs" button.
- **OTLP exporter failure** → Mitigation: non-blocking — log to SQLite first, then attempt OTel export. If export fails, log locally and continue.
- **Performance overhead on config saves** → Mitigation: log writes are async (goroutine for OTel, synchronous SQLite INSERT is fast enough for low-frequency config saves).
- **Stale logs in the UI** → Mitigation: the polling refresh will show new entries; old entries remain until pruned.
