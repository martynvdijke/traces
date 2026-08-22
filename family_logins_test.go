package main

import (
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
	"golang.org/x/crypto/bcrypt"
)

// setupFamilyAuthTest swaps globals for an isolated in-memory DB and session
// store with the schema pieces needed by auth/account handlers.
func setupFamilyAuthTest(t *testing.T) *sql.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origDB := db
	origSessionStore := sessionStore
	origCSRFTokens := csrfTokens
	t.Cleanup(func() {
		db = origDB
		sessionStore = origSessionStore
		csrfTokens = origCSRFTokens
	})

	var err error
	db, err = sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	sessionStore = make(map[string]sessionInfo)
	csrfTokens = make(map[string]string)

	db.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		display_name TEXT DEFAULT '',
		email TEXT DEFAULT '',
		color TEXT DEFAULT '#7c3aed',
		avatar_url TEXT DEFAULT '',
		password_hash TEXT DEFAULT '',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)

	return db
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hashed)
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

func TestFamilyLogin(t *testing.T) {
	database := setupFamilyAuthTest(t)

	database.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", "admin", mustHash(t, "admin_password"))

	aliceHash := mustHash(t, "alice_password")
	res, err := database.Exec("INSERT INTO users (username, display_name, color, password_hash) VALUES (?, ?, ?, ?)",
		"alice", "Alice", "#ef4444", aliceHash)
	if err != nil {
		t.Fatal(err)
	}
	aliceID, _ := res.LastInsertId()

	// Bob has no password yet: profile only, cannot log in.
	database.Exec("INSERT INTO users (username, display_name, color) VALUES (?, ?, ?)", "bob", "Bob", "#3b82f6")

	router := gin.New()
	router.POST("/api/login", handleLogin)

	countSessions := func() int { return len(sessionStore) }

	t.Run("family_login_success", func(t *testing.T) {
		before := countSessions()
		w := doJSON(router, "POST", "/api/login", `{"username":"alice","password":"alice_password"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if countSessions() != before+1 {
			t.Fatal("no session created")
		}
		found := false
		for _, sess := range sessionStore {
			if sess.userID == aliceID {
				found = true
			}
		}
		if !found {
			t.Errorf("no session stamped with family userID %d", aliceID)
		}
	})

	t.Run("family_wrong_password_rejected", func(t *testing.T) {
		before := countSessions()
		w := doJSON(router, "POST", "/api/login", `{"username":"alice","password":"wrong"}`)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
		if countSessions() != before {
			t.Error("session created despite wrong password")
		}
	})

	t.Run("family_account_without_password_rejected", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/login", `{"username":"bob","password":"whatever"}`)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("unknown_user_generic_error", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/login", `{"username":"nobody","password":"x"}`)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Invalid credentials") {
			t.Errorf("expected generic error, got %s", w.Body.String())
		}
	})

	t.Run("admin_login_still_works", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/login", `{"username":"admin","password":"admin_password"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		found := false
		for _, sess := range sessionStore {
			if sess.userID == 0 {
				found = true
			}
		}
		if !found {
			t.Error("admin login did not create a userID-0 session")
		}
	})
}

func TestLegacySessionAuthenticatesAsAdmin(t *testing.T) {
	setupFamilyAuthTest(t)

	// Expiry-only style entry: userID 0 carries no identity and must resolve
	// to the admin identity, as legacy sessions did.
	router := gin.New()
	router.Use(authMiddlewareGin())
	router.GET("/probe", func(c *gin.Context) {
		cu := getCurrentUser(c)
		c.JSON(http.StatusOK, gin.H{"id": cu.ID, "name": cu.Name})
	})
	cookie := "legacy-cookie"
	sessionStore[cookie] = sessionInfo{userID: 0, expiresAt: time.Now().Add(time.Hour).Unix()}

	w := doJSON(router, "GET", "/probe", "", "session="+cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.ID != 0 || got.Name != "Admin" {
		t.Errorf("legacy session resolved to %+v, want admin identity", got)
	}
}

func TestFamilySessionResolvesToUser(t *testing.T) {
	database := setupFamilyAuthTest(t)

	res, _ := database.Exec("INSERT INTO users (username, display_name, color) VALUES (?, ?, ?)", "alice", "Alice", "#ef4444")
	aliceID, _ := res.LastInsertId()

	router := gin.New()
	router.Use(authMiddlewareGin())
	router.GET("/probe", func(c *gin.Context) {
		cu := getCurrentUser(c)
		c.JSON(http.StatusOK, gin.H{"id": cu.ID, "name": cu.Name, "color": cu.Color})
	})
	cookie := "family-cookie"
	sessionStore[cookie] = sessionInfo{userID: aliceID, expiresAt: time.Now().Add(time.Hour).Unix()}

	w := doJSON(router, "GET", "/probe", "", "session="+cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.ID != aliceID || got.Name != "Alice" || got.Color != "#ef4444" {
		t.Errorf("family session resolved to %+v, want Alice identity", got)
	}
}

func TestSaveUserAccount(t *testing.T) {
	database := setupFamilyAuthTest(t)

	database.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", "admin", mustHash(t, "admin_password"))

	router := gin.New()
	router.POST("/api/users", saveUser)

	t.Run("create_with_password_hashes_bcrypt", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/users", `{"username":"carol","display_name":"Carol","color":"#10b981","password":"carol_password"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "carol_password") {
			t.Error("response leaks password")
		}

		var hash string
		if err := database.QueryRow("SELECT password_hash FROM users WHERE username = 'carol'").Scan(&hash); err != nil {
			t.Fatalf("carol row missing: %v", err)
		}
		if !strings.HasPrefix(hash, "$2") {
			t.Errorf("password_hash is not bcrypt: %q", hash)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("carol_password")); err != nil {
			t.Errorf("hash does not verify against password: %v", err)
		}
	})

	t.Run("create_rejects_admin_username_collision", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/users", `{"username":"admin","display_name":"Impostor","password":"pw12345"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Username already taken") {
			t.Errorf("expected validation error, got %s", w.Body.String())
		}
	})

	t.Run("create_rejects_family_username_collision", func(t *testing.T) {
		w := doJSON(router, "POST", "/api/users", `{"username":"carol","display_name":"Clone","password":"pw12345"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("update_replaces_and_keeps_hash", func(t *testing.T) {
		var carolID int
		database.QueryRow("SELECT id FROM users WHERE username = 'carol'").Scan(&carolID)

		var oldHash string
		database.QueryRow("SELECT password_hash FROM users WHERE id = ?", carolID).Scan(&oldHash)

		// Update without password keeps the hash.
		w := doJSON(router, "POST", "/api/users", `{"id":`+fmt.Sprintf("%d", carolID)+`,"username":"carol","display_name":"Carol II","color":"#10b981"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var keptHash string
		database.QueryRow("SELECT password_hash FROM users WHERE id = ?", carolID).Scan(&keptHash)
		if keptHash != oldHash {
			t.Error("hash changed on update without password")
		}

		// Update with password replaces the hash.
		w = doJSON(router, "POST", "/api/users", `{"id":`+fmt.Sprintf("%d", carolID)+`,"username":"carol","display_name":"Carol II","color":"#10b981","password":"new_password"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		var newHash string
		database.QueryRow("SELECT password_hash FROM users WHERE id = ?", carolID).Scan(&newHash)
		if newHash == oldHash {
			t.Error("hash not replaced on password update")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(newHash), []byte("new_password")); err != nil {
			t.Errorf("new hash does not verify: %v", err)
		}
	})
}
