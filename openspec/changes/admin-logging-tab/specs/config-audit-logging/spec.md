## ADDED Requirements

### Requirement: Backend log service
The Go backend SHALL provide a logging service that creates structured log entries stored in a SQLite `app_logs` table.

#### Scenario: Log entry creation
- **WHEN** any code calls the log service with severity, source, message, and optional metadata
- **THEN** a new row SHALL be inserted into the `app_logs` table with: auto-increment ID, ISO 8601 timestamp, severity, source, message, and JSON metadata
- **WHEN** the `app_logs` table exceeds 10,000 rows
- **THEN** the oldest rows SHALL be deleted to maintain the cap

### Requirement: Log level filtering on write
The logging service SHALL respect the configured minimum severity level — entries below the threshold MUST NOT be written to the database.

#### Scenario: Filter debug entries
- **WHEN** the minimum severity is set to `warn`
- **THEN** a `debug` or `info` log entry SHALL NOT be written to the database

#### Scenario: Allow warn and above
- **WHEN** the minimum severity is set to `warn`
- **THEN** a `warn` or `error` log entry SHALL be written to the database

### Requirement: Log settings persistence
The application SHALL store log verbosity settings in a `log_settings` table with a minimum severity level.

#### Scenario: Default settings
- **WHEN** the application starts for the first time
- **THEN** a `log_settings` row SHALL be created with `min_severity = "warn"`

#### Scenario: Save verbosity level
- **WHEN** an admin changes the verbosity level via `POST /api/logs/settings`
- **THEN** the `log_settings` table SHALL be updated with the new `min_severity`

#### Scenario: Read verbosity level
- **WHEN** `GET /api/logs/settings` is called
- **THEN** the current log settings SHALL be returned as JSON

### Requirement: REST API for log queries
The backend SHALL provide REST API endpoints for log retrieval and management.

#### Scenario: List logs with filters
- **WHEN** `GET /api/logs` is called
- **THEN** log entries SHALL be returned as a JSON array
- **Query parameters**:
  - `severity` — filter by minimum severity (debug/info/warn/error)
  - `source` — filter by source name
  - `q` — search in message text
  - `limit` — max entries to return (default 50, max 200)
  - `offset` — pagination offset
  - `since` — ISO 8601 timestamp, only entries after this time

#### Scenario: Delete all logs
- **WHEN** `DELETE /api/logs` is called
- **THEN** all rows in the `app_logs` table SHALL be deleted

#### Scenario: Count logs
- **WHEN** `GET /api/logs/count` is called
- **THEN** the total number of log entries SHALL be returned

### Requirement: Config save handlers emit logs
All settings save handlers SHALL emit a structured log entry on successful save.

#### Scenario: Gotify config save logs
- **WHEN** `POST /api/gotify/config` succeeds
- **THEN** a log entry SHALL be created with source "gotify", info severity, message "Gotify settings saved"

#### Scenario: Email config save logs
- **WHEN** `POST /api/email/config` succeeds
- **THEN** a log entry SHALL be created with source "email", info severity, message "Email settings saved"

#### Scenario: Email test logs
- **WHEN** `POST /api/email/test` is called (success or failure)
- **THEN** a log entry SHALL be created with source "email", appropriate severity, message describing the result

#### Scenario: Ollama config save logs
- **WHEN** `POST /api/ollama/config` succeeds
- **THEN** a log entry SHALL be created with source "ollama", info severity, message "Ollama settings saved"

#### Scenario: Immich config save logs
- **WHEN** `POST /api/immich/config` succeeds
- **THEN** a log entry SHALL be created with source "immich", info severity, message "Immich settings saved"

#### Scenario: Memories config save logs
- **WHEN** `POST /api/memories/config` succeeds
- **THEN** a log entry SHALL be created with source "memories", info severity, message "Memories settings saved"

#### Scenario: Backup config save logs
- **WHEN** `POST /api/backup/config` succeeds
- **THEN** a log entry SHALL be created with source "backup", info severity, message "Backup settings saved"

#### Scenario: Backup creation logs
- **WHEN** `POST /api/backup` succeeds
- **THEN** a log entry SHALL be created with source "backup", info severity, message including the backup filename

#### Scenario: Immich test logs
- **WHEN** `POST /api/immich/test` completes
- **THEN** a log entry SHALL be created with source "immich", severity based on result, message describing connection test result
