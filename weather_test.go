package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/events"
)

func TestWeatherDataStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		event_date TEXT,
		weather_data TEXT DEFAULT ''
	)`)

	t.Run("store_and_retrieve_weather", func(t *testing.T) {
		weatherJSON := `{"temperature":22.5,"condition":"Partly cloudy","icon":"cloud-sun","humidity":65,"wind_speed":12.3}`
		_, err := db.Exec("INSERT INTO timeline_events (title, event_date, weather_data) VALUES (?, ?, ?)", "Weather Event", "2026-06-15", weatherJSON)
		if err != nil {
			t.Fatal(err)
		}

		var data string
		db.QueryRow("SELECT weather_data FROM timeline_events WHERE id = 1").Scan(&data)
		if data != weatherJSON {
			t.Errorf("weather_data mismatch")
		}

		var parsed struct {
			Temperature float64 `json:"temperature"`
			Condition   string  `json:"condition"`
		}
		json.Unmarshal([]byte(data), &parsed)
		if parsed.Temperature != 22.5 {
			t.Errorf("temperature = %f", parsed.Temperature)
		}
		if parsed.Condition != "Partly cloudy" {
			t.Errorf("condition = %q", parsed.Condition)
		}
	})
}

func TestWeatherCodeMapping(t *testing.T) {
	tests := []struct {
		code      int
		condition string
		icon      string
	}{
		{0, "Clear sky", "sun"},
		{1, "Partly cloudy", "cloud-sun"},
		{3, "Partly cloudy", "cloud-sun"},
		{45, "Foggy", "smog"},
		{51, "Drizzle", "cloud-rain"},
		{61, "Rain", "cloud-showers-heavy"},
		{71, "Snow", "snowflake"},
		{80, "Rain showers", "cloud-showers-heavy"},
		{85, "Snow showers", "snowflake"},
		{95, "Thunderstorm", "bolt"},
		{99, "Thunderstorm", "bolt"},
	}
	for _, tt := range tests {
		if got := events.WeatherCodeToCondition(tt.code); got != tt.condition {
			t.Errorf("WeatherCodeToCondition(%d) = %q, want %q", tt.code, got, tt.condition)
		}
		if got := events.WeatherCodeToIcon(tt.code); got != tt.icon {
			t.Errorf("WeatherCodeToIcon(%d) = %q, want %q", tt.code, got, tt.icon)
		}
	}
}

func TestWeatherResponseParsing(t *testing.T) {
	// Verify the response struct can parse Open-Meteo's daily format with
	// the corrected field name relative_humidity_2m_mean
	respJSON := `{
		"daily": {
			"time": ["2026-05-12"],
			"temperature_2m_max": [15.0],
			"temperature_2m_min": [8.0],
			"weathercode": [3],
			"wind_speed_10m_max": [12.5],
			"relative_humidity_2m_mean": [72]
		}
	}`
	var parsed struct {
		Daily struct {
			Time           []string  `json:"time"`
			TemperatureMax []float64 `json:"temperature_2m_max"`
			TemperatureMin []float64 `json:"temperature_2m_min"`
			WeatherCode    []int     `json:"weathercode"`
			WindSpeedMax   []float64 `json:"wind_speed_10m_max"`
			Humidity       []float64 `json:"relative_humidity_2m_mean"`
		} `json:"daily"`
	}
	if err := json.Unmarshal([]byte(respJSON), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Daily.Humidity) == 0 {
		t.Fatal("Humidity array is empty — json tag mismatch")
	}
	if parsed.Daily.Humidity[0] != 72 {
		t.Errorf("humidity = %f, want 72", parsed.Daily.Humidity[0])
	}
	if parsed.Daily.WeatherCode[0] != 3 {
		t.Errorf("weatherCode = %d, want 3", parsed.Daily.WeatherCode[0])
	}
}

func TestWeatherURLContainsCorrectParameters(t *testing.T) {
	// Verify the forecast and archive URLs use the corrected parameter names
	lat := 52.52
	lng := 13.41
	date := "2026-05-12"

	forecastURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
		lat, lng, date, date)
	archiveURL := fmt.Sprintf("https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
		lat, lng, date, date)

	if !strings.Contains(forecastURL, "relative_humidity_2m_mean") {
		t.Error("forecast URL missing relative_humidity_2m_mean")
	}
	if !strings.Contains(forecastURL, "weathercode") {
		t.Error("forecast URL missing weathercode")
	}
	if !strings.Contains(archiveURL, "relative_humidity_2m_mean") {
		t.Error("archive URL missing relative_humidity_2m_mean")
	}
	if !strings.Contains(archiveURL, "weathercode") {
		t.Error("archive URL missing weathercode")
	}
	if strings.Contains(forecastURL, "relative_humidity_2m&") {
		t.Error("forecast URL still uses old relative_humidity_2m parameter")
	}
	if strings.Contains(archiveURL, "relative_humidity_2m&") {
		t.Error("archive URL still uses old relative_humidity_2m parameter")
	}
}

func TestFetchWeatherValidatesInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newTestDB(t)

	t.Run("missing_fields_returns_400", func(t *testing.T) {
		svc := ensureEventsSvc(t)
		r := gin.New()
		r.POST("/api/weather/fetch", svc.FetchWeather)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/weather/fetch", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid_date_returns_400", func(t *testing.T) {
		svc := ensureEventsSvc(t)
		r := gin.New()
		r.POST("/api/weather/fetch", svc.FetchWeather)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/weather/fetch", strings.NewReader(`{"latitude":52.52,"longitude":13.41,"date":"bad-date"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}
