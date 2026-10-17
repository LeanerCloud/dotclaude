package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleLedger() *Ledger {
	return &Ledger{Slots: []Slot{{
		Name: "cc-impl", Repo: "cloud-commitments-platform", Role: "impl", Harness: "kimi",
		SessionID: "00000000-0000-4000-8000-000000000001", Pane: "w1:p1", Cwd: "~/x", HandoffPath: "~/h.md",
		Status: Active, LastSeq: 5, LastRevision: 2, LastProgress: time.Date(2026, 10, 17, 1, 2, 3, 0, time.UTC),
	}}}
}

func TestLedgerRoundTrip(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "slots.json")
	if err := WriteLedger(p, sampleLedger()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLedger(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slots[0] != sampleLedger().Slots[0] {
		t.Fatalf("round trip mismatch: %+v", got.Slots[0])
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestLedgerValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Ledger)
		want   string
	}{
		{"bad status", func(l *Ledger) { l.Slots[0].Status = "running" }, "unknown status"},
		{"empty status", func(l *Ledger) { l.Slots[0].Status = "" }, "unknown status"},
		{"harness in name", func(l *Ledger) { l.Slots[0].Name = "Cc Impl" }, "invalid name"},
		{"dup", func(l *Ledger) { l.Slots = append(l.Slots, l.Slots[0]) }, "duplicate"},
		{"no repo", func(l *Ledger) { l.Slots[0].Repo = "" }, "repo is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := sampleLedger()
			tt.mutate(l)
			p := filepath.Join(t.TempDir(), "slots.json")
			if err := WriteLedger(p, l); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("write: want %q, got %v", tt.want, err)
			}
			if _, err := os.Stat(p); err == nil {
				t.Fatal("invalid ledger was written")
			}
		})
	}
}

func TestReadLedgerRejects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown.json":  `{"slots":[],"extra":1}`,
		"trailing.json": `{"slots":[]} {}`,
		"garbage.json":  `{"slots":[`,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadLedger(p); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := ReadLedger(filepath.Join(dir, "absent.json")); err == nil {
		t.Error("missing file must fail")
	}
}

// A crash between writing the temp file and the rename must leave the old ledger valid.
func TestCrashBeforeRenameKeepsOldFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "slots.json")
	if err := WriteLedger(p, sampleLedger()); err != nil {
		t.Fatal(err)
	}
	next := sampleLedger()
	next.Slots[0].Status = Retired
	data, _ := json.Marshal(next)
	tmp, err := writeTmp(p, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(tmp) != dir {
		t.Fatalf("temp file %s not in target dir", tmp)
	}
	got, err := ReadLedger(p)
	if err != nil || got.Slots[0].Status != Active {
		t.Fatalf("old file damaged: %v %+v", err, got)
	}
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	if got, _ = ReadLedger(p); got.Slots[0].Status != Retired {
		t.Fatal("rename did not publish new content")
	}
}

func TestAtomicWriteLeavesNoTemp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	for i := 0; i < 3; i++ {
		if err := WriteFileAtomic(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "nodir", "f"), nil, 0o600); err == nil {
		t.Fatal("want error for missing dir")
	}
}

const goodHandoff = `# cc-impl handoff
Updated: 2026-10-17T10:00:00Z
## Current item
LeanerCloud/cloud-commitments-platform#42, fix thing
## Worktree
/home/x/wt
## Branch
fix/thing
## PR
LeanerCloud/cloud-commitments-platform#43
## Head SHA
0123456789abcdef0123456789abcdef01234567
## Claims held
- issue LeanerCloud/cloud-commitments-platform#42
- claim-pr-cloud-commitments-platform-43.txt
## Next action
wait for CI
## Blockers
none
## Queue
- #44 after this
`

func TestHandoffParse(t *testing.T) {
	t.Parallel()
	h, err := ParseHandoff(goodHandoff)
	if err != nil {
		t.Fatal(err)
	}
	if h.Slot != "cc-impl" || h.PR == "" || h.Blockers != "" || len(h.Queue) != 1 || h.Idle() {
		t.Fatalf("bad parse: %+v", h)
	}
	want := []Claim{{ClaimIssue, "LeanerCloud/cloud-commitments-platform", 42}, {ClaimPR, "cloud-commitments-platform", 43}}
	if len(h.Claims) != 2 || h.Claims[0] != want[0] || h.Claims[1] != want[1] {
		t.Fatalf("claims: %+v", h.Claims)
	}
	out, err := h.Format()
	if err != nil {
		t.Fatal(err)
	}
	if out != goodHandoff {
		t.Fatalf("format round trip differs:\n%s", out)
	}
}

func TestHandoffIdle(t *testing.T) {
	t.Parallel()
	h := &Handoff{Slot: "cc-impl", Updated: time.Now(), NextAction: "await work"}
	out, err := h.Format()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseHandoff(out)
	if err != nil || !back.Idle() || len(back.Claims) != 0 {
		t.Fatalf("idle round trip: %v %+v", err, back)
	}
}

func TestHandoffRejects(t *testing.T) {
	t.Parallel()
	rep := func(old, new string) string { return strings.Replace(goodHandoff, old, new, 1) }
	tests := []struct{ name, in, want string }{
		{"empty", "", "empty"},
		{"bad title", rep("# cc-impl handoff", "# handoff"), "want title"},
		{"no updated", rep("Updated: 2026-10-17T10:00:00Z\n", ""), "Updated"},
		{"bad updated", rep("2026-10-17T10:00:00Z", "yesterday"), "Updated line"},
		{"missing Blockers", rep("## Blockers\nnone\n", ""), `missing heading "Blockers"`},
		{"missing Claims", rep("## Claims held\n", "## Claims\n"), `missing heading "Claims held"`},
		{"duplicate", rep("## Branch", "## PR\nx\n## Branch"), "duplicate heading"},
		{"unknown heading", goodHandoff + "## Extra\nx\n", `unknown heading "Extra"`},
		{"out of order", rep("## Branch\nfix/thing\n## PR\nLeanerCloud/cloud-commitments-platform#43\n", "## PR\nLeanerCloud/cloud-commitments-platform#43\n## Branch\nfix/thing\n"), "out of order"},
		{"bad claim", rep("- issue LeanerCloud/cloud-commitments-platform#42", "- issue 42"), "unrecognised claim"},
		{"claim file bad n", rep("claim-pr-cloud-commitments-platform-43.txt", "claim-pr-cloud-commitments-platform-x.txt"), "unrecognised claim"},
		{"empty next action", rep("wait for CI", ""), "Next action must not be empty"},
		{"text before heading", rep("Updated: 2026-10-17T10:00:00Z\n", "Updated: 2026-10-17T10:00:00Z\nstray\n"), "before the first heading"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseHandoff(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}

func TestHandoffFormatRejectsHeadingInjection(t *testing.T) {
	t.Parallel()
	h := &Handoff{Slot: "a", NextAction: "x\n## Blockers\nnone"}
	if _, err := h.Format(); err == nil {
		t.Fatal("want error")
	}
	if _, err := (&Handoff{Slot: "Bad Slot", NextAction: "x"}).Format(); err == nil {
		t.Fatal("want invalid slot error")
	}
}

func TestParseClaim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want Claim
		ok   bool
	}{
		{"issue a/b#1", Claim{ClaimIssue, "a/b", 1}, true},
		{"claim-pr-cli-7.txt", Claim{ClaimPR, "cli", 7}, true},
		{"claim-pr-cloud-commitments-go-12.txt", Claim{ClaimPR, "cloud-commitments-go", 12}, true},
		{"issue a/b", Claim{}, false},
		{"claim-pr-cli.txt", Claim{}, false},
		{"pr a/b#1", Claim{}, false},
	}
	for _, tt := range tests {
		got, err := ParseClaim(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("%q: got %+v err=%v", tt.in, got, err)
		}
		if tt.ok && got.String() != tt.in {
			t.Errorf("%q: String() = %q", tt.in, got.String())
		}
	}
}
