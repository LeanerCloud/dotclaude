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
