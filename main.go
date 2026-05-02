package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

type TimelineEvent struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Description string `json:"description"`
	Date        string `json:"date"`
	Location    string `json:"location"`
	MediaType   string `json:"media_type"` // "image", "video", "audio"
	MediaURL    string `json:"media_url"`
	Thumbnail  string `json:"thumbnail"`
	CreatedAt   string `json:"created_at"`
}

type AdminUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Password string `json:"-"`
}

const currentSchemaVersion = 1
const currentVersion = "1.0.0"

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
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	);`

	createAdminTable := `
	CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	);`

	_, err = db.Exec(createEventsTable)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createAdminTable)
	if err != nil {
		log.Fatal(err)
	}

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

	var query string
	var args []interface{}

	if year != "" && month != "" {
		query = `SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, created_at 
				 FROM timeline_events WHERE strftime('%Y', event_date) = ? AND strftime('%m', event_date) = ? ORDER BY event_date ASC`
		args = []interface{}{year, month}
	} else if year != "" {
		query = `SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, created_at 
				 FROM timeline_events WHERE strftime('%Y', event_date) = ? ORDER BY event_date ASC`
		args = []interface{}{year}
	} else {
		query = `SELECT id, title, description, event_date, location, media_type, media_url, thumbnail, created_at 
				 FROM timeline_events ORDER BY event_date ASC`
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
		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Date, &e.Location, &e.MediaType, &e.MediaURL, &e.Thumbnail, &e.CreatedAt)
		if err != nil {
			continue
		}
		events = append(events, e)
	}

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

	log.Printf("[EVENT] Saving event: ID=%d, Title=%s, Date=%s, MediaType=%s",
		e.ID, e.Title, e.Date, e.MediaType)

	if e.ID == 0 {
		_, err := db.Exec("INSERT INTO timeline_events (title, description, event_date, location, media_type, media_url, thumbnail) VALUES (?, ?, ?, ?, ?, ?, ?)",
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail)
		if err != nil {
			log.Printf("[EVENT] Insert failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[EVENT] Created new event")
	} else {
		_, err := db.Exec("UPDATE timeline_events SET title=?, description=?, event_date=?, location=?, media_type=?, media_url=?, thumbnail=? WHERE id=?",
			e.Title, e.Description, e.Date, e.Location, e.MediaType, e.MediaURL, e.Thumbnail, e.ID)
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