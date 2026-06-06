## ADDED Requirements

### Requirement: OpenTelemetry log exporter
The application SHALL export log entries via OpenTelemetry using the OTLP protocol alongside existing trace export.

#### Scenario: Log export on startup
- **WHEN** the application starts
- **THEN** an OTel log exporter SHALL be initialized (gRPC or HTTP based on `OTEL_EXPORTER_OTLP_PROTOCOL` env var, defaulting to gRPC)
- **THEN** log entries SHALL be sent to the OTel collector endpoint configured via standard OTel environment variables (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`, etc.)

#### Scenario: Each log entry exported
- **WHEN** a structured log entry is created
- **THEN** it SHALL be exported as an OTel LogRecord with:
  - Timestamp
  - Severity mapped to OTel severity numbers
  - Body set to the message string
  - Attributes: `service.name`, `log.source`, `log.severity`, `log.metadata` (from JSON metadata field)

### Requirement: Non-blocking log export
Log export via OTel SHALL be non-blocking — failure to export MUST NOT affect application operation.

#### Scenario: OTel export failure
- **WHEN** the OTel exporter is unreachable or returns an error
- **THEN** the application SHALL continue running normally
- **THEN** the error SHALL be logged locally via `log.Printf`
- **THEN** existing SQLite log storage SHALL remain unaffected

### Requirement: Configurable via environment variables
The OTel log exporter SHALL respect standard OpenTelemetry environment variables for configuration.

#### Scenario: Default configuration
- **WHEN** no OTel env vars are set
- **THEN** the log exporter SHALL default to stdout (same as existing trace exporter)

#### Scenario: Custom endpoint
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is set to `http://otel-collector:4318`
- **THEN** logs SHALL be exported via HTTP to that endpoint
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is set to `otel-collector:4317`
- **THEN** logs SHALL be exported via gRPC to that endpoint

### Requirement: Resource attributes applied to logs
The OTel resource attributes SHALL be applied to exported log records.

#### Scenario: Service name attribute
- **WHEN** a log entry is exported
- **THEN** the `service.name` attribute SHALL be set (from `OTEL_SERVICE_NAME` env var, defaulting to "traces")
