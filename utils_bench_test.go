package main

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"

	"traces/internal/models"
)

func TestHashPassword(t *testing.T) {
	tests := []struct {
		password string
	}{
		{"password123"},
		{""},
		{"verylongpasswordthat exceeds the normal length"},
	}

	for _, tt := range tests {
		t.Run("hash_"+tt.password, func(t *testing.T) {
			hash, err := bcrypt.GenerateFromPassword([]byte(tt.password), bcrypt.DefaultCost)
			if err != nil {
				t.Fatalf("bcrypt.GenerateFromPassword(%q) failed: %v", tt.password, err)
			}
			if len(hash) == 0 {
				t.Errorf("bcrypt hash is empty for %q", tt.password)
			}
		})
	}

	t.Run("verify", func(t *testing.T) {
		pwd := "testpassword"
		hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := bcrypt.CompareHashAndPassword(hash, []byte(pwd)); err != nil {
			t.Error("bcrypt verification failed")
		}
		if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong")); err == nil {
			t.Error("bcrypt should reject wrong password")
		}
	})
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{[]string{"a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{[]string{}, []string{}},
		{[]string{"same", "same", "same"}, []string{"same"}},
	}
	for _, tt := range tests {
		result := uniqueStrings(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("uniqueStrings(%v) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("uniqueStrings(%v) = %v, want %v", tt.input, result, tt.expected)
				break
			}
		}
	}
}

func BenchmarkHashPassword(b *testing.B) {
	password := "benchmark_password"
	for i := 0; i < b.N; i++ {
		bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	}
}

func BenchmarkEscapeHtml(b *testing.B) {
	text := "<script>alert('xss')</script>"
	for i := 0; i < b.N; i++ {
		models.EscapeHtml(text)
	}
}

func BenchmarkGetMediaIcon(b *testing.B) {
	types := []string{"image", "video", "audio"}
	for i := 0; i < b.N; i++ {
		models.GetMediaIcon(types[i%len(types)])
	}
}
