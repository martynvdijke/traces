package events

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"traces/internal/auth"
	"traces/internal/integrations"
	"traces/internal/models"
)

// CapsuleSummary is the only thing the capsules view may reveal about a sealed
// event: its title, media type, reveal moment and a live countdown. Body,
// media, location, people and tags stay hidden until the capsule opens.
type CapsuleSummary struct {
	ID                 int    `json:"id"`
	Title              string `json:"title"`
	MediaType          string `json:"media_type"`
	RevealAt           string `json:"reveal_at"`
	CreatedAt          string `json:"created_at"`
	SecondsUntilReveal int64  `json:"seconds_until_reveal"`
}

type capsuleCandidate struct {
	id     int
	title  string
	userID int
}

// GetCapsules lists the sealed capsules visible to the current user: their own,
// or every user's for an administrator.
func (s *Service) GetCapsules(c *gin.Context) {
	cu := auth.GetCurrentUser(c)
	now := time.Now().UTC()

	query := `SELECT id, title, COALESCE(media_type, ''), COALESCE(reveal_at, ''), COALESCE(created_at, ''), COALESCE(user_id, 0)
		FROM timeline_events
		WHERE (deleted_at IS NULL OR deleted_at = '')
		  AND COALESCE(reveal_at, '') > strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`
	args := make([]any, 0, 1)
	if !cu.IsAdmin {
		query += " AND COALESCE(user_id, 0) = ?"
		args = append(args, cu.ID)
	}
	query += " ORDER BY reveal_at ASC"

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load capsules"})
		return
	}
	defer rows.Close()

	capsules := make([]CapsuleSummary, 0)
	for rows.Next() {
		var cs CapsuleSummary
		var userID int
		if err := rows.Scan(&cs.ID, &cs.Title, &cs.MediaType, &cs.RevealAt, &cs.CreatedAt, &userID); err != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339, cs.RevealAt); err == nil {
			cs.SecondsUntilReveal = int64(t.Sub(now).Seconds())
		}
		capsules = append(capsules, cs)
	}

	c.JSON(http.StatusOK, gin.H{"capsules": capsules, "total": len(capsules)})
}

// UnsealCapsule opens a sealed capsule early. Only the owner or an
// administrator may do this, and only deliberately.
func (s *Service) UnsealCapsule(c *gin.Context) {
	var req struct {
		ID int `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	cu := auth.GetCurrentUser(c)
	var title, revealAt string
	var ownerID int
	err := s.DB.QueryRow(
		`SELECT title, COALESCE(reveal_at, ''), COALESCE(user_id, 0)
		 FROM timeline_events
		 WHERE id = ? AND (deleted_at IS NULL OR deleted_at = '')`,
		req.ID,
	).Scan(&title, &revealAt, &ownerID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Event not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load capsule"})
		return
	}
	if !cu.IsAdmin && int64(ownerID) != cu.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "You may only open your own capsules"})
		return
	}
	if revealAt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Event is not a sealed capsule"})
		return
	}

	now := models.FormatRevealAt(time.Now())
	if _, err := s.DB.Exec(
		`UPDATE timeline_events SET reveal_at = '', revealed_at = ? WHERE id = ?`,
		now, req.ID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open capsule"})
		return
	}

	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification(
			"Time capsule opened: "+title,
			fmt.Sprintf("%q was opened early.", title),
		)
	}

	c.JSON(http.StatusOK, gin.H{"id": req.ID, "title": title, "revealed_at": now})
}

// RevealDueCapsules marks every due capsule as revealed and notifies its owner
// once. Each capsule is claimed with a conditional UPDATE, so concurrent or
// restarted sweeps can never record or notify the same capsule twice.
func (s *Service) RevealDueCapsules() (int, error) {
	rows, err := s.DB.Query(`
		SELECT id, title, COALESCE(user_id, 0)
		FROM timeline_events
		WHERE COALESCE(reveal_at, '') <> ''
		  AND COALESCE(revealed_at, '') = ''
		  AND reveal_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		ORDER BY reveal_at ASC`)
	if err != nil {
		return 0, err
	}
	candidates := make([]capsuleCandidate, 0)
	for rows.Next() {
		var cand capsuleCandidate
		if err := rows.Scan(&cand.id, &cand.title, &cand.userID); err == nil {
			candidates = append(candidates, cand)
		}
	}
	rows.Close()

	opened := 0
	for _, cand := range candidates {
		now := models.FormatRevealAt(time.Now())
		res, err := s.DB.Exec(`
			UPDATE timeline_events
			SET revealed_at = ?
			WHERE id = ?
			  AND COALESCE(revealed_at, '') = ''
			  AND COALESCE(reveal_at, '') <> ''
			  AND reveal_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
			now, cand.id)
		if err != nil {
			continue
		}
		if affected, err := res.RowsAffected(); err != nil || affected != 1 {
			continue
		}
		opened++
		s.notifyCapsuleOpened(cand)
	}
	return opened, nil
}

// notifyCapsuleOpened announces a due capsule through the already configured
// channels. Unconfigured channels are silent no-ops.
func (s *Service) notifyCapsuleOpened(cand capsuleCandidate) {
	title := "Time capsule opened: " + cand.title
	body := fmt.Sprintf("Your time capsule %q has reached its reveal date and is now visible.", cand.title)

	if s.Integrations != nil {
		s.Integrations.SendGotifyNotification(title, body)
	}

	if cand.userID == 0 {
		return
	}
	var cfg models.EmailConfig
	err := s.DB.QueryRow(
		`SELECT smtp_host, smtp_port, smtp_user, smtp_pass, from_addr, to_addr
		 FROM email_settings WHERE id = 1`,
	).Scan(&cfg.SMTPHost, &cfg.SMTPPort, &cfg.SMTPUser, &cfg.SMTPPass, &cfg.FromAddr, &cfg.ToAddr)
	if err != nil || cfg.SMTPHost == "" {
		return
	}
	var to string
	if err := s.DB.QueryRow(`SELECT COALESCE(email, '') FROM users WHERE id = ?`, cand.userID).Scan(&to); err != nil || to == "" {
		return
	}
	go func() {
		_ = integrations.SendEmail(cfg, to, title, body)
	}()
}

// StartCapsuleRevealWorker sweeps due capsules every minute until ctx is
// cancelled, catching up on anything that came due while the server was down.
func (s *Service) StartCapsuleRevealWorker(ctx context.Context) {
	_, _ = s.RevealDueCapsules()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RevealDueCapsules(); err != nil {
				continue
			}
		}
	}
}
