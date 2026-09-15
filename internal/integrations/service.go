package integrations

import (
	"database/sql"

	"go.opentelemetry.io/otel/trace"

	"traces/internal/logging"
)

// Service owns all provider integrations and their mutable configuration.
type Service struct {
	db  *sql.DB
	log *logging.LogService

	gotifyURL     string
	gotifyToken   string
	gotifyEnabled bool

	immichURL     string
	immichAPIKey  string
	immichEnabled bool

	umamiURL     string
	umamiSiteID  string
	umamiEnabled bool

	bggUsername string
	bggEnabled  bool
	bggLastSync string

	otelEndpoint       string
	otelTracesEnabled  bool
	otelMetricsEnabled bool
	otelLogsEnabled    bool

	tracer     trace.Tracer
	publicMode func() bool

	onOtelConfigChange func(traces, metrics, logs bool)
}

// New creates a Service. onOtelConfigChange is called when OTEL settings are saved
// so the composition root can update its globals that feed logging/telemetry.
func New(db *sql.DB, log *logging.LogService, onOtelConfigChange func(traces, metrics, logs bool)) *Service {
	return &Service{db: db, log: log, onOtelConfigChange: onOtelConfigChange}
}

// SetGotify seeds gotify state (from env / DB at startup).
func (s *Service) SetGotify(url, token string, enabled bool) {
	s.gotifyURL = url
	s.gotifyToken = token
	s.gotifyEnabled = enabled
}

// SetImmich seeds immich state.
func (s *Service) SetImmich(url, apiKey string, enabled bool) {
	s.immichURL = url
	s.immichAPIKey = apiKey
	s.immichEnabled = enabled
}

// SetUmami seeds umami state.
func (s *Service) SetUmami(url, siteID string, enabled bool) {
	s.umamiURL = url
	s.umamiSiteID = siteID
	s.umamiEnabled = enabled
}

// SetBGG seeds BGG state.
func (s *Service) SetBGG(username, lastSync string, enabled bool) {
	s.bggUsername = username
	s.bggEnabled = enabled
	s.bggLastSync = lastSync
}

// SetOtel seeds OTEL state.
func (s *Service) SetOtel(endpoint string, traces, metrics, logs bool) {
	s.otelEndpoint = endpoint
	s.otelTracesEnabled = traces
	s.otelMetricsEnabled = metrics
	s.otelLogsEnabled = logs
}

// SetTracer sets the OTEL tracer used for spans inside integrations.
func (s *Service) SetTracer(t trace.Tracer) { s.tracer = t }

// SetPublicMode sets a callback that reports whether public mode is enabled.
func (s *Service) SetPublicMode(fn func() bool) { s.publicMode = fn }

// UmamiSettings returns umami config for getPublicConfig callers in main.
func (s *Service) UmamiSettings() (url, siteID string, enabled bool) {
	return s.umamiURL, s.umamiSiteID, s.umamiEnabled
}

// currentTracer returns s.tracer (may be nil).
func (s *Service) currentTracer() trace.Tracer { return s.tracer }
