package integrations

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetOllamaConfig(c *gin.Context) {
	var cfg models.OllamaConfig
	var enabledInt int
	err := s.db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&cfg.URL, &cfg.Model, &enabledInt)
	if err != nil {
		c.JSON(http.StatusOK, models.OllamaConfig{URL: "http://localhost:11434", Model: "llama3.2", Enabled: false})
		return
	}
	cfg.Enabled = enabledInt == 1
	c.JSON(http.StatusOK, cfg)
}

func (s *Service) SaveOllamaConfig(c *gin.Context) {
	var cfg models.OllamaConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}
	if cfg.URL == "" {
		cfg.URL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "llama3.2"
	}
	_, err := s.db.Exec(`UPDATE ollama_settings SET url=?, model=?, enabled=? WHERE id=1`, cfg.URL, cfg.Model, enabledInt)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	if s.log != nil {
		s.log.Log("info", "ollama", "Ollama settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) AutoTagEvent(c *gin.Context) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Location    string `json:"location"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(input.Title) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title too long"})
		return
	}
	if len(input.Description) > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Description too long"})
		return
	}
	var ollamaURL, ollamaModel string
	var enabledInt int
	err := s.db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&ollamaURL, &ollamaModel, &enabledInt)
	if err != nil || enabledInt == 0 {
		ollamaURL = os.Getenv("OLLAMA_URL")
		if ollamaURL == "" {
			ollamaURL = "http://localhost:11434"
		}
		ollamaModel = os.Getenv("OLLAMA_MODEL")
		if ollamaModel == "" {
			ollamaModel = "llama3.2"
		}
	}
	sanitizePrompt := func(s string) string {
		s = strings.ReplaceAll(s, "\n", " ")
		s = strings.ReplaceAll(s, "\r", " ")
		if len(s) > 200 {
			s = s[:200]
		}
		return s
	}
	prompt := "Suggest 3-5 single-word tags for this event (comma-separated):\nTitle: " + sanitizePrompt(input.Title) + "\nDescription: " + sanitizePrompt(input.Description) + "\nLocation: " + sanitizePrompt(input.Location) + "\nTags:"
	reqBody, _ := json.Marshal(map[string]any{
		"model":  ollamaModel,
		"prompt": prompt,
		"stream": false,
	})
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", ollamaURL+"/api/generate", bytes.NewReader(reqBody))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Ollama"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Ollama"})
		return
	}
	defer resp.Body.Close()
	var ollamaResp struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to parse Ollama response"})
		return
	}
	tags := strings.Split(ollamaResp.Response, ",")
	cleanTags := make([]string, 0)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "- ")
		t = strings.TrimPrefix(t, "* ")
		if t != "" && !strings.HasPrefix(t, "Tags:") && len(t) < 50 {
			cleanTags = append(cleanTags, t)
		}
		if len(cleanTags) >= 10 {
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"tags": cleanTags})
}
