package events

import (
	"database/sql"

	"go.opentelemetry.io/otel/trace"

	"traces/internal/httpx"
	"traces/internal/integrations"
	"traces/internal/logging"
	"traces/internal/media"
)

type Deps struct {
	DB           *sql.DB
	Log          *logging.LogService
	Renderer     *httpx.Renderer
	Integrations *integrations.Service
	Media        *media.Media
	PublicMode   func() bool
	Tracer       func() trace.Tracer
}

type Service struct{ Deps }

func New(d Deps) *Service { return &Service{Deps: d} }

// tracer returns the configured tracer or nil.
func (s *Service) tracer() trace.Tracer {
	if s.Deps.Tracer != nil {
		return s.Deps.Tracer()
	}
	return nil
}

func (s *Service) isPublicMode() bool {
	if s.Deps.PublicMode != nil {
		return s.Deps.PublicMode()
	}
	return false
}
