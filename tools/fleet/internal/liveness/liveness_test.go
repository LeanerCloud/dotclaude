package liveness

import (
	"strings"
	"testing"
)

func TestStallSignature(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		stalled    bool
	}{
		{"claude api error", "⏺ Working\n  ⎿  API Error: Connection reset\n\n╭────╮\n│ > │\n╰────╯\n", true},
		{"claude glyph connection error", "  ⎿  Connection error\n❯ \n", true},
		{"request timed out", "⏺ Request timed out\n", true},
		{"econnreset anywhere in line", "  ⎿  read tcp: ECONNRESET by peer\n", true},
		{"overloaded", "⎿ {\"type\":\"overloaded_error\"}\n", true},
		{"stream idle timeout", "⎿  Stream idle timeout\n", true},
		{"plain label without glyph", "Error: Network error talking to the API\n", true},
		{"bullet glyph", "● fetch failed\n", true},
		{"codex stream disconnected", "■ stream disconnected before completion\n", true},
		{"error scrolled above the tail", "⎿  API Error\n" + strings.Repeat("line\n", 8), false},
		{"prose mentioning an error", "I fixed the API Error handling in the client\n", false},
		{"clean", "⏺ Done\n❯ \n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StallSignature(tt.text)
			if (got != "") != tt.stalled {
				t.Fatalf("got %q, stalled=%v", got, tt.stalled)
			}
		})
	}
	a, b := StallSignature("⎿  API Error\n"), StallSignature("⎿  API Error\n")
	if a == "" || a != b || a == StallSignature("⎿  API Error: other\n") {
		t.Fatalf("signature must be stable and tail-specific: %q %q", a, b)
	}
}
