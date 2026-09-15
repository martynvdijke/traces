package events

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"

	"traces/internal/httpx"
	"traces/internal/models"
)

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func UniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	var r []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			r = append(r, v)
		}
	}
	return r
}

func IsLeapYear(year string) bool {
	y, err := strconv.Atoi(year)
	if err != nil {
		return false
	}
	return (y%4 == 0 && y%100 != 0) || y%400 == 0
}

func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLng := (lng2 - lng1) * (math.Pi / 180.0)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func WeatherCodeToCondition(code int) string {
	switch {
	case code == 0:
		return "Clear sky"
	case code <= 3:
		return "Partly cloudy"
	case code <= 48:
		return "Foggy"
	case code <= 57:
		return "Drizzle"
	case code <= 67:
		return "Rain"
	case code <= 77:
		return "Snow"
	case code <= 82:
		return "Rain showers"
	case code <= 86:
		return "Snow showers"
	default:
		return "Thunderstorm"
	}
}

func WeatherCodeToIcon(code int) string {
	switch {
	case code == 0:
		return "sun"
	case code <= 3:
		return "cloud-sun"
	case code <= 48:
		return "smog"
	case code <= 57:
		return "cloud-rain"
	case code <= 67:
		return "cloud-showers-heavy"
	case code <= 77:
		return "snowflake"
	case code <= 82:
		return "cloud-showers-heavy"
	case code <= 86:
		return "snowflake"
	default:
		return "bolt"
	}
}

func (s *Service) SearchEvents(c *gin.Context) {
	query := c.Query("q")
	filters := EventFilters{
		Year: c.Query("year"), Month: c.Query("month"), Tag: c.Query("tag"),
		Person: c.Query("person"), PersonID: c.Query("person_id"),
		MediaType: c.Query("media_type"), Location: c.Query("location"),
		UserID: c.Query("user_id"),
	}

	sqlStr := BuildEventQueryPrefix()
	args := []any{}

	if query != "" {
		ftsOK := true
		ftsQuery := SanitizeFTSQuery(query)
		var ftsCount int
		if err := s.DB.QueryRow("SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH ?", ftsQuery).Scan(&ftsCount); err != nil {
			ftsOK = false
		}
		if ftsOK && ftsCount > 0 {
			sqlStr += " AND e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)"
			args = append(args, ftsQuery)
		} else {
			filters.Query = query
		}
	}

	sqlStr, args = AppendEventFilters(sqlStr, args, filters)
	sqlStr += " ORDER BY e.event_date ASC"

	rows, err := s.DB.Query(sqlStr, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	evs := ScanEvents(rows)
	c.JSON(http.StatusOK, evs)
}

func (s *Service) GetAutocomplete(c *gin.Context) {
	field := c.Query("field")
	q := c.Query("q")

	var results []string

	switch field {
	case "location":
		rows, err := s.DB.Query(`SELECT DISTINCT location FROM timeline_events WHERE location != '' AND location LIKE ? ORDER BY location LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	case "person":
		rows, err := s.DB.Query(`SELECT name FROM persons WHERE name LIKE ? ORDER BY name LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	case "tag":
		rows, err := s.DB.Query(`SELECT DISTINCT tags FROM timeline_events WHERE tags != '' AND tags LIKE ? ORDER BY tags LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				for t := range strings.SplitSeq(v, ",") {
					t = strings.TrimSpace(t)
					if t != "" && strings.Contains(strings.ToLower(t), strings.ToLower(q)) {
						results = append(results, t)
					}
				}
			}
		}
		results = UniqueStrings(results)
		if len(results) > 10 {
			results = results[:10]
		}
	case "media":
		rows, err := s.DB.Query(`SELECT DISTINCT media_url FROM timeline_events WHERE media_url != '' AND media_url LIKE ? ORDER BY media_url LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	case "user":
		rows, err := s.DB.Query(`SELECT display_name FROM users WHERE display_name LIKE ? ORDER BY display_name LIMIT 10`, "%"+q+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v string
				rows.Scan(&v)
				results = append(results, v)
			}
		}
	}

	c.JSON(http.StatusOK, results)
}

func (s *Service) GlobalSearchEvents(c *gin.Context) {
	query := c.Query("q")
	limit := c.Query("limit")

	if query == "" {
		c.JSON(http.StatusOK, []any{})
		return
	}

	l := 10
	if limit != "" {
		if v, err := strconv.Atoi(limit); err == nil && v > 0 {
			l = v
		}
	}

	sqlStr := BuildEventQueryPrefix()
	args := []any{}

	ftsQuery := SanitizeFTSQuery(query)
	var ftsCount int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH ?", ftsQuery).Scan(&ftsCount); err != nil || ftsCount == 0 {
		sqlStr += " AND (e.title LIKE ? OR e.description LIKE ? OR e.location LIKE ? OR p.name LIKE ?)"
		like := "%" + query + "%"
		args = append(args, like, like, like, like)
	} else {
		sqlStr += " AND e.id IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)"
		args = append(args, ftsQuery)
	}

	sqlStr += " ORDER BY e.event_date DESC LIMIT ?"
	args = append(args, l)

	rows, err := s.DB.Query(sqlStr, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	evs := ScanEvents(rows)
	c.JSON(http.StatusOK, evs)
}

func (s *Service) GetStatsDistribution(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	dist := models.StatsDistribution{
		ByMonth:        make(map[string]int),
		ByWeekday:      make(map[string]int),
		MediaBreakdown: make(map[string]int),
		ByTag:          make([]models.TagCount, 0),
		ByPerson:       make([]models.PersonCount, 0),
		ByUser:         make([]models.UserCount, 0),
		ByLocation:     make([]models.LocationCount, 0),
	}

	s.DB.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", year).Scan(&dist.EventCount)

	daysInYear := 365
	if IsLeapYear(year) {
		daysInYear = 366
	}
	if dist.EventCount > 0 {
		dist.DailyAvg = float64(dist.EventCount) / float64(daysInYear)
		dist.MonthlyAvg = float64(dist.EventCount) / 12.0
	}

	dist.ByMonth = QueryMonthlyCounts(s.DB, year)

	wdCounts := QueryWeekdayCounts(s.DB, year)
	maps.Copy(dist.ByWeekday, wdCounts)

	tagResult := QueryTagFrequency(s.DB, year)
	if tagResult != nil {
		dist.ByTag = tagResult
	}

	dist.ByPerson = QueryPersonEventCounts(s.DB, year)
	dist.ByUser = QueryUserEventCounts(s.DB, year)
	dist.ByLocation = QueryLocationCounts(s.DB, year, 20)
	dist.MediaBreakdown = QueryMediaBreakdown(s.DB, year)
	dist.TopDay = QueryTopDay(s.DB, year)

	if len(dist.ByLocation) >= 2 {
		totalDist := 0.0
		pairs := 0
		for i := 0; i < len(dist.ByLocation); i++ {
			for j := i + 1; j < len(dist.ByLocation); j++ {
				totalDist += Haversine(dist.ByLocation[i].Lat, dist.ByLocation[i].Lng,
					dist.ByLocation[j].Lat, dist.ByLocation[j].Lng)
				pairs++
			}
		}
		if pairs > 0 {
			dist.GeoSpread = totalDist / float64(pairs)
		}
	}

	c.JSON(http.StatusOK, dist)
}

func (s *Service) GetWrapped(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	var total int
	s.DB.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=?", year).Scan(&total)

	var topEventTitle string
	var topEventDate string
	s.DB.QueryRow(`SELECT title, event_date FROM timeline_events 
		WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=? 
		ORDER BY (LENGTH(description) - LENGTH(REPLACE(description, ' ', '')) + 1) DESC LIMIT 1`, year).Scan(&topEventTitle, &topEventDate)

	var longestStreak int
	streakRows, _ := s.DB.Query("SELECT event_date FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=? GROUP BY event_date ORDER BY event_date ASC", year)
	var prevDate time.Time
	currentStreak := 0
	if streakRows != nil {
		for streakRows.Next() {
			var dateStr string
			streakRows.Scan(&dateStr)
			d, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			if !prevDate.IsZero() {
				diff := d.Sub(prevDate).Hours() / 24
				if diff <= 1 {
					currentStreak++
				} else {
					if currentStreak > longestStreak {
						longestStreak = currentStreak
					}
					currentStreak = 1
				}
			} else {
				currentStreak = 1
			}
			prevDate = d
		}
		if currentStreak > longestStreak {
			longestStreak = currentStreak
		}
		streakRows.Close()
	}

	var mostTagsTitle, mostTags string
	s.DB.QueryRow(`SELECT title, tags FROM timeline_events 
		WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=? AND tags != '' 
		ORDER BY (LENGTH(tags) - LENGTH(REPLACE(tags, ',', '')) + 1) DESC LIMIT 1`, year).Scan(&mostTagsTitle, &mostTags)

	tagCount := 0
	if mostTags != "" {
		tagCount = len(strings.Split(mostTags, ","))
	}

	var favCount int
	s.DB.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=? AND is_favorite=1", year).Scan(&favCount)

	var topMonth string
	var topMonthCount int
	s.DB.QueryRow(`SELECT CASE CAST(strftime('%m', event_date) AS INTEGER)
		WHEN 1 THEN 'January' WHEN 2 THEN 'February' WHEN 3 THEN 'March'
		WHEN 4 THEN 'April' WHEN 5 THEN 'May' WHEN 6 THEN 'June'
		WHEN 7 THEN 'July' WHEN 8 THEN 'August' WHEN 9 THEN 'September'
		WHEN 10 THEN 'October' WHEN 11 THEN 'November' WHEN 12 THEN 'December' END,
		COUNT(*) as cnt FROM timeline_events
		WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=?
		GROUP BY strftime('%m', event_date) ORDER BY cnt DESC LIMIT 1`, year).Scan(&topMonth, &topMonthCount)

	var totalMedia int
	s.DB.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=? AND media_url != ''", year).Scan(&totalMedia)

	monthlyRows, _ := s.DB.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events
		WHERE (deleted_at IS NULL OR deleted_at = '') AND strftime('%Y', event_date)=? GROUP BY strftime('%m', event_date)`, year)
	byMonth := make(map[string]int)
	if monthlyRows != nil {
		for monthlyRows.Next() {
			var m string
			var c int
			monthlyRows.Scan(&m, &c)
			byMonth[m] = c
		}
		monthlyRows.Close()
	}

	c.JSON(http.StatusOK, gin.H{
		"year":                year,
		"total_events":        total,
		"top_event":           topEventTitle,
		"top_event_date":      topEventDate,
		"longest_streak":      longestStreak,
		"most_tags_title":     mostTagsTitle,
		"most_tags_count":     tagCount,
		"favorite_count":      favCount,
		"busiest_month":       topMonth,
		"busiest_month_count": topMonthCount,
		"total_media":         totalMedia,
		"by_month":            byMonth,
	})
}

func (s *Service) GetEventStats(c *gin.Context) {
	_, span := httpx.StartSpan(s.tracer(), c, "getEventStats")
	defer span.End()

	year := c.Query("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	span.SetAttributes(attribute.String("year", year))

	stats := QueryYearStats(s.DB, year)
	stats.TotalYears = len(stats.ByYear)

	c.JSON(http.StatusOK, stats)
}

func (s *Service) CreateShareLink(c *gin.Context) {
	var input struct {
		EventIDs []int  `json:"event_ids"`
		Year     string `json:"year"`
		Days     int    `json:"days"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Days == 0 {
		input.Days = 7
	}

	token, err := generateSessionID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	var eventIDsStr strings.Builder
	for idx, idVal := range input.EventIDs {
		if idx > 0 {
			eventIDsStr.WriteString(",")
		}
		eventIDsStr.WriteString(strconv.Itoa(idVal))
	}

	expires := time.Now().Add(time.Duration(input.Days) * 24 * time.Hour)

	_, err = s.DB.Exec(`INSERT INTO share_tokens (token, event_ids, year, expires_at) VALUES (?, ?, ?, ?)`,
		token, eventIDsStr.String(), input.Year, expires.Format("2006-01-02"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create share link"})
		return
	}

	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification("New share link created", fmt.Sprintf("Expires: %s", expires.Format("2006-01-02")))
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires": expires.Format("2006-01-02")})
}

func (s *Service) GetShareLink(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token required"})
		return
	}

	var eventIDs, year string
	var expires string
	err := s.DB.QueryRow("SELECT event_ids, year, expires_at FROM share_tokens WHERE token = ?", token).Scan(&eventIDs, &year, &expires)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid token"})
		return
	}

	expTime, _ := time.Parse("2006-01-02", expires)
	if time.Now().After(expTime) {
		c.JSON(http.StatusGone, gin.H{"error": "Token expired"})
		return
	}

	c.Redirect(http.StatusFound, "/?share="+token)
}

func (s *Service) GetMapData(c *gin.Context) {
	year := c.Query("year")
	query := `SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, latitude, longitude 
		FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '') AND latitude IS NOT NULL AND longitude IS NOT NULL AND latitude != 0 AND longitude != 0`
	args := []any{}

	if year != "" {
		query += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}
	query += " ORDER BY event_date ASC"

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	defer rows.Close()

	type MapFeature struct {
		ID          int     `json:"id"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Date        string  `json:"date"`
		Location    string  `json:"location"`
		MediaType   string  `json:"media_type"`
		MediaURL    string  `json:"media_url"`
		Thumbnail   string  `json:"thumbnail"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}

	var features []MapFeature
	for rows.Next() {
		var f MapFeature
		var lat, lng sql.NullFloat64
		var mediaURL, mediaType, thumbnail sql.NullString
		err := rows.Scan(&f.ID, &f.Title, &f.Description, &f.Date, &f.Location, &mediaType, &mediaURL, &thumbnail, &lat, &lng)
		if err != nil {
			continue
		}
		f.MediaType = mediaType.String
		f.MediaURL = mediaURL.String
		f.Thumbnail = thumbnail.String
		if lat.Valid {
			f.Latitude = lat.Float64
		}
		if lng.Valid {
			f.Longitude = lng.Float64
		}
		features = append(features, f)
	}

	result := gin.H{
		"type":     "FeatureCollection",
		"features": features,
	}
	c.JSON(http.StatusOK, result)
}

func (s *Service) GetCalendar(c *gin.Context) {
	year := c.Query("year")
	month := c.Query("month")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	if month == "" {
		month = fmt.Sprintf("%02d", time.Now().Month())
	}

	y, errY := strconv.Atoi(year)
	m, errM := strconv.Atoi(month)
	if errY != nil || errM != nil || m < 1 || m > 12 || y < 1900 || y > 2100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid year or month"})
		return
	}

	startDate := fmt.Sprintf("%04d-%02d-01", y, m)
	firstDay, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
		return
	}
	lastDay := firstDay.AddDate(0, 1, -1)

	rows, err := s.DB.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, e.media_url, e.thumbnail, e.media_caption, e.tags, e.sort_order, e.is_public, e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude, e.recurring, e.weather_data, e.user_id, e.event_start_time, e.event_end_time,
		p.id, p.name, p.avatar_url, p.bio, p.birth_date, p.color, p.created_at
		FROM timeline_events e LEFT JOIN persons p ON e.person_id = p.id
		WHERE (e.deleted_at IS NULL OR e.deleted_at = '') AND e.event_date BETWEEN ? AND ?
		ORDER BY e.event_date ASC, e.id ASC`, startDate, lastDay.Format("2006-01-02"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}
	defer rows.Close()

	evs := ScanEventsWithPerson(rows)

	daysMap := make(map[string][]models.TimelineEvent)
	for _, e := range evs {
		daysMap[e.Date] = append(daysMap[e.Date], e)
	}

	var calendar []models.CalendarDay
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		dayEvents := daysMap[dateStr]
		sort.Slice(dayEvents, func(i, j int) bool {
			if dayEvents[i].SortOrder != dayEvents[j].SortOrder {
				return dayEvents[i].SortOrder < dayEvents[j].SortOrder
			}
			return dayEvents[i].ID < dayEvents[j].ID
		})
		calendar = append(calendar, models.CalendarDay{
			Date:   dateStr,
			Events: dayEvents,
			Count:  len(dayEvents),
		})
	}

	c.JSON(http.StatusOK, calendar)
}

func (s *Service) FetchWeather(c *gin.Context) {
	var input struct {
		EventID   int     `json:"event_id"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Date      string  `json:"date"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	eventDate, err := time.Parse("2006-01-02", input.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date"})
		return
	}

	var apiURL string
	if time.Since(eventDate) < 16*24*time.Hour {
		apiURL = fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
			input.Latitude, input.Longitude, input.Date, input.Date)
	} else {
		apiURL = fmt.Sprintf("https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,wind_speed_10m_max,relative_humidity_2m_mean&timezone=auto&start_date=%s&end_date=%s",
			input.Latitude, input.Longitude, input.Date, input.Date)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", apiURL, nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to create request"})
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch weather data"})
		return
	}
	defer resp.Body.Close()

	var weatherResp struct {
		Daily struct {
			Time           []string  `json:"time"`
			TemperatureMax []float64 `json:"temperature_2m_max"`
			TemperatureMin []float64 `json:"temperature_2m_min"`
			WeatherCode    []int     `json:"weathercode"`
			WindSpeedMax   []float64 `json:"wind_speed_10m_max"`
			Humidity       []float64 `json:"relative_humidity_2m_mean"`
		} `json:"daily"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&weatherResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to parse weather data"})
		return
	}

	if len(weatherResp.Daily.Time) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No weather data available for this date"})
		return
	}

	weatherCode := 0
	if len(weatherResp.Daily.WeatherCode) > 0 {
		weatherCode = weatherResp.Daily.WeatherCode[0]
	}

	tempMax := 0.0
	if len(weatherResp.Daily.TemperatureMax) > 0 {
		tempMax = weatherResp.Daily.TemperatureMax[0]
	}
	tempMin := 0.0
	if len(weatherResp.Daily.TemperatureMin) > 0 {
		tempMin = weatherResp.Daily.TemperatureMin[0]
	}

	windSpeed := 0.0
	if len(weatherResp.Daily.WindSpeedMax) > 0 {
		windSpeed = weatherResp.Daily.WindSpeedMax[0]
	}

	humidity := 0.0
	if len(weatherResp.Daily.Humidity) > 0 {
		humidity = weatherResp.Daily.Humidity[0]
	}

	condition := WeatherCodeToCondition(weatherCode)
	icon := WeatherCodeToIcon(weatherCode)

	weather := models.WeatherData{
		Temperature: (tempMax + tempMin) / 2,
		Condition:   condition,
		Icon:        icon,
		WindSpeed:   windSpeed,
		Humidity:    humidity,
		FetchedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	weatherJSON, _ := json.Marshal(weather)

	if input.EventID > 0 {
		s.DB.Exec("UPDATE timeline_events SET weather_data = ? WHERE id = ?", string(weatherJSON), input.EventID)
	}

	c.JSON(http.StatusOK, weather)
}
