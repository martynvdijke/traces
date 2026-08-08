## Why

The admin panel has no unified logging view. Config saves, errors, and system events are silent — troubleshooting requires server log access. Settings endpoints (Gotify, Email, Ollama, Immich, Memories, Backup) have no audit trail. Adding central logging with OTel export provides observability, auditability, and a self-serve debugging surface.

## What Changes

- New **Logs** tab in the admin navigation with a structured log viewer
- Backend log service: captures app events (config saves, errors, system actions) as structured, severity-tagged log entries stored in SQLite
- OTel log export: logs are exported via OpenTelemetry Logging SDK alongside existing traces
- Log verbosity control: UI slider/selector setting the minimum severity level shown (default: `warn`); config stored in DB
- Onboard settings endpoints onto the logging system:
  - Gotify config save
  - Email config save / test
  - Ollama config save
  - Immich config save / test
  - Memories config save
  - Backup config save / create backup
- Admin log viewer with: severity filter, date range, source filter, search, auto-refresh

## Capabilities

### New Capabilities
- `central-logging`: Admin log viewer UI — tab, table, filters, severity control, polling refresh
- `otel-log-export`: OpenTelemetry log exporter — OTLP gRPC/HTTP export with configurable endpoint, service name, verbosity level stored in DB
- `config-audit-logging`: Backend log recording — structured log entries for config saves, errors, system actions; API endpoints for log query and severity config

### Modified Capabilities
- *(None — no existing specs to modify)*

## Impact

- **Go backend**: New `logging.go` (log service, DB schema), update `telemetry.go` (add log exporter), new API routes under `/api/logs`
- **Admin frontend**: New tab HTML in `admin.html`, new JS module/logic in `ts/admin.ts` for log viewer
- **Database**: New `app_logs` and `log_settings` tables
- **Dependencies**: New OTel log SDK dependencies (`go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc`, `go.opentelemetry.io/otel/sdk/log`)
- **Config endpoints**: All settings save handlers updated to call `logService.Log()`
