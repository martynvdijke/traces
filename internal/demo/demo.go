// Package demo seeds an optional, clearly-labelled reviewer account.
//
// The account is intended for public demo deployments where visitors should be
// able to look around without registering. It is disabled by default and only
// enabled automatically when the instance is served from the canonical public
// host, unless DEMO_USER_ENABLED is set explicitly.
package demo

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Config describes the demo reviewer account.
type Config struct {
	Enabled     bool
	Username    string
	Password    string
	Email       string
	DisplayName string
	SeedContent bool
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

const publicDemoHost = "traces.vandijke.xyz"

// LoadConfigFromEnv reads the DEMO_USER_* variables and derives the default
// enabled state from TRACES_PUBLIC_URL. An explicit DEMO_USER_ENABLED value
// always wins over the derived default.
func LoadConfigFromEnv() Config {
	cfg := Config{
		Username:    envOr("DEMO_USER_USERNAME", "demo"),
		Password:    envOr("DEMO_USER_PASSWORD", "demo"),
		Email:       envOr("DEMO_USER_EMAIL", "demo@traces.local"),
		DisplayName: envOr("DEMO_USER_DISPLAY_NAME", "Demo User"),
		SeedContent: true,
	}

	if raw := strings.TrimSpace(os.Getenv("DEMO_USER_ENABLED")); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			cfg.Enabled = parsed
		}
	} else if u, err := url.Parse(os.Getenv("TRACES_PUBLIC_URL")); err == nil {
		cfg.Enabled = strings.EqualFold(u.Hostname(), publicDemoHost)
	}

	if raw := strings.TrimSpace(os.Getenv("DEMO_USER_SEED_CONTENT")); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			cfg.SeedContent = parsed
		}
	}

	// A passwordless demo account would be impossible to sign in to.
	if strings.TrimSpace(cfg.Password) == "" {
		cfg.Enabled = false
	}

	return cfg
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// Ensure creates or reconciles the demo account. It is safe to call on every
// startup: the account is only inserted when missing and existing accounts are
// never promoted to administrators or taken over from another owner.
func Ensure(db *sql.DB, cfg Config, logf func(string, ...any)) error {
	if !cfg.Enabled {
		return nil
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}

	username := strings.TrimSpace(cfg.Username)
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("invalid demo username %q", username)
	}
	if strings.TrimSpace(cfg.Password) == "" {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash demo password: %w", err)
	}

	var (
		userID       int64
		email        string
		passwordHash string
	)
	err = db.QueryRow(`SELECT id, COALESCE(email,''), COALESCE(password_hash,'')
		FROM users WHERE username = ?`, username).Scan(&userID, &email, &passwordHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result, insertErr := db.Exec(`INSERT INTO users
			(username, display_name, email, color, password_hash, auth_method, is_admin)
			VALUES (?, ?, ?, '#B5452B', ?, 'password', 0)`,
			username, cfg.DisplayName, cfg.Email, string(hash))
		if insertErr != nil {
			return fmt.Errorf("insert demo user: %w", insertErr)
		}
		userID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read demo user id: %w", err)
		}
	case err != nil:
		return fmt.Errorf("look up demo user: %w", err)
	default:
		if email != cfg.Email {
			logf("[demo] user %q already exists with a different email (%q); leaving it untouched", username, email)
			return nil
		}
		// is_admin is deliberately never included here: a demo account must
		// never be promoted (nor demoted) by this reconciler.
		if _, err := db.Exec(`UPDATE users SET display_name = ?, auth_method = 'password' WHERE id = ?`,
			cfg.DisplayName, userID); err != nil {
			return fmt.Errorf("update demo user: %w", err)
		}
		if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(cfg.Password)) != nil {
			if _, err := db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), userID); err != nil {
				return fmt.Errorf("reset demo password: %w", err)
			}
			logf("[demo] reset password for user %q", username)
		}
	}

	if cfg.SeedContent {
		if err := seedDemoContent(db, userID, logf); err != nil {
			return err
		}
	}
	return nil
}

// seedDemoContent inserts a handful of realistic, text-only memories, but only
// when the demo account has no events yet. This keeps re-seeded data from
// accumulating across restarts.
func seedDemoContent(db *sql.DB, userID int64, logf func(string, ...any)) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return fmt.Errorf("count demo events: %w", err)
	}
	if count > 0 {
		return nil
	}

	type seedEvent struct {
		title       string
		description string
		date        string
		location    string
		tags        string
		lat         float64
		lng         float64
	}
	seed := []seedEvent{
		{
			title:       "First light over the harbour",
			description: "An early walk along the waterfront before the city woke up.",
			date:        "2022-06-18",
			location:    "Rotterdam, Netherlands",
			tags:        "harbour,sunrise,walk",
			lat:         51.9217,
			lng:         4.4781,
		},
		{
			title:       "Autumn picnic in the park",
			description: "Blankets, soup and the last warm afternoon of the season.",
			date:        "2023-10-08",
			location:    "Vondelpark, Amsterdam",
			tags:        "autumn,picnic,friends",
			lat:         52.3580,
			lng:         4.8686,
		},
		{
			title:       "Weekend in the Ardennes",
			description: "A quiet cabin, long forest walks and no phone reception.",
			date:        "2024-04-20",
			location:    "Durbuy, Belgium",
			tags:        "hiking,weekend,nature",
			lat:         50.3526,
			lng:         5.4563,
		},
		{
			title:       "Summer evening at the lake",
			description: "Swimming until sunset and dinner cooked outdoors.",
			date:        "2025-07-26",
			location:    "Lake Constance, Germany",
			tags:        "lake,summer,sunset",
			lat:         47.6592,
			lng:         9.1750,
		},
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin demo seed: %w", err)
	}
	defer tx.Rollback()

	for _, event := range seed {
		if _, err := tx.Exec(`INSERT INTO timeline_events
			(title, description, event_date, location, media_type, media_url, thumbnail,
			 media_caption, tags, sort_order, is_public, is_favorite, person_id, latitude,
			 longitude, recurring, weather_data, user_id, event_start_time, event_end_time,
			 deleted_at, source, source_ref)
			VALUES (?, ?, ?, ?, '', '', '', '', ?, 0, 0, 0, NULL, ?, ?, '', '', ?, '', '', '', '', '')`,
			event.title, event.description, event.date, event.location, event.tags,
			event.lat, event.lng, userID); err != nil {
			return fmt.Errorf("seed demo event %q: %w", event.title, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit demo seed: %w", err)
	}
	logf("[demo] seeded %d sample memories for demo account", len(seed))
	return nil
}
