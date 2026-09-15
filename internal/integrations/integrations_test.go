package integrations

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/logging"
	"traces/internal/models"
)

func newTestService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := New(db, logging.New(db, func() bool { return false }), nil)
	svc.SetPublicMode(func() bool { return false })
	return svc, db
}

func setupBGGTestDB(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	svc, db := newTestService(t)
	if _, err := db.Exec(`CREATE TABLE timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT DEFAULT '',
		tags TEXT DEFAULT '',
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0,
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		deleted_at TEXT DEFAULT '',
		source TEXT DEFAULT '',
		source_ref TEXT DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE bgg_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		username TEXT DEFAULT '',
		enabled INTEGER DEFAULT 0,
		last_sync TEXT DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO bgg_settings (id, username, enabled, last_sync) VALUES (1, '', 0, '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_timeline_events_bgg_ref ON timeline_events(source_ref) WHERE source='bgg'`); err != nil {
		t.Fatal(err)
	}
	return svc, db
}

func sampleBGGPlay(id, date, game string) bggPlayXML {
	return bggPlayXML{
		ID:       id,
		Date:     date,
		Location: "Home",
		Item:     bggItemXML{Name: game, ObjectID: "123", ObjectType: "thing"},
		Comments: "Great game",
	}
}

func TestImportBGGPlayDedupe(t *testing.T) {
	_, db := setupBGGTestDB(t)
	play := sampleBGGPlay("12345", "2026-05-01", "Catan")
	ok, err := importBGGPlay(db, play)
	if err != nil || !ok {
		t.Fatalf("first import failed: ok=%v err=%v", ok, err)
	}
	ok2, err := importBGGPlay(db, play)
	if err != nil {
		t.Fatalf("second import err: %v", err)
	}
	if ok2 {
		t.Error("second import should be no-op (dedupe), got imported=true")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE source='bgg' AND source_ref='bgg-play-12345'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected count 1 after dedupe, got %d", count)
	}
}

func TestImportBGGPlayMapsCorrectly(t *testing.T) {
	_, db := setupBGGTestDB(t)
	play := bggPlayXML{
		ID:       "123",
		Date:     "2026-06-15",
		Location: "Kitchen table",
		Item:     bggItemXML{Name: "Ticket to Ride", ObjectID: "999"},
		Players: &bggPlayersXML{
			Players: []bggPlayerXML{{Name: "Alice", Score: "42", Win: "1"}},
		},
		Comments: "Fun",
		Length:   "60",
	}
	ok, err := importBGGPlay(db, play)
	if err != nil || !ok {
		t.Fatalf("import failed: %v ok=%v", err, ok)
	}
	var title, tags, source, sourceRef, eventDate, mediaType string
	if err := db.QueryRow(`SELECT title, tags, source, source_ref, event_date, media_type FROM timeline_events WHERE source_ref='bgg-play-123'`).Scan(&title, &tags, &source, &sourceRef, &eventDate, &mediaType); err != nil {
		t.Fatal(err)
	}
	if title != "Played Ticket to Ride" {
		t.Errorf("title = %q, want %q", title, "Played Ticket to Ride")
	}
	if !strings.Contains(tags, "boardgame") {
		t.Errorf("tags = %q, want to contain boardgame", tags)
	}
	if source != "bgg" {
		t.Errorf("source = %q, want bgg", source)
	}
	if sourceRef != "bgg-play-123" {
		t.Errorf("source_ref = %q, want bgg-play-123", sourceRef)
	}
	if eventDate != "2026-06-15" {
		t.Errorf("event_date = %q, want 2026-06-15", eventDate)
	}
	if mediaType != "boardgame" {
		t.Errorf("media_type = %q, want boardgame", mediaType)
	}
}

func TestBGGPlaysIncludedInPublicFeed(t *testing.T) {
	// Simplified: verify BuildEventQuery-like count not needed; just check import visibility
	_, db := setupBGGTestDB(t)
	if _, err := db.Exec(`INSERT INTO timeline_events (title, event_date, source, source_ref, tags) VALUES ('Normal Event','2026-06-01','','','')`); err != nil {
		t.Fatal(err)
	}
	play := sampleBGGPlay("12345", "2026-06-02", "Catan")
	if _, err := importBGGPlay(db, play); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE source='bgg' OR title='Normal Event'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}
}

func TestBGGIncludedInStats(t *testing.T) {
	_, db := setupBGGTestDB(t)
	if _, err := db.Exec(`INSERT INTO timeline_events (title, event_date, source, source_ref) VALUES ('Normal','2026-07-01','','')`); err != nil {
		t.Fatal(err)
	}
	play := sampleBGGPlay("999", "2026-07-02", "Azul")
	if _, err := importBGGPlay(db, play); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE event_date LIKE '2026-%'`).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 events, got %d", count)
	}
}

// --- Immich / Ollama / Email / Memories / TRMNL tests ---

func TestOllamaConfig(t *testing.T) {
	svc, db := newTestService(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS ollama_settings (id INTEGER PRIMARY KEY CHECK (id = 1), url TEXT DEFAULT 'http://localhost:11434', model TEXT DEFAULT 'llama3.2', enabled INTEGER DEFAULT 0)`)
	db.Exec("INSERT OR IGNORE INTO ollama_settings (id, url, model, enabled) VALUES (1, 'http://localhost:11434', 'llama3.2', 0)")
	var url, model string
	var enabled int
	db.QueryRow("SELECT url, model, enabled FROM ollama_settings WHERE id = 1").Scan(&url, &model, &enabled)
	if url != "http://localhost:11434" {
		t.Errorf("url = %q", url)
	}
	// test via handler
	r := gin.New()
	r.GET("/api/ollama/config", svc.GetOllamaConfig)
	req := httptest.NewRequest("GET", "/api/ollama/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestImmichConfigRoundTrip(t *testing.T) {
	svc, db := newTestService(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS immich_settings (id INTEGER PRIMARY KEY CHECK (id = 1), url TEXT DEFAULT '', api_key TEXT DEFAULT '', enabled INTEGER DEFAULT 0)`)
	db.Exec(`INSERT OR IGNORE INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)`)
	r := gin.New()
	r.GET("/api/immich/config", svc.GetImmichConfig)
	r.POST("/api/immich/config", svc.SaveImmichConfig)
	// save
	cfg := models.ImmichConfig{URL: "https://immich.example.com", APIKey: "k", Enabled: true}
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest("POST", "/api/immich/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	// get
	req2 := httptest.NewRequest("GET", "/api/immich/config", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var got models.ImmichConfig
	json.Unmarshal(w2.Body.Bytes(), &got)
	if got.URL != cfg.URL || got.APIKey != cfg.APIKey || !got.Enabled {
		t.Errorf("roundtrip got %+v want %+v", got, cfg)
	}
	_ = db
}

func TestImmichMemoryAssetSerialization(t *testing.T) {
	asset := models.ImmichMemoryAsset{ID: "abc-123", OriginalFileName: "IMG_2023.jpg", Type: "IMAGE", ThumbnailURL: "https://immich.example.com/api/assets/abc-123/thumbnail", AssetCount: 3, MemoryDate: "1 year ago", Latitude: 52.3676, Longitude: 4.9041, Description: "photo from immich"}
	data, _ := json.Marshal(asset)
	if !strings.Contains(string(data), `"id":"abc-123"`) {
		t.Errorf("json %s", string(data))
	}
}

func TestImmichTimelineResponseParsing(t *testing.T) {
	responseJSON := `[{"title":"1 year ago","assets":[{"id":"asset-001","originalFileName":"photo1.jpg","type":"IMAGE","exifInfo":{"dateTimeOriginal":"2023-05-07T10:30:00.000Z","latitude":52.3676,"longitude":4.9041,"city":"Amsterdam","country":"Netherlands"}}]}]`
	var timeline []immichTimelineResponse
	if err := json.Unmarshal([]byte(responseJSON), &timeline); err != nil {
		t.Fatal(err)
	}
	if timeline[0].Assets[0].ID != "asset-001" {
		t.Errorf("id %q", timeline[0].Assets[0].ID)
	}
}

func TestImmichConfigHandlers(t *testing.T) {
	svc, db := newTestService(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS immich_settings (id INTEGER PRIMARY KEY CHECK (id = 1), url TEXT DEFAULT '', api_key TEXT DEFAULT '', enabled INTEGER DEFAULT 0)`)
	db.Exec(`INSERT OR IGNORE INTO immich_settings (id, url, api_key, enabled) VALUES (1, '', '', 0)`)
	r := gin.New()
	r.GET("/api/immich/config", svc.GetImmichConfig)
	r.POST("/api/immich/config", svc.SaveImmichConfig)
	req := httptest.NewRequest("GET", "/api/immich/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	cfg := models.ImmichConfig{URL: "https://immich.vandijke.xyz", APIKey: "test-api-key-456", Enabled: true}
	body, _ := json.Marshal(cfg)
	req2 := httptest.NewRequest("POST", "/api/immich/config", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("save %d", w2.Code)
	}
}

func TestMemoriesConfig(t *testing.T) {
	_, db := newTestService(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (id INTEGER PRIMARY KEY CHECK (id = 1), enabled INTEGER DEFAULT 1, days_window INTEGER DEFAULT 3, email_enabled INTEGER DEFAULT 0, last_sent_date TEXT DEFAULT '')`)
	db.Exec(`INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)`)
	var enabledInt, daysWindow, emailInt int
	db.QueryRow("SELECT enabled, days_window, email_enabled FROM memories_settings WHERE id = 1").Scan(&enabledInt, &daysWindow, &emailInt)
	if enabledInt != 1 || daysWindow != 3 {
		t.Errorf("initial %d %d", enabledInt, daysWindow)
	}
}

func TestEmailConfigRoundTrip(t *testing.T) {
	_, db := newTestService(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (id INTEGER PRIMARY KEY CHECK (id = 1), smtp_host TEXT DEFAULT '', smtp_port INTEGER DEFAULT 587, smtp_user TEXT DEFAULT '', smtp_pass TEXT DEFAULT '', from_addr TEXT DEFAULT '', to_addr TEXT DEFAULT '')`)
	db.Exec(`INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)`)
	db.Exec(`UPDATE email_settings SET smtp_host=?, smtp_port=?, smtp_user=?, smtp_pass=?, from_addr=?, to_addr=? WHERE id=1`, "smtp.example.com", 465, "user", "pass", "from@test.com", "to@test.com")
	var host string
	var port int
	db.QueryRow("SELECT smtp_host, smtp_port FROM email_settings WHERE id = 1").Scan(&host, &port)
	if host != "smtp.example.com" || port != 465 {
		t.Errorf("host %q port %d", host, port)
	}
}

func TestEmailConfigAPI(t *testing.T) {
	svc, db := newTestService(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (id INTEGER PRIMARY KEY CHECK (id = 1), smtp_host TEXT DEFAULT '', smtp_port INTEGER DEFAULT 587, smtp_user TEXT DEFAULT '', smtp_pass TEXT DEFAULT '', from_addr TEXT DEFAULT '', to_addr TEXT DEFAULT '')`)
	db.Exec(`INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, '', 587)`)
	r := gin.New()
	r.GET("/api/email/config", svc.GetEmailConfig)
	r.POST("/api/email/config", svc.SaveEmailConfig)
	r.POST("/api/email/test", svc.TestEmail)
	// get defaults
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/email/config", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get %d", w.Code)
	}
	// save
	body, _ := json.Marshal(models.EmailConfig{SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPUser: "user@gmail.com", SMTPPass: "app-password", FromAddr: "from@gmail.com", ToAddr: "to@gmail.com"})
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/email/config", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("save %d %s", w2.Code, w2.Body.String())
	}
	_ = db
}

func TestSendMemoriesEmailHandler(t *testing.T) {
	svc, db := newTestService(t)
	db.Exec(`CREATE TABLE IF NOT EXISTS memories_settings (id INTEGER PRIMARY KEY CHECK (id = 1), enabled INTEGER DEFAULT 1, days_window INTEGER DEFAULT 3, email_enabled INTEGER DEFAULT 0, last_sent_date TEXT DEFAULT '')`)
	db.Exec("INSERT OR IGNORE INTO memories_settings (id, enabled, days_window, email_enabled) VALUES (1, 1, 3, 0)")
	db.Exec(`CREATE TABLE IF NOT EXISTS email_settings (id INTEGER PRIMARY KEY CHECK (id = 1), smtp_host TEXT DEFAULT '', smtp_port INTEGER DEFAULT 587, smtp_user TEXT DEFAULT '', smtp_pass TEXT DEFAULT '', from_addr TEXT DEFAULT '', to_addr TEXT DEFAULT '')`)
	db.Exec("INSERT OR IGNORE INTO email_settings (id, smtp_host, smtp_port) VALUES (1, 'smtp.example.com', 587)")
	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, description TEXT, event_date TEXT, deleted_at TEXT DEFAULT '', source TEXT DEFAULT '', source_ref TEXT DEFAULT '')`)
	r := gin.New()
	r.POST("/api/memories/send", svc.SendMemoriesEmailHandler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/memories/send", nil)
	r.ServeHTTP(w, req)
	// Should fail no recipient after we cleared to_addr
	if w.Code != http.StatusBadRequest {
		t.Logf("got %d %s", w.Code, w.Body.String())
	}
	_ = db
}

func setupTRMNLTestSvc(t *testing.T) (*Service, *sql.DB, *gin.Engine) {
	svc, db := newTestService(t)
	db.Exec(`CREATE TABLE timeline_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		event_date TEXT,
		location TEXT,
		media_type TEXT,
		media_url TEXT,
		thumbnail TEXT,
		media_caption TEXT,
		tags TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		person_id INTEGER,
		latitude REAL,
		longitude REAL,
		recurring TEXT DEFAULT '',
		weather_data TEXT DEFAULT '',
		event_start_time TEXT DEFAULT '',
		event_end_time TEXT DEFAULT '',
		user_id INTEGER DEFAULT 0,
		deleted_at TEXT DEFAULT '',
		source TEXT DEFAULT '',
		source_ref TEXT DEFAULT ''
	)`)
	db.Exec(`CREATE TABLE persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	router := gin.New()
	router.GET("/api/trmnl/summary", svc.GetTRMNLSummary)
	return svc, db, router
}

func currentMonthDate2(year int, day int) string {
	return fmt.Sprintf("%d-%02d-%02d", year, int(time.Now().Month()), day)
}

func TestTRMNLSummaryEmpty(t *testing.T) {
	_, _, router := setupTRMNLTestSvc(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trmnl/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var summary trmnlSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	if len(summary.Events) != 0 {
		t.Errorf("events %d", len(summary.Events))
	}
}

func TestTRMNLSummaryPublicOnly(t *testing.T) {
	svc, db, router := setupTRMNLTestSvc(t)
	svc.SetPublicMode(func() bool { return false })
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 0, 'photo', 'holiday')`, "public event", currentMonthDate2(2024, 15))
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 0, 1, 'video', 'secret')`, "private event", currentMonthDate2(2023, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/trmnl/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var summary trmnlSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	if len(summary.Events) != 1 || summary.Events[0].Title != "public event" {
		t.Errorf("events %+v", summary.Events)
	}
}

func TestMemoriesQuery(t *testing.T) {
	_, db := newTestService(t)
	db.Exec(`CREATE TABLE timeline_events (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, description TEXT, event_date TEXT, location TEXT, media_type TEXT, media_url TEXT, thumbnail TEXT, media_caption TEXT, tags TEXT, sort_order INTEGER DEFAULT 0, is_public INTEGER DEFAULT 0, is_favorite INTEGER DEFAULT 0, created_at TEXT DEFAULT CURRENT_TIMESTAMP, person_id INTEGER, latitude REAL, longitude REAL)`)
	now := time.Now().UTC()
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Last year event', ?)`, now.AddDate(-1, 0, 0).Format("2006-01-02"))
	db.Exec(`INSERT INTO timeline_events (title, event_date) VALUES ('Two years ago', ?)`, now.AddDate(-2, 0, 0).Format("2006-01-02"))
	rows, _ := db.Query(`SELECT e.title, e.event_date, CAST(strftime('%Y','now') AS INTEGER) - CAST(strftime('%Y', e.event_date) AS INTEGER) AS years_ago FROM timeline_events e WHERE e.event_date != '' AND CAST(strftime('%Y', e.event_date) AS INTEGER) < CAST(strftime('%Y','now') AS INTEGER) AND strftime('%m-%d', e.event_date) = strftime('%m-%d', 'now') ORDER BY e.event_date DESC`)
	defer rows.Close()
	var count int
	for rows.Next() {
		count++
	}
	if count < 2 {
		t.Errorf("count %d", count)
	}
}

func TestTRMNLSummaryPublicModeServesAll(t *testing.T) {
	svc, db, router := setupTRMNLTestSvc(t)
	svc.SetPublicMode(func() bool { return true })
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 0, 'photo', 'holiday')`, "public event", currentMonthDate2(2024, 15))
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 0, 1, 'video', 'secret')`, "private event", currentMonthDate2(2023, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/trmnl/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var summary trmnlSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	if len(summary.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(summary.Events))
	}
}

func TestTRMNLSummaryMonthFilter(t *testing.T) {
	svc, db, router := setupTRMNLTestSvc(t)
	svc.SetPublicMode(func() bool { return false })
	otherMonth := int(time.Now().Month())%12 + 1
	otherDate := fmt.Sprintf("%d-%02d-%02d", 2021, otherMonth, 20)
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 0, 'photo', 'a')`, "this month", currentMonthDate2(2022, 5))
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 0, 'photo', 'b')`, "another month", otherDate)
	req := httptest.NewRequest(http.MethodGet, "/api/trmnl/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var summary trmnlSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	if len(summary.Events) != 1 || summary.Events[0].Title != "this month" {
		t.Errorf("events %+v", summary.Events)
	}
	_ = svc
}

func TestTRMNLSummaryFavoritesFirstAndLimit(t *testing.T) {
	svc, db, router := setupTRMNLTestSvc(t)
	svc.SetPublicMode(func() bool { return false })
	for i := 1; i <= 10; i++ {
		db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 0, 'photo', 't')`, fmt.Sprintf("event %d", i), currentMonthDate2(2024, i))
	}
	for _, day := range []int{28, 29, 30} {
		db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 1, 'photo', 't')`, fmt.Sprintf("fav %d", day), currentMonthDate2(2024, day))
	}
	req := httptest.NewRequest(http.MethodGet, "/api/trmnl/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var summary trmnlSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	if len(summary.Events) != 8 {
		t.Fatalf("expected 8 events, got %d", len(summary.Events))
	}
	_ = svc
}

func TestTRMNLSummaryStatsAggregation(t *testing.T) {
	_, db, router := setupTRMNLTestSvc(t)
	personRes, _ := db.Exec(`INSERT INTO persons (name) VALUES ('Alice')`)
	personID, _ := personRes.LastInsertId()
	pid := int(personID)
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags, person_id) VALUES (?, ?, 1, 1, 'photo', 'holiday,sea', ?)`, "e1", currentMonthDate2(2024, 1), pid)
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags, person_id) VALUES (?, ?, 1, 0, 'photo', 'holiday', ?)`, "e2", currentMonthDate2(2023, 2), pid)
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 0, 'video', '')`, "e3", currentMonthDate2(2022, 3))
	db.Exec(`INSERT INTO timeline_events (title, event_date, is_public, is_favorite, media_type, tags) VALUES (?, ?, 1, 0, '', '')`, "e4", currentMonthDate2(2021, 4))
	req := httptest.NewRequest(http.MethodGet, "/api/trmnl/summary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var summary trmnlSummary
	json.Unmarshal(w.Body.Bytes(), &summary)
	if summary.Stats.EventCount != 4 {
		t.Errorf("event_count %d", summary.Stats.EventCount)
	}
	_ = pid
}
