package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	authpkg "traces/internal/auth"
	"traces/internal/database"
	"traces/internal/models"
)

func setupPasswordResetRouter2(t *testing.T, capture *string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	authSvc.SetSendEmail(func(cfg models.EmailConfig, to, subject, body string) error {
		if capture != nil {
			idx := strings.Index(body, "token=")
			if idx != -1 {
				tok := body[idx+6:]
				tok = strings.Fields(tok)[0]
				tok = strings.TrimSpace(tok)
				*capture = tok
			}
		}
		return nil
	})
	r := gin.New()
	r.POST("/api/request-password-reset", authSvc.HandleRequestPasswordReset)
	r.POST("/api/reset-password", authSvc.HandleResetPassword)
	r.GET("/api/reset-password/validate", authSvc.HandleValidateResetToken)
	r.POST("/api/login", authSvc.HandleLogin)
	return r
}

func TestPasswordReset(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("enumeration_safe_unknown_email", func(t *testing.T) {
		newTestDB(t)
		database.Migrate(db)
		authpkg.ResetRateLimitForTest()
		var captured string
		router := setupPasswordResetRouter2(t, &captured)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/request-password-reset", strings.NewReader(`{"email":"unknown@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d want 200 body %s", w.Code, w.Body.String())
		}
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "ok" {
			t.Errorf("resp %v", resp)
		}
		var count int
		db.QueryRow("SELECT COUNT(*) FROM password_reset_tokens").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 tokens for unknown user, got %d", count)
		}
		if captured != "" {
			t.Errorf("should not have sent email")
		}
	})

	t.Run("successful_reset", func(t *testing.T) {
		newTestDB(t)
		database.Migrate(db)
		hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.DefaultCost)
		if _, err := db.Exec("INSERT INTO users (username, display_name, email, password_hash) VALUES (?, ?, ?, ?)", "alice", "Alice", "alice@example.com", string(hash)); err != nil {
			t.Fatalf("insert alice: %v", err)
		}
		db.Exec(`CREATE TABLE IF NOT EXISTS admin_users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE, password TEXT)`)
		authpkg.ResetRateLimitForTest()
		var captured string
		router := setupPasswordResetRouter2(t, &captured)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/request-password-reset", strings.NewReader(`{"email":"alice@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request status %d body %s", w.Code, w.Body.String())
		}
		if captured == "" {
			t.Fatal("expected token captured")
		}
		w = httptest.NewRecorder()
		req = httptest.NewRequest("GET", "/api/reset-password/validate?token="+captured, nil)
		router.ServeHTTP(w, req)
		var vresp map[string]bool
		json.Unmarshal(w.Body.Bytes(), &vresp)
		if !vresp["valid"] {
			t.Fatalf("expected valid token, got %v", vresp)
		}
		th := sha256.Sum256([]byte(captured))
		thHex := hex.EncodeToString(th[:])
		var stored string
		db.QueryRow("SELECT token_hash FROM password_reset_tokens").Scan(&stored)
		if stored != thHex {
			t.Errorf("hash mismatch")
		}
		if stored == captured {
			t.Errorf("token stored plaintext")
		}
		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/api/reset-password", strings.NewReader(`{"token":"`+captured+`","password":"newpassword123"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("reset status %d body %s", w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"alice","password":"newpassword123"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login after reset failed %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("expired_token_rejected", func(t *testing.T) {
		newTestDB(t)
		database.Migrate(db)
		hash, _ := bcrypt.GenerateFromPassword([]byte("oldpass123"), bcrypt.DefaultCost)
		db.Exec("INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)", "bob", "bob@example.com", string(hash))
		var uid int64
		db.QueryRow("SELECT id FROM users WHERE username='bob'").Scan(&uid)
		tok := "expiredtoken1234567890abcdef1234567890abcdef"
		th := sha256.Sum256([]byte(tok))
		thHex := hex.EncodeToString(th[:])
		db.Exec("INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)", uid, thHex, time.Now().Add(-2*time.Hour).Unix())
		var captured string
		router := setupPasswordResetRouter2(t, &captured)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/reset-password", strings.NewReader(`{"token":"`+tok+`","password":"newpassword123"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for expired, got %d %s", w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		req = httptest.NewRequest("GET", "/api/reset-password/validate?token="+tok, nil)
		router.ServeHTTP(w, req)
		var vresp map[string]bool
		json.Unmarshal(w.Body.Bytes(), &vresp)
		if vresp["valid"] {
			t.Errorf("expired should be invalid")
		}
	})

	t.Run("reused_token_rejected", func(t *testing.T) {
		newTestDB(t)
		database.Migrate(db)
		hash, _ := bcrypt.GenerateFromPassword([]byte("oldpass123"), bcrypt.DefaultCost)
		if _, err := db.Exec("INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)", "carol", "carol@example.com", string(hash)); err != nil {
			t.Fatalf("insert carol: %v", err)
		}
		authpkg.ResetRateLimitForTest()
		var captured string
		router := setupPasswordResetRouter2(t, &captured)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/request-password-reset", strings.NewReader(`{"email":"carol@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if captured == "" {
			t.Fatalf("no token body=%s", w.Body.String())
		}
		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/api/reset-password", strings.NewReader(`{"token":"`+captured+`","password":"newpass123"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("first reset failed %d %s", w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/api/reset-password", strings.NewReader(`{"token":"`+captured+`","password":"another123"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for reused, got %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("weak_password_rejected", func(t *testing.T) {
		newTestDB(t)
		database.Migrate(db)
		hash, _ := bcrypt.GenerateFromPassword([]byte("oldpass123"), bcrypt.DefaultCost)
		if _, err := db.Exec("INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)", "dave", "dave@example.com", string(hash)); err != nil {
			t.Fatalf("insert dave: %v", err)
		}
		authpkg.ResetRateLimitForTest()
		var captured string
		router := setupPasswordResetRouter2(t, &captured)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/request-password-reset", strings.NewReader(`{"email":"dave@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if captured == "" {
			t.Fatalf("no token body=%s", w.Body.String())
		}
		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/api/reset-password", strings.NewReader(`{"token":"`+captured+`","password":"short"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for weak password, got %d %s", w.Code, w.Body.String())
		}
	})
}
