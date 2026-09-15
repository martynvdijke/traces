package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

// GetGotifyConfig returns current Gotify settings.
func (s *Service) GetGotifyConfig(c *gin.Context) {
	var cfg models.GotifyConfig
	var enabledInt int
	err := s.db.QueryRow("SELECT url, token, enabled FROM gotify_settings WHERE id = 1").Scan(&cfg.URL, &cfg.Token, &enabledInt)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

// SaveGotifyConfig persists Gotify settings and updates in-memory state.
func (s *Service) SaveGotifyConfig(c *gin.Context) {
	var cfg models.GotifyConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}
	_, err := s.db.Exec(`UPDATE gotify_settings SET url=?, token=?, enabled=? WHERE id=1`, cfg.URL, cfg.Token, enabledInt)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	s.gotifyURL = cfg.URL
	s.gotifyToken = cfg.Token
	s.gotifyEnabled = cfg.Enabled
	if s.log != nil {
		s.log.Log("info", "gotify", "Gotify settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// TestGotify checks Gotify connectivity.
func (s *Service) TestGotify(c *gin.Context) {
	if s.gotifyURL == "" || s.gotifyToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Gotify URL and token not configured"})
		return
	}
	body := fmt.Sprintf(`{"title":"TRACES Test","message":"This is a test notification from TRACES","priority":5}`)
	req, err := http.NewRequest("POST", s.gotifyURL+"/message", strings.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", s.gotifyToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Gotify"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Notification sent successfully"})
	} else {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Gotify returned %d", resp.StatusCode)})
	}
}

// SendGotifyNotification sends a Gotify notification async (no-op when disabled).
func (s *Service) SendGotifyNotification(title, message string) {
	if s == nil || !s.gotifyEnabled || s.gotifyURL == "" || s.gotifyToken == "" {
		return
	}
	payload := map[string]any{
		"title":    title,
		"message":  message,
		"priority": 5,
	}
	body, _ := json.Marshal(payload)
	url := strings.TrimSuffix(s.gotifyURL, "/") + "/message"
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			log.Printf("[GOTIFY] Notification failed: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Gotify-Key", s.gotifyToken)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[GOTIFY] Notification failed: %v", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			log.Printf("[GOTIFY] Notification failed with status: %d", resp.StatusCode)
		} else {
			log.Printf("[GOTIFY] Notification sent: %s", title)
		}
	}()
}
