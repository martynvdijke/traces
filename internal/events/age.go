package events

import (
	"strconv"
	"strings"
	"time"

	"traces/internal/models"
)

// parseLenientDate parses dates in YYYY-MM-DD, YYYY-MM, or YYYY form.
func parseLenientDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// AgeAt computes whole years and remaining months between birthDate and
// eventDate using lenient date parsing.
func AgeAt(birthDate, eventDate string) (years, months int, ok bool) {
	b, okB := parseLenientDate(birthDate)
	e, okE := parseLenientDate(eventDate)
	if !okB || !okE || e.Before(b) {
		return 0, 0, false
	}
	years = e.Year() - b.Year()
	months = int(e.Month()) - int(b.Month())
	if e.Day() < b.Day() {
		months--
	}
	if months < 0 {
		years--
		months += 12
	}
	return years, months, true
}

// LifeGroup returns the display group for a milestone entry.
func LifeGroup(birthDate, eventDate string) string {
	e, okE := parseLenientDate(eventDate)
	if !okE {
		return "Undated"
	}
	b, okB := parseLenientDate(birthDate)
	if !okB {
		return strconv.Itoa(e.Year())
	}
	if e.Before(b) {
		return "Before"
	}
	totalMonths := (e.Year()-b.Year())*12 + int(e.Month()) - int(b.Month())
	if e.Day() < b.Day() {
		totalMonths--
	}
	if totalMonths < 0 {
		totalMonths = 0
	}
	return "Year " + strconv.Itoa(totalMonths/12+1)
}

// PersonMilestone is a timeline event enriched with milestone metadata.
type PersonMilestone struct {
	models.TimelineEvent
	AgeYears  *int   `json:"age_years,omitempty"`
	AgeMonths *int   `json:"age_months,omitempty"`
	Group     string `json:"group"`
}

// PersonEventsResponse is the enriched payload returned by the person events endpoint.
type PersonEventsResponse struct {
	Person models.Person     `json:"person"`
	Events []PersonMilestone `json:"events"`
}
