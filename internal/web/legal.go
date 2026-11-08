package web

import (
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// LegalConfig is the operator identity rendered into the public legal pages.
// Values come from LEGAL_* environment variables and deliberately have
// non-identifying fallbacks so an unconfigured instance still serves a page
// instead of failing.
type LegalConfig struct {
	ControllerName    string
	ControllerEmail   string
	ControllerAddress string
	HostingRegion     string
}

// LoadLegalConfigFromEnv reads the LEGAL_* variables with safe fallbacks.
func LoadLegalConfigFromEnv() LegalConfig {
	cfg := LegalConfig{
		ControllerName:    strings.TrimSpace(os.Getenv("LEGAL_CONTROLLER_NAME")),
		ControllerEmail:   strings.TrimSpace(os.Getenv("LEGAL_CONTROLLER_EMAIL")),
		ControllerAddress: strings.TrimSpace(os.Getenv("LEGAL_CONTROLLER_ADDRESS")),
		HostingRegion:     strings.TrimSpace(os.Getenv("LEGAL_HOSTING_REGION")),
	}
	if cfg.ControllerName == "" {
		cfg.ControllerName = "the operator of this TRACES instance"
	}
	if cfg.ControllerEmail == "" {
		cfg.ControllerEmail = "privacy@example.invalid"
	}
	if cfg.ControllerAddress == "" {
		cfg.ControllerAddress = "Not configured by the instance operator"
	}
	if cfg.HostingRegion == "" {
		cfg.HostingRegion = "European Union"
	}
	return cfg
}

// serveLegalPage renders a static HTML page, substituting the operator's
// identity. Only the LEGAL_* placeholders are replaced; all other markup is
// served as-is.
func (s *Service) serveLegalPage(c *gin.Context, name string) {
	raw, err := os.ReadFile(filepath.Join(s.basePath, "static", name))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	page := string(raw)
	replacements := map[string]string{
		"{{LEGAL_CONTROLLER_NAME}}":    html.EscapeString(s.legal.ControllerName),
		"{{LEGAL_CONTROLLER_EMAIL}}":   html.EscapeString(s.legal.ControllerEmail),
		"{{LEGAL_CONTROLLER_ADDRESS}}": html.EscapeString(s.legal.ControllerAddress),
		"{{LEGAL_HOSTING_REGION}}":     html.EscapeString(s.legal.HostingRegion),
	}
	for placeholder, value := range replacements {
		page = strings.ReplaceAll(page, placeholder, value)
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, page)
}

// PrivacyPage serves GET /privacy.
func (s *Service) PrivacyPage(c *gin.Context) { s.serveLegalPage(c, "privacy.html") }

// TermsPage serves GET /terms.
func (s *Service) TermsPage(c *gin.Context) { s.serveLegalPage(c, "terms.html") }

// AccountDeletePage serves GET /account/delete.
func (s *Service) AccountDeletePage(c *gin.Context) { s.serveLegalPage(c, "account_delete.html") }
