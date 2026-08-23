package main

import (
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"traces/internal/models"
)

// getBGGConfig returns current BGG settings.
func getBGGConfig(c *gin.Context) {
	var cfg models.BGGConfig
	var enabledInt int
	err := db.QueryRow("SELECT username, enabled, last_sync FROM bgg_settings WHERE id = 1").Scan(&cfg.Username, &enabledInt, &cfg.LastSync)
	if err == nil {
		cfg.Enabled = enabledInt == 1
	}
	c.JSON(http.StatusOK, cfg)
}

// saveBGGConfig persists BGG settings and updates in-memory globals.
func saveBGGConfig(c *gin.Context) {
	var cfg models.BGGConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabledInt := 0
	if cfg.Enabled {
		enabledInt = 1
	}
	_, err := db.Exec(`UPDATE bgg_settings SET username=?, enabled=? WHERE id=1`, cfg.Username, enabledInt)
	if err != nil {
		serverError(c, err)
		return
	}
	bggUsername = cfg.Username
	bggEnabled = cfg.Enabled
	if logService != nil {
		logService.Log("info", "bgg", "BGG settings saved", map[string]interface{}{"username": cfg.Username})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// testBGG checks that the configured username resolves against BGG XMLAPI2.
func testBGG(c *gin.Context) {
	username := bggUsername
	// Allow ad-hoc test with body username if settings not yet saved
	if username == "" {
		var body struct {
			Username string `json:"username"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Username != "" {
			username = body.Username
		}
	}
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BGG username not configured"})
		return
	}
	u := fmt.Sprintf("https://boardgamegeek.com/xmlapi2/plays?username=%s&page=1", url.QueryEscape(username))
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to connect to BGG: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 202 {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "BGG accepted request (202) — try again in a moment"})
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("BGG returned %d: %s", resp.StatusCode, string(body))})
		return
	}
	// Try to parse minimal structure to ensure valid XML
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var plays bggPlaysXML
	if err := xml.Unmarshal(body, &plays); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "BGG returned invalid XML: " + err.Error()})
		return
	}
	if logService != nil {
		logService.Log("info", "bgg", "BGG connection test successful", map[string]interface{}{"username": username})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": fmt.Sprintf("Connected to BGG (%s) — %d plays on first page", username, len(plays.Plays))})
}

// ---- BGG XML structures ----

type bggPlaysXML struct {
	XMLName xml.Name     `xml:"plays"`
	Total   int          `xml:"total,attr"`
	Page    int          `xml:"page,attr"`
	Plays   []bggPlayXML `xml:"play"`
}

type bggPlayXML struct {
	ID       string         `xml:"id,attr"`
	Date     string         `xml:"date,attr"`
	Quantity string         `xml:"quantity,attr"`
	Length   string         `xml:"length,attr"`
	Location string         `xml:"location,attr"`
	Item     bggItemXML     `xml:"item"`
	Players  *bggPlayersXML `xml:"players"`
	Comments string         `xml:"comments"`
}

type bggItemXML struct {
	Name       string `xml:"name,attr"`
	ObjectID   string `xml:"objectid,attr"`
	ObjectType string `xml:"objecttype,attr"`
}

type bggPlayersXML struct {
	Players []bggPlayerXML `xml:"player"`
}

type bggPlayerXML struct {
	Username string `xml:"username,attr"`
	Name     string `xml:"name,attr"`
	Score    string `xml:"score,attr"`
	Win      string `xml:"win,attr"`
}

// ---- Sync ----

const bggPerPage = 100

func syncBGGHandler(c *gin.Context) {
	if bggUsername == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BGG username not configured"})
		return
	}
	result, err := syncBGGPlays(db, bggUsername)
	if err != nil {
		log.Printf("[BGG] sync failed: %v", err)
		if logService != nil {
			logService.Log("error", "bgg", "BGG sync failed: "+err.Error(), nil)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	// Update last_sync
	now := time.Now().Format(time.RFC3339)
	_, _ = db.Exec(`UPDATE bgg_settings SET last_sync=? WHERE id=1`, now)
	bggLastSync = now
	if logService != nil {
		logService.Log("info", "bgg", fmt.Sprintf("BGG sync completed: %d imported, %d skipped", result.Imported, result.Skipped), nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "imported": result.Imported, "skipped": result.Skipped, "total": result.Total, "message": fmt.Sprintf("Imported %d new plays (%d already present)", result.Imported, result.Skipped)})
}

type bggSyncResult struct {
	Imported int
	Skipped  int
	Total    int
}

func syncBGGPlays(db *sql.DB, username string) (bggSyncResult, error) {
	var res bggSyncResult
	client := &http.Client{Timeout: 15 * time.Second}
	page := 1
	for {
		plays, statusCode, err := fetchBGGPlaysPage(client, username, page)
		if err != nil {
			return res, err
		}
		if statusCode == 202 {
			// BGG throttling — wait and retry same page
			time.Sleep(3 * time.Second)
			plays, statusCode, err = fetchBGGPlaysPage(client, username, page)
			if err != nil {
				return res, err
			}
			if statusCode == 202 {
				return res, fmt.Errorf("BGG still throttling (202) — try sync again shortly")
			}
		}
		if statusCode < 200 || statusCode >= 300 {
			return res, fmt.Errorf("BGG returned %d on page %d", statusCode, page)
		}
		if len(plays) == 0 {
			break
		}
		for _, p := range plays {
			res.Total++
			imported, err := importBGGPlay(db, p)
			if err != nil {
				// Log but continue — skip corrupt row
				log.Printf("[BGG] import play %s failed: %v", p.ID, err)
				continue
			}
			if imported {
				res.Imported++
			} else {
				res.Skipped++
			}
		}
		if len(plays) < bggPerPage {
			break
		}
		page++
		time.Sleep(1 * time.Second)
	}
	return res, nil
}

func fetchBGGPlaysPage(client *http.Client, username string, page int) ([]bggPlayXML, int, error) {
	u := fmt.Sprintf("https://boardgamegeek.com/xmlapi2/plays?username=%s&page=%d", url.QueryEscape(username), page)
	resp, err := client.Get(u)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch BGG page %d: %w", page, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 202 {
		return nil, 202, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, resp.StatusCode, fmt.Errorf("BGG page %d returned %d: %s", page, resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	var parsed bggPlaysXML
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parse BGG XML page %d: %w", page, err)
	}
	return parsed.Plays, resp.StatusCode, nil
}

func importBGGPlay(db *sql.DB, p bggPlayXML) (bool, error) {
	if p.ID == "" {
		return false, fmt.Errorf("missing play id")
	}
	sourceRef := "bgg-play-" + p.ID
	// Check existing
	var exists int
	err := db.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE source='bgg' AND source_ref=?`, sourceRef).Scan(&exists)
	if err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	title := "Played " + p.Item.Name
	if p.Item.Name == "" {
		title = "Played board game"
	}
	// Date fallback to today if empty or invalid
	eventDate := strings.TrimSpace(p.Date)
	if eventDate == "" {
		eventDate = time.Now().Format("2006-01-02")
	}
	location := strings.TrimSpace(p.Location)
	// Build description: players summary + comments + duration
	descParts := []string{}
	if p.Players != nil && len(p.Players.Players) > 0 {
		parts := []string{}
		for _, pl := range p.Players.Players {
			name := pl.Name
			if name == "" {
				name = pl.Username
			}
			if name == "" {
				name = "Player"
			}
			bit := name
			if pl.Score != "" {
				bit += " (" + pl.Score + ")"
			}
			if pl.Win == "1" {
				bit += " ★"
			}
			parts = append(parts, bit)
		}
		descParts = append(descParts, "Players: "+strings.Join(parts, ", "))
	}
	if strings.TrimSpace(p.Comments) != "" {
		descParts = append(descParts, strings.TrimSpace(p.Comments))
	}
	if p.Length != "" && p.Length != "0" {
		descParts = append(descParts, "Duration: "+p.Length+" min")
	}
	description := strings.Join(descParts, "\n")
	tags := "boardgame"
	if p.Item.Name != "" {
		// also tag with slug? keep simple boardgame
	}

	_, err = db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, tags, source, source_ref) VALUES (?, ?, ?, ?, ?, 'bgg', ?)`,
		title, description, eventDate, location, tags, sourceRef)
	if err != nil {
		// Unique constraint violation means race — treat as skipped
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// getBGGEvents returns BGG-sourced events grouped for corner view (used by API).
func getBGGEvents(c *gin.Context) {
	rows, err := db.Query(`SELECT e.id, e.title, e.description, e.event_date, e.location, e.media_type, COALESCE(e.media_url,''), COALESCE(e.thumbnail,''), COALESCE(e.media_caption,''), COALESCE(e.tags,''), COALESCE(e.source,''), COALESCE(e.source_ref,''), e.is_favorite, e.created_at, e.person_id, e.latitude, e.longitude FROM timeline_events e WHERE e.source='bgg' AND (e.deleted_at IS NULL OR e.deleted_at='') ORDER BY e.event_date DESC`)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()
	events := []models.TimelineEvent{}
	for rows.Next() {
		var ev models.TimelineEvent
		var mediaURL, thumb, caption, tags, source, sourceRef sql.NullString
		var favInt int
		var personID sql.NullInt64
		var lat, lng sql.NullFloat64
		var createdAt sql.NullString
		var mediaType sql.NullString
		if err := rows.Scan(&ev.ID, &ev.Title, &ev.Description, &ev.Date, &ev.Location, &mediaType, &mediaURL, &thumb, &caption, &tags, &source, &sourceRef, &favInt, &createdAt, &personID, &lat, &lng); err != nil {
			continue
		}
		if mediaType.Valid {
			ev.MediaType = mediaType.String
		}
		if mediaURL.Valid {
			ev.MediaURL = mediaURL.String
		}
		if thumb.Valid {
			ev.Thumbnail = thumb.String
		}
		if caption.Valid {
			ev.MediaCaption = caption.String
		}
		if tags.Valid {
			ev.Tags = tags.String
		}
		if source.Valid {
			ev.Source = source.String
		}
		if sourceRef.Valid {
			ev.SourceRef = sourceRef.String
		}
		if createdAt.Valid {
			ev.CreatedAt = createdAt.String
		}
		if personID.Valid {
			v := int(personID.Int64)
			ev.PersonID = &v
		}
		if lat.Valid {
			v := lat.Float64
			ev.Latitude = &v
		}
		if lng.Valid {
			v := lng.Float64
			ev.Longitude = &v
		}
		ev.IsFavorite = favInt == 1
		events = append(events, ev)
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// getBGGStats returns per-year counts and per-game tally for corner.
func getBGGStats(c *gin.Context) {
	type gameCount struct {
		Game  string `json:"game"`
		Count int    `json:"count"`
	}
	type yearCount struct {
		Year  string `json:"year"`
		Count int    `json:"count"`
	}
	rows, err := db.Query(`SELECT event_date, title FROM timeline_events WHERE source='bgg' AND (deleted_at IS NULL OR deleted_at='')`)
	if err != nil {
		serverError(c, err)
		return
	}
	defer rows.Close()
	byYear := map[string]int{}
	byGame := map[string]int{}
	for rows.Next() {
		var date, title string
		rows.Scan(&date, &title) //nolint
		year := ""
		if len(date) >= 4 {
			year = date[:4]
		}
		if year != "" {
			byYear[year]++
		}
		game := strings.TrimPrefix(title, "Played ")
		if game != "" {
			byGame[game]++
		}
	}
	years := []yearCount{}
	for y, cnt := range byYear {
		years = append(years, yearCount{Year: y, Count: cnt})
	}
	games := []gameCount{}
	for g, cnt := range byGame {
		games = append(games, gameCount{Game: g, Count: cnt})
	}
	c.JSON(http.StatusOK, gin.H{"by_year": byYear, "by_game": byGame, "years": years, "games": games})
}

// seedBGGForTest inserts a canned BGG event for E2E. Guarded to E2E/test environments.
func seedBGGForTest(c *gin.Context) {
	if os.Getenv("E2E_BGG_SEED") == "" && os.Getenv("CI") == "" && os.Getenv("PLAYWRIGHT") == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var body struct {
		Title    string `json:"title"`
		Game     string `json:"game"`
		Date     string `json:"date"`
		Location string `json:"location"`
		Ref      string `json:"ref"`
	}
	_ = c.ShouldBindJSON(&body)
	game := body.Game
	if game == "" {
		game = "Catan"
	}
	title := body.Title
	if title == "" {
		title = "Played " + game
	}
	date := body.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	ref := body.Ref
	if ref == "" {
		ref = fmt.Sprintf("bgg-play-e2e-%d", time.Now().UnixNano())
	}
	location := body.Location
	res, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, tags, source, source_ref) VALUES (?, ?, ?, ?, 'boardgame', 'bgg', ?)`,
		title, "Players: E2E Tester\nSeeded for BGG corner E2E", date, location, ref)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": id, "title": title, "source_ref": ref, "date": date})
}
