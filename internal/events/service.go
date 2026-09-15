package events

import (
	"database/sql"

	"traces/internal/httpx"
	"traces/internal/integrations"
	"traces/internal/logging"
)

type Deps struct {
	DB           *sql.DB
	Log          *logging.LogService
	Renderer     *httpx.Renderer
	Integrations *integrations.Service
}

type Service struct{ Deps }

func New(d Deps) *Service { return &Service{Deps: d} }
