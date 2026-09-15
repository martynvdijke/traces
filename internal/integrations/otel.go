package integrations

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetOtelConfig(c *gin.Context) {
	var cfg models.OtelConfig
	var tEnabled, mEnabled, lEnabled int
	err := s.db.QueryRow("SELECT endpoint, traces_enabled, metrics_enabled, logs_enabled FROM otel_settings WHERE id = 1").Scan(&cfg.Endpoint, &tEnabled, &mEnabled, &lEnabled)
	if err == nil {
		cfg.TracesEnabled = tEnabled == 1
		cfg.MetricsEnabled = mEnabled == 1
		cfg.LogsEnabled = lEnabled == 1
	}
	c.JSON(http.StatusOK, cfg)
}

func (s *Service) SaveOtelConfig(c *gin.Context) {
	var cfg models.OtelConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tEnabled := 0
	if cfg.TracesEnabled {
		tEnabled = 1
	}
	mEnabled := 0
	if cfg.MetricsEnabled {
		mEnabled = 1
	}
	lEnabled := 0
	if cfg.LogsEnabled {
		lEnabled = 1
	}
	_, err := s.db.Exec(`UPDATE otel_settings SET endpoint=?, traces_enabled=?, metrics_enabled=?, logs_enabled=? WHERE id=1`,
		cfg.Endpoint, tEnabled, mEnabled, lEnabled)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	s.otelEndpoint = cfg.Endpoint
	s.otelTracesEnabled = cfg.TracesEnabled
	s.otelMetricsEnabled = cfg.MetricsEnabled
	s.otelLogsEnabled = cfg.LogsEnabled
	if s.onOtelConfigChange != nil {
		s.onOtelConfigChange(cfg.TracesEnabled, cfg.MetricsEnabled, cfg.LogsEnabled)
	}
	if s.log != nil {
		s.log.Log("info", "otel", "OTel settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
