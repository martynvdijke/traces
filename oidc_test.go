package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// setupOIDCTestDB swaps the global db for an in-memory one with the v22 users
// columns needed by OIDC linking.
func setupOIDCTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origDB := db
	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	origCfg := oidcCfg
	t.Cleanup(func() {
		db = origDB
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
		oidcCfg = origCfg
		oidcResetProvider()
	})

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	sessionStore = make(map[string]sessionInfo)
	csrfTokens = make(map[string]string)
	oidcCfg = oidcConfig{}

	db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		email TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		avatar_url TEXT DEFAULT '',
		password_hash TEXT DEFAULT '',
		oidc_sub TEXT DEFAULT '',
		auth_method TEXT DEFAULT '',
		is_admin INTEGER DEFAULT 0,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
}

func oidcTestRouter() *gin.Engine {
	r := gin.New()
	api := r.Group("/api")
	api.GET("/auth/oidc/login", handleOIDCLogin)
	api.GET("/auth/oidc/callback", handleOIDCCallback)
	api.GET("/auth/oidc/logout", handleOIDCLogout)
	return r
}

func TestOIDCIsAdminGroups(t *testing.T) {
	if !oidcIsAdmin([]string{"users", "admins"}) {
		t.Error("groups containing admins should map to admin")
	}
	if oidcIsAdmin([]string{"users"}) {
		t.Error("groups without admins should not map to admin")
	}
	if oidcIsAdmin(nil) {
		t.Error("nil groups should not map to admin")
	}
}

func TestOIDCClientSecretFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretPath, []byte("s3cret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OIDC_CLIENT_SECRET_FILE", secretPath)
	t.Setenv("OIDC_CLIENT_SECRET", "fallback")
	if got := oidcClientSecret(); got != "s3cret" {
		t.Errorf("secret from file = %q, want %q", got, "s3cret")
	}
}

func TestOIDCDisabledRoutesReturnNotFound(t *testing.T) {
	setupOIDCTestDB(t)
	r := oidcTestRouter()

	for _, target := range []string{"/api/auth/oidc/login", "/api/auth/oidc/callback", "/api/auth/oidc/logout"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s with OIDC disabled = %d, want 404", target, w.Code)
		}
	}
}

func TestOIDCCallbackRejectsBadState(t *testing.T) {
	setupOIDCTestDB(t)
	oidcCfg = oidcConfig{Enabled: true, IssuerURL: "https://idp.example.com", ClientID: "x", ClientSecret: "y", RedirectURL: "https://app.example.com/api/auth/oidc/callback"}
	r := oidcTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=wrong&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: "right"})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("callback with mismatched state = %d, want 400", w.Code)
	}
	if len(sessionStore) != 0 {
		t.Error("no session should be created on state mismatch")
	}
}

func TestOIDCLinkOrProvision(t *testing.T) {
	setupOIDCTestDB(t)

	// First login auto-provisions, non-admin without admins group.
	id, err := oidcLinkOrProvision("sub-1", "Ada@Example.com", "Ada", []string{"users"})
	if err != nil {
		t.Fatal(err)
	}
	var email, sub, method string
	var isAdmin int
	if err := db.QueryRow("SELECT email, oidc_sub, auth_method, is_admin FROM users WHERE id = ?", id).
		Scan(&email, &sub, &method, &isAdmin); err != nil {
		t.Fatal(err)
	}
	if email != "Ada@Example.com" || sub != "sub-1" || method != "oidc" || isAdmin != 0 {
		t.Errorf("provisioned row = (%q,%q,%q,%d), unexpected", email, sub, method, isAdmin)
	}

	// Returning login with admins group syncs the admin flag on the same row.
	id2, err := oidcLinkOrProvision("sub-1", "ada@example.com", "Ada", []string{"admins"})
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id {
		t.Errorf("returning sub got new id %d, want %d", id2, id)
	}
	db.QueryRow("SELECT is_admin FROM users WHERE id = ?", id).Scan(&isAdmin)
	if isAdmin != 1 {
		t.Error("admin flag should sync to 1 when groups gains admins")
	}

	// Existing password user with matching email gets linked, not duplicated.
	if _, err := db.Exec("INSERT INTO users (username, email) VALUES ('grace', 'grace@example.com')"); err != nil {
		t.Fatal(err)
	}
	id3, err := oidcLinkOrProvision("sub-9", "grace@example.com", "Grace", nil)
	if err != nil {
		t.Fatal(err)
	}
	var linked int64
	db.QueryRow("SELECT id FROM users WHERE username = 'grace'").Scan(&linked)
	if id3 != linked {
		t.Errorf("email link created id %d, want existing %d", id3, linked)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM users WHERE email = 'grace@example.com'").Scan(&count)
	if count != 1 {
		t.Errorf("email link duplicated user row (count=%d)", count)
	}

	// Unverified/missing identity is rejected.
	if _, err := oidcLinkOrProvision("", "x@example.com", "X", nil); err == nil {
		t.Error("empty sub should be rejected")
	}
	if _, err := oidcLinkOrProvision("sub-z", "", "X", nil); err == nil {
		t.Error("empty email should be rejected")
	}
}

func TestOIDCUsernameDedup(t *testing.T) {
	setupOIDCTestDB(t)
	db.Exec("INSERT INTO users (username, email) VALUES ('ada', 'ada@x.com')")
	if got := oidcUsernameForEmail("ada@y.com"); got == "ada" {
		t.Error("derived username should avoid collision with existing username")
	}
	if got := oidcUsernameForEmail("not-an-email!!"); got == "" {
		t.Error("derived username should never be empty")
	}
}
