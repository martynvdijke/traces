package models

import (
	"fmt"
	"strings"
	"time"
)

// RevealAtLayout is the storage layout for time-capsule timestamps. It is UTC
// RFC3339 with a fixed zone designator so stored values sort lexicographically
// and compare correctly against strftime('%Y-%m-%dT%H:%M:%SZ', 'now').
const RevealAtLayout = "2006-01-02T15:04:05Z"

// RevealVisibleSQL returns a SQL fragment that keeps only rows visible now.
// Rows with an empty reveal_at are always visible; a sealed row becomes visible
// once reveal_at is at or before the database's current UTC time.
//
// The fragment is self-contained and takes no bind argument, so it can be
// appended to any hand-written query without disturbing its argument list.
func RevealVisibleSQL(alias string) string {
	col := "reveal_at"
	if alias != "" {
		col = alias + ".reveal_at"
	}
	return fmt.Sprintf(
		" AND (COALESCE(%s, '') = '' OR %s <= strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ', 'now'))",
		col, col,
	)
}

// FormatRevealAt normalizes a timestamp for storage.
func FormatRevealAt(t time.Time) string {
	return t.UTC().Format(RevealAtLayout)
}

// ParseRevealAt validates and normalizes a client-supplied reveal timestamp.
// An empty value means "not sealed" and is returned unchanged.
func ParseRevealAt(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", err
	}
	return FormatRevealAt(t), nil
}

// IsSealed reports whether a stored reveal timestamp is still in the future.
func IsSealed(revealAt string, now time.Time) bool {
	if revealAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, revealAt)
	if err != nil {
		return false
	}
	return t.After(now.UTC())
}
