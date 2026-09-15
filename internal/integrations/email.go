package integrations

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/smtp"

	"github.com/gin-gonic/gin"
	"traces/internal/httpx"
	"traces/internal/models"
)

func (s *Service) GetEmailConfig(c *gin.Context) {
	var cfg models.EmailConfig
	var port int
	err := s.db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&cfg.SMTPHost, &port, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err != nil {
		c.JSON(http.StatusOK, models.EmailConfig{SMTPPort: 587})
		return
	}
	cfg.SMTPPort = port
	c.JSON(http.StatusOK, cfg)
}

func (s *Service) SaveEmailConfig(c *gin.Context) {
	var cfg models.EmailConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}
	_, err := s.db.Exec(`UPDATE email_settings SET smtp_host=?, smtp_port=?, smtp_user=?, smtp_pass=?, from_addr=?, to_addr=? WHERE id=1`,
		cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.FromAddr, cfg.ToAddr)
	if err != nil {
		httpx.ServerError(c, err)
		return
	}
	if s.log != nil {
		s.log.Log("info", "email", "Email settings saved", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Service) TestEmail(c *gin.Context) {
	var cfg models.EmailConfig
	var port int
	err := s.db.QueryRow("SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr FROM email_settings WHERE id = 1").Scan(&cfg.SMTPHost, &port, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err != nil || cfg.SMTPHost == "" || cfg.ToAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email not configured"})
		return
	}
	cfg.SMTPPort = port
	subject := "TRACES Test Email"
	body := "This is a test email from TRACES. If you receive this, your email settings are working correctly."
	if err := SendEmail(cfg, cfg.ToAddr, subject, body); err != nil {
		if s.log != nil {
			s.log.Log("error", "email", "Email test failed: "+err.Error(), nil)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to send email"})
		return
	}
	if s.log != nil {
		s.log.Log("info", "email", "Email test sent successfully", nil)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Test email sent successfully"})
}

// SendEmail sends an email via SMTP.
func SendEmail(cfg models.EmailConfig, toAddr, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	msg := fmt.Appendf(nil, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", cfg.FromAddr, toAddr, subject, body)
	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	}
	if cfg.SMTPPort == 465 {
		tlsCfg := &tls.Config{ServerName: cfg.SMTPHost}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return err
		}
		defer client.Close()
		if auth != nil {
			if err = client.Auth(auth); err != nil {
				return err
			}
		}
		if err = client.Mail(cfg.FromAddr); err != nil {
			return err
		}
		if err = client.Rcpt(toAddr); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		_, err = w.Write(msg)
		if err != nil {
			return err
		}
		return w.Close()
	}
	return smtp.SendMail(addr, auth, cfg.FromAddr, []string{toAddr}, msg)
}
