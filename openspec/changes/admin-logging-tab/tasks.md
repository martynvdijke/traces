## 1. Database Schema & Init

- [x] 1.1 Add `app_logs` and `log_settings` table creation to `initDB()` in `main.go`
- [x] 1.2 `app_logs` columns: id (INTEGER PK AUTOINCREMENT), timestamp (TEXT), severity (TEXT), source (TEXT), message (TEXT), metadata (TEXT/JSON)
- [x] 1.3 `log_settings` columns: id (INTEGER PK =1), min_severity (TEXT DEFAULT 'warn')
- [x] 1.4 Insert default log_settings row on first init

## 2. Backend Log Service (`logging.go`)

- [x] 2.1 Create `logging.go` with `LogEntry` struct and `LogService` type
- [x] 2.2 Implement `LogService.Log(severity, source, message, metadata)` method — inserts into `app_logs`, respects min_severity threshold
- [x] 2.3 Implement row cap (10K) — prune oldest rows when exceeded after insert
- [x] 2.4 Implement `LogService.Init()` — ensure log_settings row exists
- [x] 2.5 Implement `LogService.SetMinSeverity(severity)` and `LogService.GetMinSeverity() string`
- [x] 2.6 Initialize global `logService` in `main()` after DB init

## 3. Log API Routes

- [x] 3.1 Add `GET /api/logs` handler — list logs with query params: severity, source, q, limit, offset, since
- [x] 3.2 Add `GET /api/logs/count` handler — return total log count
- [x] 3.3 Add `DELETE /api/logs` handler — clear all log entries
- [x] 3.4 Add `GET /api/logs/settings` handler — return log_settings JSON
- [x] 3.5 Add `POST /api/logs/settings` handler — update min_severity
- [x] 3.6 Register all log routes under the authenticated `/api` group in `main.go`

## 4. OTel Log Exporter (`telemetry.go` update)

- [x] 4.1 Add OTel log SDK dependency: `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc` (or http)
- [x] 4.2 Add OTel SDK log package: `go.opentelemetry.io/otel/sdk/log`
- [x] 4.3 Extend `initTelemetry()` to initialize an OTel log exporter and logger provider
- [x] 4.4 Respect `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME` env vars
- [x] 4.5 Default to stdout exporter when no OTLP endpoint configured (same pattern as trace)
- [x] 4.6 Make the OTel log exporter non-blocking — failures must not crash the app
- [x] 4.7 Run `go mod tidy` to resolve new dependencies

## 5. Onboard Settings Endpoints to Logging

- [x] 5.1 Gotify save: add `logService.Log("info", "gotify", "Gotify settings saved", nil)` after successful save
- [x] 5.2 Email save: add log entry after successful save
- [x] 5.3 Email test: add log entry with result (info on success, error on failure)
- [x] 5.4 Ollama save: add log entry after successful save
- [x] 5.5 Immich save: add log entry after successful save
- [x] 5.6 Immich test: add log entry with connection test result
- [x] 5.7 Memories save: add log entry after successful save
- [x] 5.8 Backup config save: add log entry after successful save
- [x] 5.9 Backup creation: add log entry with backup filename

## 6. Admin HTML — Logs Tab

- [x] 6.1 Add "Logs" tab nav item after Backup tab in `admin.html`
- [x] 6.2 Add logs tab-pane div with: verbosity control header, filter row (severity dropdown, source dropdown, search input), log table, pagination controls, clear button
- [x] 6.3 Design responsive filter bar that stacks on mobile
- [x] 6.4 Add severity badges CSS classes and log-related styles to the inline `<style>` block

## 7. Admin JS — Log Viewer Logic

- [x] 7.1 In `ts/admin.ts` (or new module): add `loadLogs()` function fetching `GET /api/logs` with current filters + pagination
- [x] 7.2 Add `loadLogSettings()` fetching `GET /api/logs/settings` to initialize the verbosity dropdown
- [x] 7.3 Add `saveLogSettings(severity)` posting to `POST /api/logs/settings`
- [x] 7.4 Add `clearLogs()` with confirmation calling `DELETE /api/logs`
- [x] 7.5 Add severity badge formatting (debug=gray, info=blue, warn=yellow, error=red)
- [x] 7.6 Add expandable metadata details using inline toggle
- [x] 7.7 Implement 10-second auto-refresh only while the Logs tab is visible
- [x] 7.8 Add filter change handlers (severity, source, search) that re-query
- [x] 7.9 Wire up pagination controls
- [x] 7.10 Initialize log viewer in the `init()` function

## 8. Verify

- [x] 8.1 Run `go build -o traces-server .` to verify Go compilation
- [x] 8.2 Run `npx tsc` to verify TypeScript compilation
- [x] 8.3 Run `go vet ./...` for static analysis
- [x] 8.4 Run `go test -v ./...` to ensure existing tests pass
- [ ] 8.5 Manual smoke test: start server, navigate admin, verify Logs tab renders, verify config saves produce log entries
