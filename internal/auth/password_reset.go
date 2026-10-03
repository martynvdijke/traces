package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"traces/internal/models"
)

// rate limiting: 5/min per IP
var (
	rlMu    sync.Mutex
	rlHits  = map[string][]int64{}
)

func checkRateLimit(ip string) bool {
	now := time.Now().Unix()
	rlMu.Lock()
	defer rlMu.Unlock()
	hits := rlHits[ip]
	// prune >60s
	n := hits[:0]
	for _, t := range hits {
		if now-t < 60 {
			n = append(n, t)
		}
	}
	hits = n
	if len(hits) >= 5 {
		rlHits[ip] = hits
		return false
	}
	hits = append(hits, now)
	rlHits[ip] = hits
	return true
}

func (s *Service) SetSendEmail(fn func(models.EmailConfig, string, string, string) error) { s.sendEmail = fn }

func ResetRateLimitForTest() {
	rlMu.Lock()
	defer rlMu.Unlock()
	rlHits = map[string][]int64{}
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Service) HandleRequestPasswordReset(c *gin.Context) {
	if !checkRateLimit(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
		return
	}
	var input struct {
		Email    string `json:"email"`
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	email := strings.TrimSpace(input.Email)
	username := strings.TrimSpace(input.Username)
	// also accept single field fallback: if email looks like username without @, treat as username
	var userID int64
	var userEmail string
	var found bool
	if email != "" {
		err := s.db.QueryRow("SELECT id, email FROM users WHERE LOWER(email)=LOWER(?) AND email<>'' LIMIT 1", email).Scan(&userID, &userEmail)
		if err == nil {
			found = true
		}
	}
	if !found && username != "" {
		err := s.db.QueryRow("SELECT id, email FROM users WHERE LOWER(username)=LOWER(?) AND email<>'' LIMIT 1", username).Scan(&userID, &userEmail)
		if err == nil {
			found = true
		}
	}
	// If email field provided but no @, try username lookup
	if !found && email != "" && !strings.Contains(email, "@") {
		err := s.db.QueryRow("SELECT id, email FROM users WHERE LOWER(username)=LOWER(?) AND email<>'' LIMIT 1", email).Scan(&userID, &userEmail)
		if err == nil {
			found = true
		}
	}
	if found {
		// generate token
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Printf("[auth] failed to generate token: %v", err)
		} else {
			token := hex.EncodeToString(b)
			th := hashToken(token)
			expires := time.Now().Add(1 * time.Hour).Unix()
			_, err := s.db.Exec("INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)", userID, th, expires)
			if err != nil {
				log.Printf("[auth] failed to insert reset token: %v", err)
			} else {
				// cleanup expired opportunistically
				s.db.Exec("DELETE FROM password_reset_tokens WHERE expires_at < ?", time.Now().Unix())
				// send email
				if s.sendEmail != nil {
					// build reset link
					scheme := "https"
					if c.Request.TLS == nil && c.GetHeader("X-Forwarded-Proto") != "https" {
						// use http for local dev
						scheme = "http"
					}
					host := c.Request.Host
					if host == "" {
						host = "localhost"
					}
					link := scheme + "://" + host + "/reset.html?token=" + token
					subject := "TRACES Password Reset"
					body := "You requested a password reset for TRACES.\n\nClick the link to reset your password (valid for 1 hour):\n" + link + "\n\nIf you did not request this, ignore this email.\n"
					cfg := s.getEmailConfig()
					_ = s.sendEmail(cfg, userEmail, subject, body)
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) getEmailConfig() models.EmailConfig {
	var cfg models.EmailConfig
	var port int
	err := s.db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&cfg.SMTPHost, &port, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err == nil {
		cfg.SMTPPort = port
	}
	return cfg
}

func (s *Service) HandleResetPassword(c *gin.Context) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	if len(input.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
		return
	}
	if input.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token required"})
		return
	}
	th := hashToken(input.Token)
	var id, userID int64
	var expiresAt int64
	var usedAt *int64
	err := s.db.QueryRow("SELECT id, user_id, expires_at, used_at FROM password_reset_tokens WHERE token_hash=?", th).Scan(&id, &userID, &expiresAt, &usedAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired token"})
		return
	}
	if usedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token already used"})
		return
	}
	if time.Now().Unix() > expiresAt {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired token"})
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}
	_, err = s.db.Exec("UPDATE users SET password_hash=? WHERE id=?", string(hashed), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}
	now := time.Now().Unix()
	s.db.Exec("UPDATE password_reset_tokens SET used_at=? WHERE id=?", now, id)
	// invalidate other tokens for user
	s.db.Exec("DELETE FROM password_reset_tokens WHERE user_id=? AND id != ?", userID, id)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) HandleValidateResetToken(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusOK, gin.H{"valid": false})
		return
	}
	th := hashToken(token)
	var expiresAt int64
	var usedAt *int64
	err := s.db.QueryRow("SELECT expires_at, used_at FROM password_reset_tokens WHERE token_hash=?", th).Scan(&expiresAt, &usedAt)
	if err != nil || usedAt != nil || time.Now().Unix() > expiresAt {
		c.JSON(http.StatusOK, gin.H{"valid": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true})
}
