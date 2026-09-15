package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/database"
	"traces/internal/httpx"
	"traces/internal/models"
)

func TestRecycleBin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	t.Cleanup(func() {
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	})

	newTestDB(t)

	sessionStore = make(map[string]sessionInfo)
	csrfTokens = make(map[string]string)

	db.Exec(`CREATE TABLE IF NOT EXISTS timeline_events (
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
	db.Exec(`CREATE TABLE IF NOT EXISTS persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		avatar_url TEXT,
		bio TEXT,
		birth_date TEXT,
		color TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	router := gin.New()
	auth := router.Group("")
	auth.Use(func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil || sessionStore[cookie].expiresAt == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		c.Next()
	})
	auth.GET("/api/events", getEvents)
	auth.GET("/api/events/trash", getTrashEvents)
	auth.POST("/api/events/restore", restoreEvents)
	auth.POST("/api/events/empty-trash", emptyTrash)
	auth.POST("/api/events", saveEvent)

	sessionID := "test-trash-session"
	sessionStore[sessionID] = sessionInfo{userID: 0, expiresAt: time.Now().Add(24 * time.Hour).Unix()}

	t.Run("soft_delete_moves_event_to_trash", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO timeline_events (title, description, event_date) VALUES (?, ?, ?)", "Trash Event", "Will be deleted", "2026-07-04")
		if err != nil {
			t.Fatal(err)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '')").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 active event, got %d", count)
		}

		_, err = db.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=1")
		if err != nil {
			t.Fatal(err)
		}

		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '')").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 active events after soft delete, got %d", count)
		}
	})

	t.Run("trash_endpoint_returns_deleted_events", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/events/trash", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/events/trash status = %d", w.Code)
		}

		var events []models.TimelineEvent
		json.Unmarshal(w.Body.Bytes(), &events)
		if len(events) != 1 {
			t.Fatalf("expected 1 trashed event, got %d", len(events))
		}
		if events[0].Title != "Trash Event" {
			t.Errorf("trashed event title = %q", events[0].Title)
		}
		if events[0].DeletedAt == "" {
			t.Error("expected deleted_at to be set")
		}
	})

	t.Run("restore_endpoint_brings_event_back", func(t *testing.T) {
		body := `{"ids":[1]}`
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/events/restore", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/events/restore status = %d", w.Code)
		}

		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("status = %q", resp["status"])
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events WHERE (deleted_at IS NULL OR deleted_at = '')").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 active event after restore, got %d", count)
		}
	})

	t.Run("empty_trash_permanently_deletes", func(t *testing.T) {
		_, err := db.Exec("UPDATE timeline_events SET deleted_at=datetime('now') WHERE id=1")
		if err != nil {
			t.Fatal(err)
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/events/empty-trash", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/events/empty-trash status = %d", w.Code)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 events after empty trash, got %d", count)
		}
	})
}

func TestHTMXEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	origPublicMode := publicMode
	origRenderer := htmxRenderer
	origBasePath := basePath
	tmpDir := t.TempDir()
	t.Cleanup(func() {
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
		publicMode = origPublicMode
		htmxRenderer = origRenderer
		basePath = origBasePath
	})
	basePath = tmpDir

	newTestDB(t)

	database.Migrate(db)
	database.SeedEvents(db, basePath)
	var err error
	htmxRenderer, err = httpx.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	sessionStore = make(map[string]sessionInfo)
	csrfTokens = make(map[string]string)

	r := gin.New()

	auth := r.Group("/api/admin")
	auth.Use(authMiddlewareGin(), csrfMiddleware())
	{
		auth.GET("/events", htmxListEvents)
		auth.POST("/events", htmxSaveEvent)
		auth.DELETE("/events/:id", htmxDeleteEvent)
		auth.GET("/events/:id/edit", htmxEditEventForm)
		auth.GET("/persons", htmxListPersons)
		auth.POST("/persons", htmxSavePerson)
		auth.DELETE("/persons/:id", htmxDeletePerson)
		auth.GET("/tags", htmxListTags)
		auth.GET("/collections", htmxListCollections)
		auth.POST("/collections", htmxSaveCollection)
		auth.GET("/templates", htmxListTemplates)
		auth.POST("/templates", htmxSaveTemplate)
		auth.GET("/users", htmxListUsers)
		auth.POST("/users", htmxSaveUser)
		auth.GET("/trash", htmxListTrash)
		auth.POST("/trash/:id/restore", htmxRestoreEvent)
		auth.POST("/trash/empty", htmxEmptyTrash)
	}

	sessionID := "htmx-test-session"
	csrfToken := "htmx-test-csrf-token"
	sessionStore[sessionID] = sessionInfo{userID: 0, expiresAt: time.Now().Add(24 * time.Hour).Unix()}
	csrfTokens[sessionID] = csrfToken

	t.Run("htmx_events_list_returns_html", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/events", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", contentType)
		}
		if !strings.Contains(w.Body.String(), "tr") && !strings.Contains(w.Body.String(), "No events found") {
			t.Error("response should contain HTML table or message")
		}
	})

	t.Run("htmx_persons_list_returns_html", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/persons", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", contentType)
		}
	})

	t.Run("htmx_tags_list_returns_html", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/tags", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", contentType)
		}
	})

	t.Run("htmx_create_event_via_form", func(t *testing.T) {
		body := "title=HTMX+Test+Event&date=2026-06-15&location=Test&media_type=image&tags=test"
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/admin/events status = %d, body = %s", w.Code, w.Body.String())
		}

		var eventCount int
		db.QueryRow("SELECT COUNT(*) FROM timeline_events").Scan(&eventCount)
		if eventCount < 1 {
			t.Errorf("expected at least 1 event in database, got %d. Body: %s", eventCount, w.Body.String())
		}
	})

	t.Run("htmx_create_person_via_form", func(t *testing.T) {
		body := "name=HTMX+Person&bio=Test+bio&color=%23ff0000"
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/persons", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), "HTMX Person") {
			t.Error("response should contain the new person name")
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM persons WHERE name='HTMX Person'").Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 person, got %d", count)
		}
	})

	t.Run("htmx_create_collection_via_form", func(t *testing.T) {
		body := "name=HTMX+Collection&description=Test+collection&color=%2300ff00"
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/collections", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), "HTMX Collection") {
			t.Error("response should contain the new collection name")
		}
	})

	t.Run("htmx_trash_endpoint_returns_html", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/trash", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", contentType)
		}
	})

	t.Run("htmx_endpoints_reject_unauthorized", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/events", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})
}
