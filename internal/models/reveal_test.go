package models

import (
	"strings"
	"testing"
	"time"
)

func TestParseRevealAt(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty stays unsealed", input: "", want: ""},
		{name: "whitespace is unsealed", input: "   ", want: ""},
		{name: "utc passes through", input: "2027-05-01T09:00:00Z", want: "2027-05-01T09:00:00Z"},
		{name: "offset is normalized to utc", input: "2027-05-01T11:00:00+02:00", want: "2027-05-01T09:00:00Z"},
		{name: "date only is rejected", input: "2027-05-01", wantErr: true},
		{name: "garbage is rejected", input: "next christmas", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRevealAt(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRevealAt(%q) = %q, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRevealAt(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseRevealAt(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatRevealAtIsUTCAndSecondPrecision(t *testing.T) {
	loc := time.FixedZone("CEST", 2*60*60)
	at := time.Date(2027, 5, 1, 11, 0, 0, 987654321, loc)

	if got, want := FormatRevealAt(at), "2027-05-01T09:00:00Z"; got != want {
		t.Fatalf("FormatRevealAt = %q, want %q", got, want)
	}
}

func TestIsSealed(t *testing.T) {
	now := time.Date(2027, 5, 1, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		revealAt string
		want     bool
	}{
		{name: "empty is never sealed", revealAt: "", want: false},
		{name: "future is sealed", revealAt: "2027-05-01T09:00:01Z", want: true},
		{name: "equal instant is open", revealAt: "2027-05-01T09:00:00Z", want: false},
		{name: "past is open", revealAt: "2027-04-30T09:00:00Z", want: false},
		{name: "unparseable is treated as open", revealAt: "not-a-time", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSealed(tc.revealAt, now); got != tc.want {
				t.Fatalf("IsSealed(%q) = %v, want %v", tc.revealAt, got, tc.want)
			}
		})
	}
}

func TestRevealVisibleSQL(t *testing.T) {
	unqualified := RevealVisibleSQL("")
	if strings.Contains(unqualified, "e.reveal_at") {
		t.Fatalf("unqualified fragment must not reference an alias: %q", unqualified)
	}
	if !strings.Contains(unqualified, "reveal_at") {
		t.Fatalf("fragment must reference reveal_at: %q", unqualified)
	}

	qualified := RevealVisibleSQL("e")
	for _, want := range []string{"e.reveal_at", "strftime("} {
		if !strings.Contains(qualified, want) {
			t.Fatalf("fragment %q missing %q", qualified, want)
		}
	}

	for _, fragment := range []string{unqualified, qualified} {
		if !strings.HasPrefix(fragment, " AND (") {
			t.Fatalf("fragment must be appended with AND: %q", fragment)
		}
		if strings.Count(fragment, "?") != 0 {
			t.Fatalf("fragment must not consume bind arguments: %q", fragment)
		}
	}
}
