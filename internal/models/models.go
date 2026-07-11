package models

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/yuin/goldmark"
)

// Domain types

type TimelineEvent struct {
	ID           int      `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Date         string   `json:"date"`
	Location     string   `json:"location"`
	MediaType    string   `json:"media_type"`
	MediaURL     string   `json:"media_url"`
	Thumbnail    string   `json:"thumbnail"`
	MediaCaption string   `json:"media_caption"`
	Tags         string   `json:"tags"`
	SortOrder    int      `json:"sort_order"`
	IsPublic     bool     `json:"is_public"`
	IsFavorite   bool     `json:"is_favorite"`
	CreatedAt    string   `json:"created_at"`
	PersonID     *int     `json:"person_id"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	Person       *Person  `json:"person,omitempty"`
	Recurring    string   `json:"recurring"`
	WeatherData  string   `json:"weather_data"`
	StartTime    string   `json:"start_time"`
	EndTime      string   `json:"end_time"`
	UserID       int      `json:"user_id"`
	User         *User    `json:"user,omitempty"`
	DeletedAt    string   `json:"deleted_at"`
}

type EventStats struct {
	Total        int            `json:"total"`
	ByMonth      map[string]int `json:"by_month"`
	ByTag        map[string]int `json:"by_tag"`
	ByMedia      map[string]int `json:"by_media"`
	Locations    int            `json:"locations"`
	Persons      int            `json:"persons"`
	YearOverYear map[string]int `json:"year_over_year"`
	MediaTotal   int            `json:"media_total"`
	WithLocation int            `json:"with_location"`
	PersonCount  int            `json:"person_count"`
	TotalYears   int            `json:"total_years"`
	WithMedia    int            `json:"with_media"`
	WithGeo      int            `json:"with_geo"`
	ByYear       map[string]int `json:"by_year"`
}

type AdminUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Password string `json:"-"`
}

type Person struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url"`
	Bio        string `json:"bio"`
	BirthDate  string `json:"birth_date"`
	Color      string `json:"color"`
	EventCount int    `json:"event_count,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type User struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Color       string `json:"color"`
	AvatarURL   string `json:"avatar_url"`
	EventCount  int    `json:"event_count,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type GotifyConfig struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
}

type MemoriesConfig struct {
	Enabled      bool `json:"enabled"`
	DaysWindow   int  `json:"days_window"`
	EmailEnabled bool `json:"email_enabled"`
}

type EmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPUser string `json:"smtp_user"`
	SMTPPass string `json:"smtp_pass"`
	FromAddr string `json:"from_addr"`
	ToAddr   string `json:"to_addr"`
}

type OllamaConfig struct {
	URL     string `json:"url"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type ImmichConfig struct {
	URL     string `json:"url"`
	APIKey  string `json:"api_key"`
	Enabled bool   `json:"enabled"`
}

type UmamiConfig struct {
	URL     string `json:"url"`
	SiteID  string `json:"site_id"`
	Enabled bool   `json:"enabled"`
}

type BackupConfig struct {
	RetentionDays int  `json:"retention_days"`
	AutoPrune     bool `json:"auto_prune"`
}

type OtelConfig struct {
	Endpoint       string `json:"endpoint"`
	TracesEnabled  bool   `json:"traces_enabled"`
	MetricsEnabled bool   `json:"metrics_enabled"`
	LogsEnabled    bool   `json:"logs_enabled"`
}

type EventTemplate struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	PersonID    *int   `json:"person_id"`
	UserID      int    `json:"user_id"`
	Location    string `json:"location"`
	MediaType   string `json:"media_type"`
	CreatedAt   string `json:"created_at"`
}

type Collection struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	EventCount  int    `json:"event_count,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type ImmichMemoryAsset struct {
	ID               string  `json:"id"`
	OriginalFileName string  `json:"originalFileName"`
	Type             string  `json:"type"`
	ThumbnailURL     string  `json:"thumbnail_url"`
	AssetCount       int     `json:"asset_count"`
	MemoryDate       string  `json:"memory_date"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	Description      string  `json:"description"`
}

type WeatherData struct {
	Temperature float64 `json:"temperature"`
	Condition   string  `json:"condition"`
	Icon        string  `json:"icon"`
	Humidity    float64 `json:"humidity"`
	WindSpeed   float64 `json:"wind_speed"`
	FetchedAt   string  `json:"fetched_at"`
}

type CalendarDay struct {
	Date   string          `json:"date"`
	Events []TimelineEvent `json:"events"`
	Count  int             `json:"count"`
}

// Constants
const (
	DefaultColor         = "#7c3aed"
	CurrentSchemaVersion = 19
	CurrentVersion       = "1.22.0"
)

// Helper functions

var mdRenderer = goldmark.New()

func EscapeHtml(text string) string {
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	text = strings.ReplaceAll(text, `"`, "&quot;")
	text = strings.ReplaceAll(text, "'", "&#039;")
	return text
}

func RenderMarkdown(text string) string {
	if text == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(text), &buf); err != nil {
		return EscapeHtml(text)
	}
	return buf.String()
}

func GetMediaIcon(mediaType string) string {
	switch mediaType {
	case "video":
		return "fa-solid fa-video"
	case "audio":
		return "fa-solid fa-music"
	default:
		return "fa-solid fa-image"
	}
}

func FormatDate(dateStr string) string {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return date.Format("Jan 2")
}

func GenerateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
