package limits

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func detect(t *testing.T, fx, status, prev string) Result {
	t.Helper()
	return Detect(Input{Harness: config.Claude, Status: status, Detection: fixture(t, fx), PrevSignature: prev, Now: now})
}

func TestClaudeConfirmedNeedsAllConditions(t *testing.T) {
	t.Parallel()
	first := detect(t, "claude/limit_prompt.txt", "blocked", "")
	if !first.Matched || !first.Chrome || first.Confirmed || !first.Verified {
		t.Fatalf("first sighting must match but not confirm: %+v", first)
	}
	// 3pm in Bucharest (UTC+3 until Oct 25) is 12:00 UTC, which is not after now, so tomorrow.
	if want := now.Add(24 * time.Hour); !first.ResetAt.Equal(want) {
		t.Fatalf("reset %v, want %v", first.ResetAt, want)
	}
	for _, status := range []string{"blocked", "idle", "done"} {
		if r := detect(t, "claude/limit_prompt.txt", status, first.Signature); !r.Confirmed {
			t.Errorf("status %s: want confirmed, got %+v", status, r)
		}
	}
}

func TestClaudeNegatives(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, fx, status string }{
		{"approval dialog", "claude/approval_dialog.txt", "blocked"},
		{"transcript discussing 429s and limits", "claude/discusses_limits.txt", "idle"},
		{"quoted limit line without the menu", "claude/quoted_limit_no_menu.txt", "idle"},
		{"working agent showing the limit text", "claude/limit_prompt.txt", "working"},
		{"unknown status", "claude/limit_prompt.txt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Give it the best case for the previous tick so only the guard under test can refuse.
			prev := detect(t, tt.fx, "blocked", "").Signature
			if r := detect(t, tt.fx, tt.status, prev); r.Confirmed || r.Candidate {
				t.Fatalf("must not confirm: %+v", r)
			}
		})
	}
}

func TestClaudeSignatureMustRepeat(t *testing.T) {
	t.Parallel()
	if r := detect(t, "claude/limit_prompt.txt", "blocked", "claude:something else"); r.Confirmed {
		t.Fatalf("a different previous match must not confirm: %+v", r)
	}
}

func TestMatchOutsideBottomLinesIgnored(t *testing.T) {
	t.Parallel()
	text := fixture(t, "claude/limit_prompt.txt") + strings.Repeat("later output\n", BottomLines)
	r := Detect(Input{Harness: config.Claude, Status: "idle", Detection: text, Now: now})
	if r.Matched {
		t.Fatalf("scrolled-off match counted: %+v", r)
	}
}

func TestClaudeWaitAnswerSafe(t *testing.T) {
	t.Parallel()
	ok, keys := ClaudeWaitAnswerSafe(fixture(t, "claude/limit_prompt.txt"))
	if !ok || !reflect.DeepEqual(keys, []string{"down", "enter"}) {
		t.Fatalf("got %v %v", ok, keys)
	}
	keys[0] = "mutated"
	if ClaudeWaitKeys[0] != "down" {
		t.Fatal("returned keys alias the package variable")
	}
	for _, fx := range []string{"limit_prompt_cursor_on_2", "limit_prompt_cursor_on_3", "approval_dialog", "discusses_limits", "quoted_limit_no_menu"} {
		if ok, keys := ClaudeWaitAnswerSafe(fixture(t, "claude/"+fx+".txt")); ok || keys != nil {
			t.Errorf("%s: must be unsafe, got %v %v", fx, ok, keys)
		}
	}
	full := fixture(t, "claude/limit_prompt.txt")
	for _, drop := range []string{claudePrompt, claudeWaitOption, claudeCursorOnFirst} {
		if ok, _ := ClaudeWaitAnswerSafe(strings.Replace(full, drop, "", 1)); ok {
			t.Errorf("missing %q must be unsafe", drop)
		}
	}
	if ok, _ := ClaudeWaitAnswerSafe(""); ok {
		t.Error("empty screen must be unsafe")
	}
}

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
