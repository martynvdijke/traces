package auth

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// PrivacySettings is the per-account analytics preference. Umami is the only
// tracker this instance can enable, so opting out covers it entirely.
type PrivacySettings struct {
	AnalyticsOptOut bool `json:"analytics_opt_out"`
}

func (s *Service) currentPrivacyUser(c *gin.Context) (int64, bool) {
	cu := GetCurrentUser(c)
	if cu.ID == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "The legacy administrator account has no privacy preferences"})
		return 0, false
	}
	return cu.ID, true
}

// GetPrivacySettings handles GET /api/me/privacy.
func (s *Service) GetPrivacySettings(c *gin.Context) {
	userID, ok := s.currentPrivacyUser(c)
	if !ok {
		return
	}
	var optOut int
	err := s.db.QueryRow("SELECT COALESCE(analytics_opt_out, 0) FROM users WHERE id = ?", userID).Scan(&optOut)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": ErrUserNotFound.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load privacy settings"})
		return
	}
	c.JSON(http.StatusOK, PrivacySettings{AnalyticsOptOut: optOut == 1})
}

// UpdatePrivacySettings handles PATCH /api/me/privacy.
func (s *Service) UpdatePrivacySettings(c *gin.Context) {
	userID, ok := s.currentPrivacyUser(c)
	if !ok {
		return
	}
	var input PrivacySettings
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	optOut := 0
	if input.AnalyticsOptOut {
		optOut = 1
	}
	result, err := s.db.Exec("UPDATE users SET analytics_opt_out = ? WHERE id = ?", optOut, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save privacy settings"})
		return
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": ErrUserNotFound.Error()})
		return
	}
	c.JSON(http.StatusOK, PrivacySettings{AnalyticsOptOut: optOut == 1})
}
