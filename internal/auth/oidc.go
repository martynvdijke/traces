package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"

	"traces/internal/httpx"
	"traces/internal/models"
)

type oidcConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// OIDCConfig is exported for tests.
type OIDCConfig = oidcConfig

type oidcClaims struct {
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
}

func (s *Service) InitOIDCFromEnv() {
	s.oidcCfg.Enabled = os.Getenv("OIDC_ENABLED") == "true"
	s.oidcCfg.IssuerURL = strings.TrimRight(os.Getenv("OIDC_ISSUER_URL"), "/")
	s.oidcCfg.ClientID = os.Getenv("OIDC_CLIENT_ID")
	s.oidcCfg.ClientSecret = oidcClientSecret()
	s.oidcCfg.RedirectURL = os.Getenv("OIDC_REDIRECT_URL")
	s.oidcCfg.Scopes = strings.Fields(os.Getenv("OIDC_SCOPES"))
	if len(s.oidcCfg.Scopes) == 0 {
		s.oidcCfg.Scopes = []string{"openid", "email", "profile", "groups"}
	}
	if s.oidcCfg.Enabled && !s.OIDCReady() {
		log.Printf("[OIDC] OIDC_ENABLED=true but issuer/client_id/secret/redirect_url incomplete — OIDC login disabled")
	}
}

func oidcClientSecret() string {
	if path := os.Getenv("OIDC_CLIENT_SECRET_FILE"); path != "" {
		if b, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(b))
		} else {
			log.Printf("[OIDC] Could not read OIDC_CLIENT_SECRET_FILE: %v", err)
		}
	}
	return os.Getenv("OIDC_CLIENT_SECRET")
}

func (s *Service) OIDCReady() bool {
	return s.oidcCfg.Enabled && s.oidcCfg.IssuerURL != "" && s.oidcCfg.ClientID != "" &&
		s.oidcCfg.ClientSecret != "" && s.oidcCfg.RedirectURL != ""
}

func (s *Service) ensureOIDCProvider(ctx context.Context) error {
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()
	if s.oidcProvider != nil {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, s.oidcCfg.IssuerURL)
	if err != nil {
		return err
	}
	s.oidcProvider = provider
	s.oidcVerifier = provider.Verifier(&oidc.Config{ClientID: s.oidcCfg.ClientID})
	s.oidcOAuth2 = &oauth2.Config{
		ClientID:     s.oidcCfg.ClientID,
		ClientSecret: s.oidcCfg.ClientSecret,
		RedirectURL:  s.oidcCfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       s.oidcCfg.Scopes,
	}
	return nil
}

func (s *Service) OIDCResetProvider() {
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()
	s.oidcProvider = nil
	s.oidcVerifier = nil
	s.oidcOAuth2 = nil
}

func oidcRandomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func oidcPKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func setOIDCTempCookie(c *gin.Context, name, value string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/auth/oidc/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearOIDCTempCookies(c *gin.Context) {
	for _, name := range []string{"oidc_state", "oidc_nonce", "oidc_verifier"} {
		http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: "/api/auth/oidc/", MaxAge: -1, HttpOnly: true})
	}
}

func (s *Service) HandleOIDCLogin(c *gin.Context) {
	if !s.OIDCReady() {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC login is not enabled"})
		return
	}
	if err := s.ensureOIDCProvider(c.Request.Context()); err != nil {
		log.Printf("[OIDC] Discovery failed for %s: %v", s.oidcCfg.IssuerURL, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Identity provider unavailable"})
		return
	}
	state, err := oidcRandomURLSafe(24)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	nonce, err := oidcRandomURLSafe(24)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	verifier, err := oidcRandomURLSafe(32)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	setOIDCTempCookie(c, "oidc_state", state)
	setOIDCTempCookie(c, "oidc_nonce", nonce)
	setOIDCTempCookie(c, "oidc_verifier", verifier)

	s.oidcMu.Lock()
	cfg := s.oidcOAuth2
	s.oidcMu.Unlock()
	authURL := cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	c.Redirect(http.StatusFound, authURL)
}

func (s *Service) HandleOIDCCallback(c *gin.Context) {
	if !s.OIDCReady() {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC login is not enabled"})
		return
	}
	stateCookie, err := c.Cookie("oidc_state")
	if err != nil || stateCookie == "" || c.Query("state") != stateCookie {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid state"})
		return
	}
	verifier, err := c.Cookie("oidc_verifier")
	if err != nil || verifier == "" {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing PKCE verifier"})
		return
	}
	nonce, _ := c.Cookie("oidc_nonce")
	if err := s.ensureOIDCProvider(c.Request.Context()); err != nil {
		clearOIDCTempCookies(c)
		log.Printf("[OIDC] Discovery failed for %s: %v", s.oidcCfg.IssuerURL, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Identity provider unavailable"})
		return
	}

	s.oidcMu.Lock()
	cfg := s.oidcOAuth2
	verifierCfg := s.oidcVerifier
	s.oidcMu.Unlock()

	ctx := c.Request.Context()
	token, err := cfg.Exchange(ctx, c.Query("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Code exchange failed"})
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No ID token returned"})
		return
	}
	idToken, err := verifierCfg.Verify(ctx, rawID)
	if err != nil {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid ID token"})
		return
	}
	if nonce != "" && idToken.Nonce != nonce {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid nonce"})
		return
	}
	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid ID token claims"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" || !claims.EmailVerified {
		clearOIDCTempCookies(c)
		c.JSON(http.StatusForbidden, gin.H{"error": "Verified email required"})
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	userID, err := s.oidcLinkOrProvision(idToken.Subject, email, name, claims.Groups)
	if err != nil {
		clearOIDCTempCookies(c)
		httpx.ServerError(c, err)
		return
	}
	clearOIDCTempCookies(c)
	sessionID, err := GenerateSessionID()
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	s.sessions.Set(sessionID, SessionInfo{UserID: userID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()})
	s.sessions.SetCSRF(sessionID, oidcCSRFToken(sessionID))
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusFound, "/admin.html")
}

func oidcCSRFToken(sessionID string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"-csrf")))
}

func (s *Service) HandleOIDCLogout(c *gin.Context) {
	if !s.OIDCReady() {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC login is not enabled"})
		return
	}
	if cookie, err := c.Cookie("session"); err == nil {
		s.sessions.DeleteWithCSRF(cookie)
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusFound, s.oidcCfg.IssuerURL+"/logout")
}

func oidcIsAdmin(groups []string) bool {
	for _, g := range groups {
		if g == "admins" {
			return true
		}
	}
	return false
}

func (s *Service) OIDCIsAdmin(groups []string) bool { return oidcIsAdmin(groups) }

func (s *Service) oidcLinkOrProvision(sub, email, name string, groups []string) (int64, error) {
	if sub == "" || email == "" {
		return 0, errOIDCIdentity
	}
	isAdmin := 0
	if oidcIsAdmin(groups) {
		isAdmin = 1
	}
	var id int64
	if err := s.db.QueryRow("SELECT id FROM users WHERE oidc_sub = ?", sub).Scan(&id); err == nil {
		_, err := s.db.Exec("UPDATE users SET email = ?, is_admin = ?, auth_method = 'oidc' WHERE id = ?", email, isAdmin, id)
		return id, err
	}
	if err := s.db.QueryRow("SELECT id FROM users WHERE lower(email) = lower(?)", email).Scan(&id); err == nil {
		_, err := s.db.Exec("UPDATE users SET oidc_sub = ?, is_admin = ?, auth_method = 'oidc' WHERE id = ?", sub, isAdmin, id)
		return id, err
	}
	username := s.oidcUsernameForEmail(email)
	display := name
	if display == "" {
		display = username
	}
	res, err := s.db.Exec(`INSERT INTO users (username, display_name, email, color, oidc_sub, auth_method, is_admin)
		VALUES (?, ?, ?, ?, ?, 'oidc', ?)`, username, display, email, models.DefaultColor, sub, isAdmin)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// OIDCLinkOrProvision is exported for tests.
func (s *Service) OIDCLinkOrProvision(sub, email, name string, groups []string) (int64, error) {
	return s.oidcLinkOrProvision(sub, email, name, groups)
}

func (s *Service) oidcUsernameForEmail(email string) string {
	local := email
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	var sb strings.Builder
	for _, r := range strings.ToLower(local) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			sb.WriteRune(r)
		}
	}
	base := sb.String()
	if base == "" {
		base = "oidc-user"
	}
	candidate := base
	for i := 2; ; i++ {
		var count int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", candidate).Scan(&count); err != nil || count == 0 {
			return candidate
		}
		candidate = strings.ToLower(strings.TrimSpace(base + "-" + itoa(i)))
	}
}

func (s *Service) OIDCUsernameForEmail(email string) string { return s.oidcUsernameForEmail(email) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for n := i; n > 0; n /= 10 {
		p--
		b[p] = byte('0' + n%10)
	}
	return string(b[p:])
}

type oidcError string

func (e oidcError) Error() string { return string(e) }

const errOIDCIdentity oidcError = "incomplete OIDC identity"

// Exported helpers for tests / main
func (s *Service) OIDCClientSecret() string { return s.oidcCfg.ClientSecret }

func OIDCClientSecret() string { return oidcClientSecret() }

// OIDC config field accessors for tests if needed
func (s *Service) SetOIDCConfig(cfg oidcConfig) { s.oidcCfg = cfg }
func (s *Service) GetOIDCConfig() oidcConfig    { return s.oidcCfg }

// Additional fields stored inside Service struct
// We need to declare them; they are below
