package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	authpkg "traces/internal/auth"
	"traces/internal/events"
	"traces/internal/integrations"
	"traces/internal/media"
	"traces/internal/models"
	"traces/internal/web"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": models.CurrentVersion})
	})
	r.POST("/api/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	if authSvc != nil {
		r.POST("/api/logout", authSvc.HandleLogout)
	} else {
		r.POST("/api/logout", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	}
	if webSvc != nil {
		r.GET("/api/health", webSvc.Health)
	} else {
		r.GET("/api/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok", "version": models.CurrentVersion})
		})
	}

	return r
}

func doJSON(router http.Handler, method, target, body string, cookies ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, ck := range cookies {
		req.Header.Set("Cookie", ck)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	origDB := db
	origSvc := integrationsSvc
	origEvents := eventsSvc
	origAuthSvc := authSvc
	origAuthSessions := authSessions
	origWebSvc := webSvc
	newDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db = newDB
	integrationsSvc = integrations.New(newDB, logService, nil)
	eventsSvc = events.New(events.Deps{DB: newDB, Log: logService, Renderer: htmxRenderer, Integrations: integrationsSvc, Media: mediaSvc, PublicMode: func() bool { return publicMode }, Tracer: currentTracer})
	authSessions = authpkg.NewSessionStore()
	authSvc = authpkg.New(authpkg.Deps{DB: newDB, Log: logService, Renderer: htmxRenderer, Sessions: authSessions, Integrations: integrationsSvc, PublicMode: func() bool { return publicMode }})
	webSvc = web.New(web.Deps{DB: newDB, Log: logService, Sessions: authSessions, BasePath: basePath, DBPath: dbPath, BackupPath: backupPath, Umami: integrationsSvc.UmamiSettings, OIDCReady: authSvc.OIDCReady})
	t.Cleanup(func() {
		db = origDB
		integrationsSvc = origSvc
		eventsSvc = origEvents
		authSvc = origAuthSvc
		authSessions = origAuthSessions
		webSvc = origWebSvc
		newDB.Close()
	})
	return newDB
}

func newTestEventsSvc() *events.Service {
	return events.New(events.Deps{DB: db, Log: logService, Renderer: htmxRenderer, Integrations: integrationsSvc, Media: mediaSvc, PublicMode: func() bool { return publicMode }, Tracer: currentTracer})
}

func ensureEventsSvc(t *testing.T) *events.Service {
	t.Helper()
	orig := eventsSvc
	eventsSvc = newTestEventsSvc()
	t.Cleanup(func() { eventsSvc = orig })
	return eventsSvc
}

// setupTestMediaSvc points mediaPath at a temp dir and wires the composition
// root's mediaSvc, restoring both on cleanup.
func setupTestMediaSvc(t *testing.T) {
	t.Helper()
	origPath, origSvc := mediaPath, mediaSvc
	mediaPath = t.TempDir()
	mediaSvc = media.New(media.LoadConfig(mediaPath))
	t.Cleanup(func() {
		mediaPath = origPath
		mediaSvc = origSvc
	})
}
