package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"traces/internal/integrations"
)

// TestGetOtelSettingsPublicUnauthenticated proves that the mobile-facing
// endpoint answers without a session cookie and exposes the same fields as the
// admin-only /api/otel/config route. The router below mirrors the public /api
// group in main.go: no auth or CSRF middleware is attached.
func TestGetOtelSettingsPublicUnauthenticated(t *testing.T) {
	database := setupOtelDB(t)
	if _, err := database.Exec(
		`UPDATE otel_settings SET endpoint=?, traces_enabled=?, metrics_enabled=?, logs_enabled=? WHERE id=1`,
		"https://otlp.example.com", 1, 0, 1,
	); err != nil {
		t.Fatal(err)
	}

	svc := integrations.New(database, nil, nil)
	router := gin.New()
	api := router.Group("/api")
	api.GET("/otel-settings", svc.GetOtelSettings)

	req := httptest.NewRequest(http.MethodGet, "/api/otel-settings", nil)
	// Explicitly no Cookie / Authorization header: the route must be public.
	req.Header.Del("Cookie")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/otel-settings status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"endpoint", "traces_enabled", "metrics_enabled", "logs_enabled"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}
	if body["endpoint"] != "https://otlp.example.com" {
		t.Errorf("endpoint = %v, want https://otlp.example.com", body["endpoint"])
	}
	if body["traces_enabled"] != true {
		t.Errorf("traces_enabled = %v, want true", body["traces_enabled"])
	}
	if body["metrics_enabled"] != false {
		t.Errorf("metrics_enabled = %v, want false", body["metrics_enabled"])
	}
	if body["logs_enabled"] != true {
		t.Errorf("logs_enabled = %v, want true", body["logs_enabled"])
	}
}
