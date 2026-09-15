package integrations

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetImmichConfig(c *gin.Context) {
	var cfg models.ImmichConfig
	var enabledInt int
	err := s.db.QueryRow("SELECT url, api_key, enabled FROM immich_settings WHERE id = 1").Scan(&cfg.URL, &cfg.APIKey, &enabledInt)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

func (s *Service) SaveImmichConfig(c *gin.Context) {
	var cfg models.ImmichConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}
	_, err := s.db.Exec(`UPDATE immich_settings SET url=?, api_key=?, enabled=? WHERE id=1`, cfg.URL, cfg.APIKey, enabledInt)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	s.immichURL = cfg.URL
	s.immichAPIKey = cfg.APIKey
	s.immichEnabled = cfg.Enabled
	if s.log != nil {
		s.log.Log("info", "immich", "Immich settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) TestImmich(c *gin.Context) {
	if s.immichURL == "" || s.immichAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Immich URL and API key not configured"})
		return
	}
	req, err := http.NewRequest("GET", strings.TrimRight(s.immichURL, "/")+"/api/server-info/about", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}
	req.Header.Set("x-api-key", s.immichAPIKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to Immich: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if s.log != nil {
			s.log.Log("info", "immich", "Immich connection test successful", nil)
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Connected to Immich successfully"})
	} else {
		body, _ := io.ReadAll(resp.Body)
		if s.log != nil {
			s.log.Log("error", "immich", "Immich connection test failed: "+string(body), nil)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Immich returned %d: %s", resp.StatusCode, string(body))})
	}
}

type immichTimelineResponse struct {
	Title  string        `json:"title"`
	Assets []immichAsset `json:"assets"`
}

type immichAsset struct {
	ID               string      `json:"id"`
	OriginalFileName string      `json:"originalFileName"`
	Type             string      `json:"type"`
	ExifInfo         *immichExif `json:"exifInfo"`
}

type immichExif struct {
	DateTimeOriginal *string  `json:"dateTimeOriginal"`
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	City             *string  `json:"city"`
	Country          *string  `json:"country"`
}

func (s *Service) FetchImmichMemories(c *gin.Context) {
	if s.immichURL == "" || s.immichAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Immich not configured"})
		return
	}
	req, err := http.NewRequest("GET", strings.TrimRight(s.immichURL, "/")+"/api/timeline/memory", nil)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	req.Header.Set("x-api-key", s.immichAPIKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch memories from Immich: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Immich returned %d: %s", resp.StatusCode, string(body))})
		return
	}
	var timeline []immichTimelineResponse
	if err := json.NewDecoder(resp.Body).Decode(&timeline); err != nil {
		httpx.ServerError(c, err)
		return
	}
	memories := make([]models.ImmichMemoryAsset, 0)
	for _, group := range timeline {
		for _, asset := range group.Assets {
			lat := 0.0
			lng := 0.0
			if asset.ExifInfo != nil {
				if asset.ExifInfo.Latitude != nil {
					lat = *asset.ExifInfo.Latitude
				}
				if asset.ExifInfo.Longitude != nil {
					lng = *asset.ExifInfo.Longitude
				}
			}
			memories = append(memories, models.ImmichMemoryAsset{
				ID:               asset.ID,
				OriginalFileName: asset.OriginalFileName,
				Type:             asset.Type,
				ThumbnailURL:     strings.TrimRight(s.immichURL, "/") + "/api/assets/" + asset.ID + "/thumbnail",
				AssetCount:       len(group.Assets),
				MemoryDate:       group.Title,
				Latitude:         lat,
				Longitude:        lng,
				Description:      asset.OriginalFileName,
			})
		}
	}
	c.JSON(http.StatusOK, memories)
}

func (s *Service) ImportImmichMemories(c *gin.Context) {
	var assetIDs []string
	if err := c.ShouldBindJSON(&assetIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if s.immichURL == "" || s.immichAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Immich not configured"})
		return
	}
	if len(assetIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No assets selected"})
		return
	}
	today := time.Now().Format("2006-01-02")
	count := 0
	for _, assetID := range assetIDs {
		req, err := http.NewRequest("GET", strings.TrimRight(s.immichURL, "/")+"/api/assets/"+assetID, nil)
		if err != nil {
			continue
		}
		req.Header.Set("x-api-key", s.immichAPIKey)
		req.Header.Set("Accept", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var asset immichAsset
		if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		mediaType := "image"
		if asset.Type == "VIDEO" {
			mediaType = "video"
		} else if asset.Type == "AUDIO" {
			mediaType = "audio"
		}
		var lat, lng *float64
		location := ""
		eventDate := today
		if asset.ExifInfo != nil {
			if asset.ExifInfo.Latitude != nil {
				v := *asset.ExifInfo.Latitude
				lat = &v
			}
			if asset.ExifInfo.Longitude != nil {
				v := *asset.ExifInfo.Longitude
				lng = &v
			}
			if asset.ExifInfo.City != nil && *asset.ExifInfo.City != "" {
				location = *asset.ExifInfo.City
				if asset.ExifInfo.Country != nil && *asset.ExifInfo.Country != "" {
					location += ", " + *asset.ExifInfo.Country
				}
			}
			if asset.ExifInfo.DateTimeOriginal != nil && *asset.ExifInfo.DateTimeOriginal != "" {
				if t, err := time.Parse(time.RFC3339, *asset.ExifInfo.DateTimeOriginal); err == nil {
					eventDate = t.Format("2006-01-02")
				}
			}
		}
		thumbnailURL := strings.TrimRight(s.immichURL, "/") + "/api/assets/" + asset.ID + "/thumbnail"
		mediaURL := strings.TrimRight(s.immichURL, "/") + "/api/assets/" + asset.ID + "/original"
		_, err = s.db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, sort_order, latitude, longitude, recurring, weather_data, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			asset.OriginalFileName, asset.OriginalFileName, eventDate, location, mediaType, mediaURL, thumbnailURL, "immich-import", 0, lat, lng, "", "", 0)
		if err == nil {
			count++
		}
	}
	s.SendGotifyNotification(fmt.Sprintf("Imported %d memories from Immich", count), "")
	c.JSON(http.StatusOK, gin.H{"imported": count, "message": fmt.Sprintf("Successfully imported %d memories from Immich", count)})
}
