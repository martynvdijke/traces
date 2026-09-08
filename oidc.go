package main

// OIDC login via Authelia (native relying party).
//
// Disabled unless OIDC_ENABLED=true with issuer/client config present.
// Sessions reuse the existing `session` cookie + CSRF store, so no
// AuthMiddleware change is needed and password login keeps working as fallback.

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
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

type oidcConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

var (
	oidcCfg      oidcConfig
	oidcMu       sync.Mutex
	oidcProvider *oidc.Provider
	oidcVerifier *oidc.IDTokenVerifier
	oidcOAuth2   *oauth2.Config
)

// oidcClaims is the subset of ID token claims Traces cares about.
type oidcClaims struct {
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
}

// initOIDCFromEnv loads OIDC config from the environment. The client secret
// comes from OIDC_CLIENT_SECRET_FILE (Docker secret) or OIDC_CLIENT_SECRET;
// it is never read from git-tracked files.
func initOIDCFromEnv() {
	oidcCfg.Enabled = os.Getenv("OIDC_ENABLED") == "true"
	oidcCfg.IssuerURL = strings.TrimRight(os.Getenv("OIDC_ISSUER_URL"), "/")
	oidcCfg.ClientID = os.Getenv("OIDC_CLIENT_ID")
	oidcCfg.ClientSecret = oidcClientSecret()
	oidcCfg.RedirectURL = os.Getenv("OIDC_REDIRECT_URL")
	oidcCfg.Scopes = strings.Fields(os.Getenv("OIDC_SCOPES"))
	if len(oidcCfg.Scopes) == 0 {
		oidcCfg.Scopes = []string{"openid", "email", "profile", "groups"}
	}
	if oidcCfg.Enabled && !oidcReady() {
		log.Printf("[OIDC] OIDC_ENABLED=true but issuer/client_id/secret/redirect_url incomplete — OIDC login disabled")
	}
}

// oidcClientSecret resolves the secret from file first, env second.
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

// oidcReady reports whether OIDC login can be offered (env-complete).
// Provider discovery happens lazily on first login.
func oidcReady() bool {
	return oidcCfg.Enabled && oidcCfg.IssuerURL != "" && oidcCfg.ClientID != "" &&
		oidcCfg.ClientSecret != "" && oidcCfg.RedirectURL != ""
}

// ensureOIDCProvider discovers endpoints via the issuer and caches the result.
func ensureOIDCProvider(ctx context.Context) error {
	oidcMu.Lock()
	defer oidcMu.Unlock()
	if oidcProvider != nil {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, oidcCfg.IssuerURL)
	if err != nil {
		return err
	}
	oidcProvider = provider
	oidcVerifier = provider.Verifier(&oidc.Config{ClientID: oidcCfg.ClientID})
	oidcOAuth2 = &oauth2.Config{
		ClientID:     oidcCfg.ClientID,
		ClientSecret: oidcCfg.ClientSecret,
		RedirectURL:  oidcCfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       oidcCfg.Scopes,
	}
	return nil
}

// oidcResetProvider drops cached discovery state (tests).
func oidcResetProvider() {
	oidcMu.Lock()
	defer oidcMu.Unlock()
	oidcProvider = nil
	oidcVerifier = nil
	oidcOAuth2 = nil
}

func oidcRandomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// oidcPKCEChallenge derives the S256 code challenge for a verifier.
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

// @Summary OIDC login
// @Description Redirect to Authelia for OIDC Authorization Code + PKCE login
// @Tags Authentication
// @Router /auth/oidc/login [get]
func handleOIDCLogin(c *gin.Context) {
	if !oidcReady() {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC login is not enabled"})
		return
	}
	if err := ensureOIDCProvider(c.Request.Context()); err != nil {
		log.Printf("[OIDC] Discovery failed for %s: %v", oidcCfg.IssuerURL, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Identity provider unavailable"})
		return
	}
	state, err := oidcRandomURLSafe(24)
	if err != nil {
		serverError(c, err)
		return
	}
	nonce, err := oidcRandomURLSafe(24)
	if err != nil {
		serverError(c, err)
		return
	}
	verifier, err := oidcRandomURLSafe(32)
	if err != nil {
		serverError(c, err)
		return
	}
	setOIDCTempCookie(c, "oidc_state", state)
	setOIDCTempCookie(c, "oidc_nonce", nonce)
	setOIDCTempCookie(c, "oidc_verifier", verifier)

	oidcMu.Lock()
	cfg := oidcOAuth2
	oidcMu.Unlock()
	authURL := cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	c.Redirect(http.StatusFound, authURL)
}

// @Summary OIDC callback
// @Description Handle Authelia code exchange, verify ID token, create session
// @Tags Authentication
// @Router /auth/oidc/callback [get]
func handleOIDCCallback(c *gin.Context) {
	if !oidcReady() {
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
	if err := ensureOIDCProvider(c.Request.Context()); err != nil {
		clearOIDCTempCookies(c)
		log.Printf("[OIDC] Discovery failed for %s: %v", oidcCfg.IssuerURL, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Identity provider unavailable"})
		return
	}

	oidcMu.Lock()
	cfg := oidcOAuth2
	verifierCfg := oidcVerifier
	oidcMu.Unlock()

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
	userID, err := oidcLinkOrProvision(idToken.Subject, email, name, claims.Groups)
	if err != nil {
		clearOIDCTempCookies(c)
		serverError(c, err)
		return
	}
	clearOIDCTempCookies(c)
	// Same session cookie shape as password login — AuthMiddleware accepts it as-is.
	sessionID, err := generateSessionID()
	if err != nil {
		serverError(c, err)
		return
	}
	sessionMu.Lock()
	sessionStore[sessionID] = sessionInfo{userID: userID, expiresAt: time.Now().Add(24 * time.Hour).Unix()}
	csrfTokens[sessionID] = oidcCSRFToken(sessionID)
	sessionMu.Unlock()
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

// @Summary OIDC logout
// @Description Clear local session and redirect to Authelia logout
// @Tags Authentication
// @Router /auth/oidc/logout [get]
func handleOIDCLogout(c *gin.Context) {
	if !oidcReady() {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC login is not enabled"})
		return
	}
	if cookie, err := c.Cookie("session"); err == nil {
		sessionMu.Lock()
		delete(sessionStore, cookie)
		delete(csrfTokens, cookie)
		sessionMu.Unlock()
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusFound, oidcCfg.IssuerURL+"/logout")
}

// oidcIsAdmin maps the IdP groups claim to the admin flag.
func oidcIsAdmin(groups []string) bool {
	for _, g := range groups {
		if g == "admins" {
			return true
		}
	}
	return false
}

// oidcLinkOrProvision links an OIDC subject to a users row by verified email
// (the family-logins email key), auto-provisioning on first login, and syncs
// the admin flag from the groups claim on every login.
func oidcLinkOrProvision(sub, email, name string, groups []string) (int64, error) {
	if sub == "" || email == "" {
		return 0, errOIDCIdentity
	}
	isAdmin := 0
	if oidcIsAdmin(groups) {
		isAdmin = 1
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM users WHERE oidc_sub = ?", sub).Scan(&id); err == nil {
		_, err := db.Exec("UPDATE users SET email = ?, is_admin = ?, auth_method = 'oidc' WHERE id = ?", email, isAdmin, id)
		return id, err
	}
	if err := db.QueryRow("SELECT id FROM users WHERE lower(email) = lower(?)", email).Scan(&id); err == nil {
		_, err := db.Exec("UPDATE users SET oidc_sub = ?, is_admin = ?, auth_method = 'oidc' WHERE id = ?", sub, isAdmin, id)
		return id, err
	}
	username := oidcUsernameForEmail(email)
	display := name
	if display == "" {
		display = username
	}
	res, err := db.Exec(`INSERT INTO users (username, display_name, email, color, oidc_sub, auth_method, is_admin)
		VALUES (?, ?, ?, ?, ?, 'oidc', ?)`, username, display, email, defaultColor, sub, isAdmin)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// oidcUsernameForEmail derives a unique users.username from the email local part.
func oidcUsernameForEmail(email string) string {
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
		if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", candidate).Scan(&count); err != nil || count == 0 {
			return candidate
		}
		candidate = strings.ToLower(strings.TrimSpace(base + "-" + itoa(i)))
	}
}

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

// errOIDCIdentity is returned when the IdP identity is missing subject/email.
type oidcError string

func (e oidcError) Error() string { return string(e) }

const errOIDCIdentity oidcError = "incomplete OIDC identity"
