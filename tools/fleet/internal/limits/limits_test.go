package limits

import (
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func TestParseReset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		want time.Time
		text string
	}{
		{"You've hit your limit · resets 3pm (Europe/Bucharest)", time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), "3pm (Europe/Bucharest)"},
		{"limit · resets 5pm (UTC)", time.Date(2026, 10, 17, 17, 0, 0, 0, time.UTC), "5pm (UTC)"},
		{"limit · resets 12am (UTC)", time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), "12am (UTC)"},
		{"limit · resets 12:30pm (UTC)", time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC), "12:30pm (UTC)"},
		{"weekly limit · resets Oct 20, 9:30am (UTC)", time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC), "Oct 20, 9:30am (UTC)"},
		{"limit · resets Jan 2 at 8am (UTC)", time.Date(2027, 1, 2, 8, 0, 0, 0, time.UTC), "Jan 2 at 8am (UTC)"},
		{"limit · resets soon", time.Time{}, "soon"},
		{"limit · resets 3pm (Nowhere/Land)", time.Time{}, "3pm (Nowhere/Land)"},
		{"limit · resets 13pm (UTC)", time.Time{}, "13pm (UTC)"},
		{"no reset here", time.Time{}, "here"},
		{"nothing to see", time.Time{}, ""},
	}
	for _, tt := range tests {
		text, got := parseReset(tt.line, now)
		if !got.Equal(tt.want) || text != tt.text {
			t.Errorf("%q: got %q %v, want %q %v", tt.line, text, got, tt.text, tt.want)
		}
	}
}

func TestUnverifiedDetectorsNeverConfirm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		h    config.Harness
		text string
	}{
		{config.Kimi, "  Paused after provider rate limit\n"},
		{config.Kimi, "Error: You exceeded your current quota, please check your plan\n"},
		{config.OpenCode, "FreeUsageLimitError: free usage exceeded\n"},
	}
	for _, tt := range tests {
		first := Detect(Input{Harness: tt.h, Status: "idle", Detection: tt.text, Now: now})
		r := Detect(Input{Harness: tt.h, Status: "idle", Detection: tt.text, PrevSignature: first.Signature, Now: now})
		if r.Verified || r.Confirmed || !r.Candidate || !r.Matched {
			t.Errorf("%s %q: %+v", tt.h, tt.text, r)
		}
		w := Detect(Input{Harness: tt.h, Status: "working", Detection: tt.text, PrevSignature: first.Signature, Now: now})
		if w.Candidate {
			t.Errorf("%s: working agent must not be a candidate", tt.h)
		}
	}
	if r := Detect(Input{Harness: config.Kimi, Status: "idle", Detection: "all good\n", Now: now}); r.Matched || r.Verified {
		t.Errorf("no match expected: %+v", r)
	}
	if r := Detect(Input{Harness: config.Codex, Status: "idle", Detection: "rate limit\n", Now: now}); r.Matched || r.Reason == "" {
		t.Errorf("codex has no screen detector: %+v", r)
	}
}
