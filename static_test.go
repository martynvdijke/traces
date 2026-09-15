package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/web"
)

func TestTypeScriptBuildOutput(t *testing.T) {
	if _, err := os.Stat("static/js/index.js"); os.IsNotExist(err) {
		t.Skip("compiled JS not found; run 'npm run build:ts' first")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()

	basePath := "."
	router.Static("/static", filepath.Join(basePath, "static"))
	sw := web.New(web.Deps{}).ServiceWorker
	if webSvc != nil {
		sw = webSvc.ServiceWorker
	}
	router.GET("/sw.js", sw)

	jsFiles := []string{
		"/static/js/index.js",
		"/static/js/admin.js",
		"/static/js/login.js",
		"/static/js/setup.js",
		"/static/js/map.js",
	}

	for _, file := range jsFiles {
		t.Run("serves_"+file, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", file, nil)
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("GET %s returned status %d, want 200", file, w.Code)
			}
			if len(w.Body.Bytes()) == 0 {
				t.Errorf("GET %s returned empty body", file)
			}
		})
	}

	t.Run("old_appjs_returns_404", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/static/app.js", nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET /static/app.js returned status %d, want 404", w.Code)
		}
	})

	t.Run("source_maps_served", func(t *testing.T) {
		maps := []string{
			"/static/js/index.js.map",
			"/static/js/admin.js.map",
			"/static/js/login.js.map",
			"/static/js/setup.js.map",
			"/static/js/map.js.map",
		}
		for _, file := range maps {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", file, nil)
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("GET %s returned status %d, want 200", file, w.Code)
			}
		}
	})

	t.Run("service_worker_references_index_js", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/sw.js", nil)
		router.ServeHTTP(w, req)
		body := w.Body.String()
		if !strings.Contains(body, "/static/js/index.js") {
			t.Error("service worker should reference /static/js/index.js")
		}
		if strings.Contains(body, "/static/app.js") {
			t.Error("service worker should NOT reference /static/app.js")
		}
	})
}

func TestManifestAndServiceWorker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newTestDB(t)
	router := setupTestRouter()

	router.GET("/api/manifest.json", webSvc.Manifest)
	router.GET("/sw.js", webSvc.ServiceWorker)

	t.Run("manifest_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/manifest.json", nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d", w.Code)
		}
		var manifest map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest["name"] != "TRACES - Your Year in Review" {
			t.Errorf("manifest name = %q", manifest["name"])
		}
	})

	t.Run("service_worker_endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/sw.js", nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d", w.Code)
		}
		if len(w.Body.Bytes()) == 0 {
			t.Error("empty service worker response")
		}
	})
}
