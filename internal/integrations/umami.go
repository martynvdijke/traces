package integrations

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetUmamiConfig(c *gin.Context) {
	var cfg models.UmamiConfig
	var enabledInt int
	err := s.db.QueryRow("SELECT url, site_id, enabled FROM umami_settings WHERE id = 1").Scan(&cfg.URL, &cfg.SiteID, &enabledInt)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

func (s *Service) SaveUmamiConfig(c *gin.Context) {
	var cfg models.UmamiConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}
	_, err := s.db.Exec(`UPDATE umami_settings SET url=?, site_id=?, enabled=? WHERE id=1`, cfg.URL, cfg.SiteID, enabledInt)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	s.umamiURL = cfg.URL
	s.umamiSiteID = cfg.SiteID
	s.umamiEnabled = cfg.Enabled
	if s.log != nil {
		s.log.Log("info", "umami", "Umami settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
