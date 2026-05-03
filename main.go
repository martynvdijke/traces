package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

type TimelineEvent struct {
	ID           int      `json:"id"`
	Title        string   `json:"title"`
	Description string   `json:"description"`
	Date        string   `json:"date"`
	Location    string   `json:"location"`
	MediaType   string   `json:"media_type"`
	MediaURL    string   `json:"media_url"`
	Thumbnail   string   `json:"thumbnail"`
	MediaCaption string  `json:"media_caption"`
	Tags        string   `json:"tags"`
	People     string   `json:"people"`
	SortOrder   int      `json:"sort_order"`
	IsPublic   bool     `json:"is_public"`
	CreatedAt   string   `json:"created_at"`
}

type EventStats struct {
	Total      int            `json:"total"`
	ByMonth    map[string]int `json:"by_month"`
	ByTag     map[string]int `json:"by_tag"`
	ByMedia   map[string]int `json:"by_media"`
	Locations int           `json:"locations"`
}

type AdminUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Password string `json:"-"`
}

type ShareToken struct {
	Token     string    `json:"token"`
	EventIDs []int     `json:"event_ids"`
	Year      string    `json:"year"`
	Expires   time.Time `json:"expires"`
}

const currentSchemaVersion = 3
const currentVersion = "1.1.0"

var (
	publicMode bool = false
)

var (
	db           *sql.DB
	sessionStore = make(map[string]int64)
	staticCache = make(map[string][]byte)
	upgrader    = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	basePath   = "/app"
	dbPath    = "/db/traces.db"
	mediaPath = "/app/media"
)

func main() {
	if os.Getenv("DOCKER") != "true" {
		basePath = "."
		dbPath = "./traces.db"
		mediaPath = filepath.Join(basePath, "media")
	}

	if err := os.MkdirAll(mediaPath, 0755); err != nil {
		log.Printf("Warning: could not create media directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Printf("Warning: could not create database directory: %v", err)
	}

	compileTypeScript()

	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	initDB()

	http.HandleFunc("/api/login", handleLogin)
	http.HandleFunc("/api/logout", handleLogout)
	http.HandleFunc("/api/check-setup", handleCheckSetup)

	http.HandleFunc("/admin.html", func(w http.ResponseWriter, r *http.Request) {
		var validSession string
		for _, c := range r.Cookies() {
			if c.Name == "session" {
				if _, ok := sessionStore[c.Value]; ok {
					validSession = c.Value
					break
				}
			}
		}

		if validSession == "" {
			http.Redirect(w, r, "/login.html", http.StatusFound)
			return
		}

		http.ServeFile(w, r, filepath.Join(basePath, "static/admin.html"))
	})

	http.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			getEvents(w, r)
		case "POST":
			authMiddleware(saveEvent)(w, r)
		case "DELETE":
			authMiddleware(deleteEvent)(w, r)
		}
	})

	http.HandleFunc("/api/contributions", getContributions)

	http.HandleFunc("/api/upload", authMiddleware(handleUpload))

	http.HandleFunc("/api/events/search", searchEvents)
	http.HandleFunc("/api/events/clone", authMiddleware(cloneEvent))
	http.HandleFunc("/api/events/import", authMiddleware(importEvents))
	http.HandleFunc("/api/events/export", exportEvents)

	http.HandleFunc("/api/stats", getEventStats)

	http.HandleFunc("/api/share/create", authMiddleware(createShareLink))
	http.HandleFunc("/api/share", getShareLink)

	http.HandleFunc("/api/public", getPublicEvents)

	http.HandleFunc("/api/tags", getTags)

	http.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": currentVersion})
	})

	http.HandleFunc("/api-docs", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(basePath, "static/swagger.json"))
	})

	http.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
    <title>TRACES API Documentation</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
        window.onload = function() {
            SwaggerUIBundle({
                url: "/api-docs",
                dom_id: '#swagger-ui',
                presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
                layout: "BaseLayout"
            });
        };
    </script>
</body>
</html>`)
	})

	http.HandleFunc("/login.html", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err == nil {
			expiry, ok := sessionStore[cookie.Value]
			if ok && time.Now().Unix() <= expiry {
				http.Redirect(w, r, "/admin.html", http.StatusFound)
				return
			}
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
		if count == 0 {
			http.Redirect(w, r, "/setup.html", http.StatusFound)
			return
		}
		http.ServeFile(w, r, filepath.Join(basePath, "static/login.html"))
	})

	http.HandleFunc("/setup.html", func(w http.ResponseWriter, r *http.Request) {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
		if count > 0 {
			http.Redirect(w, r, "/login.html", http.StatusFound)
			return
		}
		http.ServeFile(w, r, filepath.Join(basePath, "static/setup.html"))
	})

	fs := http.FileServer(http.Dir(filepath.Join(basePath, "static")))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	mediaFs := http.FileServer(http.Dir(mediaPath))
	http.Handle("/static/media/", http.StripPrefix("/static/media/", mediaFs))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(basePath, "static/index.html"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func initDB() {
	_, _ = db.Exec("CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)")

	var version int
	err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err != nil {
		version = 0
		db.Exec("INSERT INTO schema_version (version) VALUES (0)")
	}

	log.Printf("[DB] Current schema version: %d, target: %d", version, currentSchemaVersion)

	for version < currentSchemaVersion {
		runMigration(version)
		version++
		db.Exec("DELETE FROM schema_version")
		db.Exec("INSERT INTO schema_version (version) VALUES (?)", version)
		log.Printf("[DB] Migrated to schema version %d", version)
	}

	createEventsTable := `
	CREATE TABLE IF NOT EXISTS timeline_events (
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
		people TEXT,
		sort_order INTEGER DEFAULT 0,
		is_public INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	);`

	createAdminTable := `
	CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	);`

	createShareTable := `
	CREATE TABLE IF NOT EXISTS share_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE,
		event_ids TEXT,
		year TEXT,
		expires_at TEXT
	);`

	_, err = db.Exec(createEventsTable)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createAdminTable)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createShareTable)
	if err != nil {
		log.Fatal(err)
	}

	publicMode = os.Getenv("PUBLIC_MODE") == "true"

	seedEvents()
}

func runMigration(fromVersion int) {
	switch fromVersion {
	case 0:
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			description TEXT,
			event_date TEXT,
			location TEXT,
			media_type TEXT,
			media_url TEXT,
			thumbnail TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`)
	case 1:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN media_caption TEXT`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN tags TEXT`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN sort_order INTEGER DEFAULT 0`)
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN is_public INTEGER DEFAULT 0`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS share_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE,
			event_ids TEXT,
			year TEXT,
			expires_at TEXT
		)`)
	case 2:
		_, _ = db.Exec(`ALTER TABLE timeline_events ADD COLUMN people TEXT`)
		_, _ = db.Exec(`UPDATE timeline_events SET people = '' WHERE people IS NULL`)
	}
}

func seedEvents() {
	events := []TimelineEvent{
		{
			Title:        "New Year Celebration",
			Description: "Welcome to the new year with fireworks and festivities!",
			Date:        "2026-01-01",
			Location:   "Times Square, NYC",
			MediaType:  "image",
			MediaURL:   "/static/media/newyear.jpg",
		},
		{
			Title:        "Summer Music Festival",
			Description: "Amazing performances under the stars",
			Date:        "2026-07-15",
			Location:   "Central Park",
			MediaType:  "video",
			MediaURL:   "/static/media/festival.mp4",
		},
	}

	for _, e := range events {
		db.Exec("INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail) VALUES (?, ?, ?, ?, ?, ?, ?)",
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail)
	}
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var sessionCookie *http.Cookie
		for _, c := range r.Cookies() {
			if c.Name == "session" {
				if _, ok := sessionStore[c.Value]; ok {
					sessionCookie = c
					break
				}
			}
		}

		if sessionCookie == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		expiry, ok := sessionStore[sessionCookie.Value]
		if !ok {
			http.Error(w, "Session expired", http.StatusUnauthorized)
			return
		}
		if time.Now().Unix() > expiry {
			delete(sessionStore, sessionCookie.Value)
			http.Error(w, "Session expired", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return fmt.Sprintf("%x", hash)
}

func handleCheckSetup(w http.ResponseWriter, r *http.Request) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	json.NewEncoder(w).Encode(map[string]bool{"setup": count > 0})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Setup    bool   `json:"setup"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)

		if input.Setup && count > 0 {
			http.Error(w, "Setup already completed", http.StatusForbidden)
			return
		}

		if count == 0 {
			hashed := hashPassword(input.Password)
			_, err := db.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", input.Username, hashed)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			sessionID := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d-%s-%d", 1, input.Username, time.Now().Unix()))))
			sessionStore[sessionID] = time.Now().Add(24 * time.Hour).Unix()

			cookie := &http.Cookie{Name: "session", Value: sessionID, HttpOnly: true, Path: "/"}
			http.SetCookie(w, cookie)

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}

		var user AdminUser
		err := db.QueryRow("SELECT id, username, password FROM admin_users WHERE username = ?", input.Username).Scan(&user.ID, &user.Username, &user.Password)
		if err != nil {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		inputHash := hashPassword(input.Password)
		if inputHash != user.Password {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		sessionID := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d-%s-%d", user.ID, input.Username, time.Now().Unix()))))
		sessionStore[sessionID] = time.Now().Add(24 * time.Hour).Unix()

		cookie := &http.Cookie{Name: "session", Value: sessionID, HttpOnly: true, Path: "/"}
		http.SetCookie(w, cookie)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.ServeFile(w, r, filepath.Join(basePath, "static/login.html"))
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		delete(sessionStore, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", MaxAge: -1})
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func getEvents(w http.ResponseWriter, r *http.Request) {
	year := r.URL.Query().Get("year")
	month := r.URL.Query().Get("month")
	limit := r.URL.Query().Get("limit")
	skip := r.URL.Query().Get("skip")
	sort := r.URL.Query().Get("sort")

	log.Printf("[API] getEvents: year=%s, month=%s, limit=%s, skip=%s, sort=%s", year, month, limit, skip, sort)

	var query string
	var args []interface{}

	if year != "" && month != "" {
		query = `SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, COALESCE(people,''), created_at 
				 FROM timeline_events WHERE strftime('%Y', event_date) = ? AND strftime('%m', event_date) = ?`
		args = []interface{}{year, month}
	} else if year != "" {
		query = `SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, COALESCE(people,''), created_at 
				 FROM timeline_events WHERE strftime('%Y', event_date) = ?`
		args = []interface{}{year}
	} else {
		query = `SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, COALESCE(people,''), created_at 
				 FROM timeline_events`
	}

	if sort == "desc" {
		query += " ORDER BY event_date DESC"
	} else if sort == "asc" {
		query += " ORDER BY event_date ASC"
	} else {
		query += " ORDER BY event_date ASC"
	}

	if limit != "" {
		query += " LIMIT " + limit
		if skip != "" {
			query += " OFFSET " + skip
		}
	}

	log.Printf("[API] getEvents query: %s with args: %v", query, args)

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("[API] getEvents query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		var people string
		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &people, &e.CreatedAt)
		if err != nil {
			log.Printf("[API] getEvents scan error: %v", err)
			continue
		}
		e.People = people
		events = append(events, e)
	}

	log.Printf("[API] getEvents returned %d events", len(events))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func getContributions(w http.ResponseWriter, r *http.Request) {
	year := r.URL.Query().Get("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	rows, err := db.Query(`SELECT event_date FROM timeline_events WHERE strftime('%Y', event_date) = ?`, year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	contributions := make(map[string]int)
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err == nil {
			contributions[date]++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contributions)
}

func saveEvent(w http.ResponseWriter, r *http.Request) {
	var e TimelineEvent
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		log.Printf("[EVENT] Failed to decode: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[EVENT] Saving event: ID=%d, Title=%q, Date=%q, Location=%q, MediaType=%q, Tags=%q, People=%q",
		e.ID, e.Title, e.Date, e.Location, e.MediaType, e.Tags, e.People)

	if e.ID == 0 {
		result, err := db.Exec(`INSERT INTO timeline_events 
			(title, description, event_date, location, media_type, media_url, thumbnail, media_caption, tags, people, sort_order, is_public) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.People, e.SortOrder, e.IsPublic)
		if err != nil {
			log.Printf("[EVENT] Insert failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := result.LastInsertId()
		e.ID = int(id)
		log.Printf("[EVENT] Created new event with ID: %d", e.ID)
	} else {
		_, err := db.Exec(`UPDATE timeline_events SET 
			title=?, description=?, event_date=?, location=?, media_type=?, media_url=?, thumbnail=?, media_caption=?, tags=?, people=?, sort_order=?, is_public=? 
			WHERE id=?`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.MediaCaption, e.Tags, e.People, e.SortOrder, e.IsPublic, e.ID)
		if err != nil {
			log.Printf("[EVENT] Update failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[EVENT] Updated event ID=%d", e.ID)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(e)
}

func deleteEvent(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)
	log.Printf("[EVENT] Deleting event ID=%d", id)
	_, err := db.Exec("DELETE FROM timeline_events WHERE id=?", id)
	if err != nil {
		log.Printf("[EVENT] Delete failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[EVENT] Deleted event ID=%d", id)
	w.WriteHeader(http.StatusOK)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	log.Printf("[UPLOAD] Upload request received")
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mediaType := r.FormValue("media_type")
	if mediaType == "" {
		mediaType = "image"
	}

	var formKey string
	switch mediaType {
	case "video":
		formKey = "video"
	case "audio":
		formKey = "audio"
	default:
		formKey = "image"
	}

	file, header, err := r.FormFile(formKey)
	if err != nil {
		log.Printf("[UPLOAD] Failed to get form file: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	log.Printf("[UPLOAD] File received: %s, size: %d", header.Filename, header.Size)

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string][]string{
		"image":  {".jpg", ".jpeg", ".png", ".gif", ".webp"},
		"video":  {".mp4", ".webm", ".mov"},
		"audio":  {".mp3", ".wav", ".ogg"},
	}

	validExt := false
	for _, e := range allowedExts[mediaType] {
		if ext == e {
			validExt = true
			break
		}
	}
	if !validExt {
		log.Printf("[UPLOAD] Invalid file type: %s", ext)
		http.Error(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	filename := fmt.Sprintf("%d%s", time.Now().Unix(), ext)
	uploadPath := filepath.Join(mediaPath, filename)
	log.Printf("[UPLOAD] Saving to: %s", uploadPath)

	out, err := os.Create(uploadPath)
	if err != nil {
		log.Printf("[UPLOAD] Failed to create file: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()

	data, _ := io.ReadAll(file)
	out.Write(data)

	staticCache["/static/media/"+filename] = data

	log.Printf("[UPLOAD] Success! URL: /static/media/%s", filename)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"url":       "/static/media/" + filename,
		"media_type": mediaType,
	})
}

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

func searchEvents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	year := r.URL.Query().Get("year")
	tag := r.URL.Query().Get("tag")

	sql := "SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, COALESCE(media_caption,''), COALESCE(tags,''), COALESCE(people,''), sort_order, is_public, created_at FROM timeline_events WHERE 1=1"
	args := []interface{}{}

	if query != "" {
		sql += " AND (title LIKE ? OR description LIKE ? OR location LIKE ? OR people LIKE ?)"
		like := "%" + query + "%"
		args = append(args, like, like, like, like)
	}
	if year != "" {
		sql += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}
	if tag != "" {
		sql += " AND tags LIKE ?"
		args = append(args, "%"+tag+"%")
	}

	sql += " ORDER BY event_date ASC"

	rows, err := db.Query(sql, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		/* scan with nullable strings */
		var people, mediaCaption, tags string
		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &mediaCaption, &tags, &people, &e.SortOrder, &e.IsPublic, &e.CreatedAt)
		if err != nil {
			log.Printf("[API] searchEvents scan error: %v", err)
			continue
		}
		e.MediaCaption = mediaCaption
		e.Tags = tags
		e.People = people
		events = append(events, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func cloneEvent(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID   int    `json:"id"`
		Date string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var e TimelineEvent
	var people, tags string
	err := db.QueryRow("SELECT title, description, event_date, location, media_type, media_url, thumbnail, tags, people, sort_order FROM timeline_events WHERE id = ?", input.ID).
		Scan(&e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &tags, &people, &e.SortOrder)
	if err != nil {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}
	e.Tags = tags
	e.People = people

	e.Date = input.Date
	e.ID = 0

	_, err = db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, people, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.People, e.SortOrder)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func importEvents(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	var events []TimelineEvent
	if format == "csv" {
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		reader := csv.NewReader(file)
		records, err := reader.ReadAll()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i, record := range records {
			if i == 0 {
				continue
			}
			if len(record) < 4 {
				continue
			}
			e := TimelineEvent{
				Title:        record[0],
				Description: record[1],
				Date:        record[2],
				Location:    record[3],
				MediaType:   "image",
			}
			if len(record) > 4 {
				e.Tags = record[4]
			}
			if len(record) > 5 {
				e.People = record[5]
			}
			events = append(events, e)
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	count := 0
	for _, e := range events {
		_, err := db.Exec(`INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail, tags, people, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.Tags, e.People, e.SortOrder)
		if err == nil {
			count++
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]int{"imported": count})
}

func exportEvents(w http.ResponseWriter, r *http.Request) {
	year := r.URL.Query().Get("year")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	sql := "SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, media_caption, tags, people, sort_order, is_public, created_at FROM timeline_events"
	args := []interface{}{}

	if year != "" {
		sql += " WHERE strftime('%Y', event_date) = ?"
		args = append(args, year)
	}
	sql += " ORDER BY event_date ASC"

	rows, err := db.Query(sql, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		var people, mediaCaption, tags string
		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &mediaCaption, &tags, &people, &e.SortOrder, &e.IsPublic, &e.CreatedAt)
		if err != nil {
			log.Printf("[API] exportEvents scan error: %v", err)
			continue
		}
		e.MediaCaption = mediaCaption
		e.Tags = tags
		e.People = people
		events = append(events, e)
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=events.csv")
		fmt.Fprint(w, "Title,Description,Date,Location,MediaType,Tags,People\n")
		for _, e := range events {
			fmt.Fprintf(w, "%q,%q,%s,%q,%s,%s,%s\n", e.Title, e.Description, e.Date, e.Location, e.MediaType, e.Tags, e.People)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func getEventStats(w http.ResponseWriter, r *http.Request) {
	year := r.URL.Query().Get("year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}

	var stats EventStats
	stats.ByMonth = make(map[string]int)
	stats.ByTag = make(map[string]int)
	stats.ByMedia = make(map[string]int)

	db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE strftime('%Y', event_date) = ?", year).Scan(&stats.Total)
	db.QueryRow("SELECT COUNT(DISTINCT location) FROM timeline_events WHERE strftime('%Y', event_date) = ? AND location != ''", year).Scan(&stats.Locations)

	monthRows, _ := db.Query(`SELECT strftime('%m', event_date), COUNT(*) FROM timeline_events 
		WHERE strftime('%Y', event_date) = ? GROUP BY strftime('%m', event_date)`, year)
	for monthRows.Next() {
		var month string
		var count int
		monthRows.Scan(&month, &count)
		stats.ByMonth[month] = count
	}

	tagRows, _ := db.Query(`SELECT tags, COUNT(*) FROM timeline_events 
		WHERE strftime('%Y', event_date) = ? AND tags != '' GROUP BY tags`, year)
	for tagRows.Next() {
		var tags string
		var count int
		tagRows.Scan(&tags, &count)
		stats.ByTag[tags] = count
	}

	mediaRows, _ := db.Query(`SELECT media_type, COUNT(*) FROM timeline_events 
		WHERE strftime('%Y', event_date) = ? GROUP BY media_type`, year)
	for mediaRows.Next() {
		var media string
		var count int
		mediaRows.Scan(&media, &count)
		stats.ByMedia[media] = count
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func createShareLink(w http.ResponseWriter, r *http.Request) {
	var input struct {
		EventIDs []int `json:"event_ids"`
		Year    string `json:"year"`
		Days    int   `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if input.Days == 0 {
		input.Days = 7
	}

	nowStr := time.Now().Format("2006-01-02T15:04:05")
	token := fmt.Sprintf("%x", sha256.Sum256([]byte(nowStr)))

	eventIDsStr := ""
	for idx, idVal := range input.EventIDs {
		if idx > 0 {
			eventIDsStr += ","
		}
		eventIDsStr += strconv.Itoa(idVal)
	}

	expires := time.Now().Add(time.Duration(input.Days) * 24 * time.Hour)

	_, err := db.Exec(`INSERT INTO share_tokens (token, event_ids, year, expires_at) VALUES (?, ?, ?, ?)`,
		token, eventIDsStr, input.Year, expires.Format("2006-01-02"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"token":   token,
		"expires": expires.Format("2006-01-02"),
	})
}

func getShareLink(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	var eventIDs, year string
	var expires string
	err := db.QueryRow("SELECT event_ids, year, expires_at FROM share_tokens WHERE token = ?", token).Scan(&eventIDs, &year, &expires)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusNotFound)
		return
	}

	expTime, _ := time.Parse("2006-01-02", expires)
	if time.Now().After(expTime) {
		http.Error(w, "Token expired", http.StatusGone)
		return
	}

	http.Redirect(w, r, "/?share="+token, http.StatusFound)
}

func getPublicEvents(w http.ResponseWriter, r *http.Request) {
	shareToken := r.URL.Query().Get("share")
	if shareToken == "" && !publicMode {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	var eventIDs string
	var year string

	if shareToken != "" {
		db.QueryRow("SELECT event_ids, year FROM share_tokens WHERE token = ?", shareToken).Scan(&eventIDs, &year)
	} else {
		year = r.URL.Query().Get("year")
		if year == "" {
			year = fmt.Sprintf("%d", time.Now().Year())
		}
	}

	var query string
	var args []interface{}

	if eventIDs != "" {
		query = "SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, COALESCE(media_caption,''), COALESCE(tags,''), COALESCE(people,''), sort_order, is_public, created_at FROM timeline_events WHERE id IN (" + eventIDs + ") ORDER BY event_date ASC"
	} else {
		query = "SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, COALESCE(media_caption,''), COALESCE(tags,''), COALESCE(people,''), sort_order, is_public, created_at FROM timeline_events WHERE is_public = 1"
		if year != "" {
			query += " AND strftime('%Y', event_date) = ?"
			args = append(args, year)
		}
		query += " ORDER BY event_date ASC"
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var events []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		var people, mediaCaption, tags string
		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &mediaCaption, &tags, &people, &e.SortOrder, &e.IsPublic, &e.CreatedAt)
		if err != nil {
			log.Printf("[API] getPublicEvents scan error: %v", err)
			continue
		}
		e.MediaCaption = mediaCaption
		e.Tags = tags
		e.People = people
		events = append(events, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func getTags(w http.ResponseWriter, r *http.Request) {
	year := r.URL.Query().Get("year")
	query := "SELECT DISTINCT tags FROM timeline_events WHERE tags != ''"
	args := []interface{}{}

	if year != "" {
		query += " AND strftime('%Y', event_date) = ?"
		args = append(args, year)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tags = append(tags, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

func compileTypeScript() {
	tsFile := filepath.Join(basePath, "static", "app.ts")
	jsFile := filepath.Join(basePath, "static", "app.js")

	tsInfo, err := os.Stat(tsFile)
	if err != nil {
		log.Printf("[TS] No app.ts found, skipping TypeScript compilation")
		return
	}

	jsInfo, err := os.Stat(jsFile)
	needsCompile := true

	if err == nil && jsInfo.ModTime().After(tsInfo.ModTime()) {
		needsCompile = false
	}

	if !needsCompile {
		log.Printf("[TS] app.js is up to date, skipping TypeScript compilation")
		return
	}

	log.Printf("[TS] Compiling TypeScript...")

	cmd := exec.Command("npx", "tsc")
	cmd.Dir = basePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Printf("[TS] TypeScript compilation failed: %v", err)
		return
	}

	log.Printf("[TS] TypeScript compiled successfully")
}