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

func confirmed(h config.Harness, reset time.Time) Result {
	return Result{Harness: h, Verified: true, Matched: true, Confirmed: true, Signature: "sig", ResetAt: reset}
}

func TestStateMachine(t *testing.T) {
	t.Parallel()
	s := &Set{}
	if s.Get(config.Claude).Kind != Unknown {
		t.Fatal("empty set must be unknown")
	}
	// Bootstrap: a live agent working with no match makes an unknown harness ok.
	s.Observe(Result{Harness: config.Claude}, "working", now)
	if e := s.Get(config.Claude); e.Kind != OK || !e.VerifiedAt.Equal(now) {
		t.Fatalf("bootstrap: %+v", e)
	}
	// A working agent that shows a match proves nothing.
	s.Observe(Result{Harness: config.Kimi, Matched: true}, "working", now)
	if s.Get(config.Kimi).Kind != Unknown {
		t.Fatal("working with a match must stay unknown")
	}
	// An idle agent with no match proves nothing either.
	s.Observe(Result{Harness: config.Kimi}, "idle", now)
	if s.Get(config.Kimi).Kind != Unknown {
		t.Fatal("idle must stay unknown")
	}
	reset := now.Add(3 * time.Hour)
	s.Observe(confirmed(config.Claude, reset), "blocked", now)
	if e := s.Get(config.Claude); e.Kind != Throttled || !e.Until.Equal(reset) || !e.Since.Equal(now) {
		t.Fatalf("throttle: %+v", e)
	}
	// Persisting throttle keeps Since; later reset is taken.
	later := now.Add(10 * time.Minute)
	s.Observe(confirmed(config.Claude, reset), "blocked", later)
	if e := s.Get(config.Claude); !e.Since.Equal(now) {
		t.Fatalf("since must persist: %+v", e)
	}
	if s.MigrateDue(config.Claude, now.Add(119*time.Minute), 2*time.Hour) || !s.MigrateDue(config.Claude, now.Add(2*time.Hour), 2*time.Hour) {
		t.Fatal("migrate_after boundary")
	}
	if s.MigrateDue(config.Codex, now.Add(48*time.Hour), time.Hour) {
		t.Fatal("unknown harness never migrates")
	}
	// Expiry becomes unknown, not ok.
	if got := s.ExpireThrottles(reset.Add(-time.Second)); len(got) != 0 {
		t.Fatalf("early expiry: %v", got)
	}
	if got := s.ExpireThrottles(reset); !reflect.DeepEqual(got, []config.Harness{config.Claude}) || s.Get(config.Claude).Kind != Unknown {
		t.Fatalf("expiry: %v %+v", got, s.Get(config.Claude))
	}
	// A working agent clears a throttle early; exhausted does not clear that way.
	s.Observe(confirmed(config.Claude, reset.Add(time.Hour)), "idle", reset)
	s.Observe(Result{Harness: config.Claude}, "working", reset.Add(time.Minute))
	if s.Get(config.Claude).Kind != OK {
		t.Fatal("working agent must clear a throttle")
	}
	s.Put(config.Codex, Entry{Kind: Exhausted})
	s.Observe(Result{Harness: config.Codex}, "working", now)
	if s.Get(config.Codex).Kind != Exhausted {
		t.Fatal("exhausted must not clear on a working observation")
	}
}

func TestUnparsedResetFallsBackToWait(t *testing.T) {
	t.Parallel()
	s := &Set{}
	s.Observe(confirmed(config.Claude, time.Time{}), "blocked", now)
	if e := s.Get(config.Claude); !e.Until.Equal(now.Add(UnparsedWait)) {
		t.Fatalf("%+v", e)
	}
	s2 := &Set{}
	s2.Observe(confirmed(config.Claude, now.Add(-time.Hour)), "blocked", now)
	if e := s2.Get(config.Claude); !e.Until.Equal(now.Add(UnparsedWait)) {
		t.Fatalf("a reset in the past is not usable: %+v", e)
	}
}

func TestSaveLoad(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "limits.json")
	if s, err := Load(p); err != nil || len(s.Harnesses) != 0 {
		t.Fatalf("missing file: %v %v", s, err)
	}
	s := &Set{}
	s.Put(config.Claude, Entry{Kind: Throttled, Until: now, Since: now.Add(-time.Hour)})
	s.Put(config.Kimi, Entry{Kind: OK, VerifiedAt: now})
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !got.Get(config.Claude).Until.Equal(now) || got.Get(config.Kimi).Kind != OK {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for name, body := range map[string]string{
		"unknown field":   `{"harnesses":{},"extra":1}`,
		"bad harness":     `{"harnesses":{"gemini":{"kind":"ok"}}}`,
		"bad kind":        `{"harnesses":{"claude":{"kind":"fine"}}}`,
		"throttle no end": `{"harnesses":{"claude":{"kind":"throttled-until"}}}`,
		"not json":        `nope`,
	} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	bad := &Set{Harnesses: map[config.Harness]Entry{config.Claude: {Kind: "fine"}}}
	if err := bad.Save(p); err == nil {
		t.Error("save must validate")
	}
}
