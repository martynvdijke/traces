package models

import "testing"

func TestEscapeHtml(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{"a & b", "a &amp; b"},
		{`"quotes"`, "&quot;quotes&quot;"},
		{"it's", "it&#039;s"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := EscapeHtml(tt.input); got != tt.expected {
				t.Errorf("EscapeHtml(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetMediaIcon(t *testing.T) {
	tests := []struct{ mediaType, expected string }{
		{"video", "fa-solid fa-video"},
		{"audio", "fa-solid fa-music"},
		{"image", "fa-solid fa-image"},
		{"boardgame", "fa-solid fa-dice"},
		{"unknown", "fa-solid fa-image"},
		{"", "fa-solid fa-image"},
	}
	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			if got := GetMediaIcon(tt.mediaType); got != tt.expected {
				t.Errorf("GetMediaIcon(%q) = %q, want %q", tt.mediaType, got, tt.expected)
			}
		})
	}
}

func TestFormatDate(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"2026-01-01", "Jan 1"},
		{"2026-07-15", "Jul 15"},
		{"2026-12-25", "Dec 25"},
		{"invalid", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := FormatDate(tt.input); got != tt.expected {
				t.Errorf("FormatDate(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
	t.Run("leap_year", func(t *testing.T) {
		if got := FormatDate("2024-02-29"); got == "invalid" {
			t.Error("should handle leap year")
		}
	})
}
