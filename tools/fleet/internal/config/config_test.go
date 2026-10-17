package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validJSON(mutate func(string) string) string {
	base := `{
  "repos": [{"name":"r","owner":"o","checkout":"~/r","slots":{"impl":2,"merge":1}}],
  "roles": {
    "coordinator": {"prefer":["opencode","kimi"],"min_tier":"low"},
    "impl": {"prefer":["kimi","codex","claude"],"min_tier":"medium"},
    "merge": {"prefer":["claude"],"min_tier":"high"},
    "reviewer": {"prefer":["codex"],"min_tier":"high"}
  }
}`
	if mutate == nil {
		return base
	}
	return mutate(base)
}

func TestParseDefaults(t *testing.T) {
	t.Parallel()
	c, err := Parse([]byte(validJSON(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Thresholds.HangMinutes != 45 || time.Duration(c.Thresholds.MigrateAfter) != 2*time.Hour {
		t.Fatalf("defaults not applied: %+v", c.Thresholds)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	rep := func(old, new string) func(string) string {
		return func(s string) string { return strings.Replace(s, old, new, 1) }
	}
	withThr := func(thr string) func(string) string {
		return func(s string) string {
			return strings.TrimSuffix(strings.TrimSpace(s), "}") + `,"thresholds":` + thr + "}"
		}
	}
	tests := []struct {
		name   string
		mutate func(string) string
		want   string
	}{
		{"unknown top key", rep(`"repos"`, `"bogus":1,"repos"`), "unknown field"},
		{"unknown nested key", rep(`"owner":"o"`, `"owner":"o","extra":1`), "unknown field"},
		{"unknown harness", rep(`"opencode"`, `"gemini"`), `unknown harness "gemini"`},
		{"merge non-claude", rep(`"merge": {"prefer":["claude"]`, `"merge": {"prefer":["claude","codex"]`), "claude only"},
		{"bad tier", rep(`"min_tier":"low"`, `"min_tier":"ultra"`), `unknown tier "ultra"`},
		{"unknown role", rep(`"reviewer": {`, `"boss": {`), `unknown role "boss"`},
		{"missing role", rep(`"reviewer": {"prefer":["codex"],"min_tier":"high"}`, `"x": {"prefer":["codex"],"min_tier":"high"}`), "unknown role"},
		{"negative slots", rep(`"impl":2`, `"impl":-1`), "must not be negative"},
		{"unknown slot role", rep(`"impl":2`, `"nope":2`), `unknown role "nope"`},
		{"dup repo", rep(`"slots":{"impl":2,"merge":1}}`, `"slots":{}},{"name":"r","owner":"o","checkout":"~/r"}`), "duplicate repo"},
		{"empty prefer", rep(`["opencode","kimi"]`, `[]`), "at least one harness"},
		{"zero hang", withThr(`{"hang_minutes":0}`), "hang_minutes"},
		{"negative hang", withThr(`{"hang_minutes":-5}`), "hang_minutes"},
		{"zero migrate", withThr(`{"migrate_after":"0s"}`), "migrate_after"},
		{"bad duration", withThr(`{"migrate_after":"soon"}`), "invalid duration"},
		{"zero heavy", withThr(`{"heavy_slots":0}`), "heavy_slots"},
		{"critical above warn", withThr(`{"disk_warn_gb":10,"disk_critical_gb":20}`), "below disk_warn_gb"},
		{"trailing data", func(s string) string { return s + "{}" }, "trailing data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(validJSON(tt.mutate)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLoadCheckout(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "fleet.json")
	if err := os.WriteFile(cfgPath, []byte(validJSON(nil)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath, home); err == nil || !strings.Contains(err.Error(), "repos[r].checkout") {
		t.Fatalf("missing checkout must fail, got %v", err)
	}
	if err := os.Mkdir(filepath.Join(home, "r"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath, home); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(home, "absent.json"), home); err == nil {
		t.Fatal("missing file must fail")
	}
}
