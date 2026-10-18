package fleetd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/limits"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/liveness"
)

func TestObserverMemoryArtifactsReadBoundedBytes(t *testing.T) {
	t.Parallel()
	_, paths := observerOutputFixture(t)
	out, err := openObserverOutput(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := out.close(); err != nil {
			t.Error(err)
		}
	}()
	id := strings.Repeat("a", 64)
	want := fmt.Appendf(nil, `{"Liveness":{"slots":{%[1]q:{}}},"Signatures":{%[1]q:""},"Limits":{}}`, id)
	if err := out.root.WriteFile(observerMemory, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readObserverArtifact(out.root, observerMemory)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("memory bytes were not read: got %q, err %v", got, err)
	}
	if got, err := out.loadMemory(); err != nil || got.Signatures[id] != "" || len(got.Liveness.Slots) != 1 {
		t.Fatal("valid empty signature and zero slot refused", err)
	}
}

func TestObserverMemoryRefusesUnsafeArtifacts(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "dangling-link", "hardlink", "fifo", "mode", "root-swap"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			dir, paths := observerOutputFixture(t)
			out, err := openObserverOutput(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.close() }()
			valid := []byte(`{"Liveness":{},"Signatures":{},"Limits":{}}`)
			if err := out.root.WriteFile(observerJournal, valid, 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				err = out.root.Symlink(observerJournal, observerMemory)
			case "dangling-link":
				err = out.root.Symlink("absent", observerMemory)
			case "hardlink":
				err = out.root.Link(observerJournal, observerMemory)
			case "fifo":
				err = syscall.Mkfifo(dir+"/"+observerMemory, 0o600)
			case "mode":
				err = out.root.WriteFile(observerMemory, valid, 0o644)
			case "root-swap":
				err = out.root.WriteFile(observerMemory, valid, 0o600)
				if err == nil {
					err = os.Rename(dir, dir+"-saved")
				}
				if err == nil {
					err = os.Mkdir(dir, 0o700)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := out.loadMemory(); err == nil {
				t.Fatal("unsafe memory accepted")
			} else if kind == "fifo" && !strings.Contains(err.Error(), "regular") {
				t.Fatal("FIFO regular-file guard bypassed", err)
			}
			after, err := out.root.ReadFile(observerJournal)
			if err != nil || !bytes.Equal(after, valid) {
				t.Fatal("existing artifact changed", err)
			}
		})
	}
}

func TestObserverMemoryEncodingEnforcesRoundTripBound(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("a", 64)
	mem := ObserverMemory{Signatures: map[string]string{id: ""}}
	base, err := encodeObserverMemory(mem)
	if err != nil {
		t.Fatal(err)
	}
	mem.Signatures[id] = strings.Repeat(" ", maxObserverMemoryBytes-len(base))
	data, err := encodeObserverMemory(mem)
	if err != nil || len(data) != maxObserverMemoryBytes {
		t.Fatal("exact boundary refused", len(data), err)
	}
	mem.Signatures[id] += " "
	if _, err := encodeObserverMemory(mem); err == nil {
		t.Fatal("oversized encoding accepted")
	}
	mem.Signatures = nil
	if _, err := encodeObserverMemory(mem); err == nil {
		t.Fatal("null signatures encoding accepted")
	}
	var parsed ObserverMemory
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Signatures[id] == "" {
		t.Fatal("boundary encoding invalid", err)
	}
}

func TestObserverMemoryRoundTripsAndScrubsValues(t *testing.T) {
	t.Parallel()
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			t.Parallel()
			_, paths := observerOutputFixture(t)
			out, err := openObserverOutput(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.close() }()
			if got, err := out.loadMemory(); err != nil || !reflect.DeepEqual(got, ObserverMemory{}) {
				t.Fatal("missing memory is not first run", err)
			}
			id := strings.Repeat("a", 64)
			secret := "-----BEGIN PRIVATE KEY-----\nprivate material\n-----END PRIVATE KEY-----\ngh" + "p_" + strings.Repeat("x", 24)
			redacted := "[REDACTED:pem]\n[REDACTED:github-token]"
			mem := ObserverMemory{Liveness: liveness.Memory{Recovering: true}, Signatures: map[string]string{id: secret}}
			if complete {
				mem.Liveness = liveness.Memory{LastTick: time.Now().UTC(), ListKey: id, Slots: map[string]liveness.SlotMemory{id: {Seq: 2, Revision: 3}}}
				mem.Limits.Put(config.Claude, limits.Entry{Kind: limits.Unknown, Reason: secret})
			}
			data, err := encodeObserverMemory(mem)
			if err != nil || bytes.Contains(data, []byte("private material")) || !bytes.Contains(data, []byte("[REDACTED:pem]")) {
				t.Fatal("field scrubbing failed", string(data), err)
			}
			if mem.Signatures[id] != secret || complete && mem.Limits.Get(config.Claude).Reason != secret {
				t.Fatal("encoding mutated caller memory")
			}
			raw, err := json.Marshal(mem)
			if err != nil {
				t.Fatal(err)
			}
			if err := out.root.WriteFile(observerMemory, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := out.loadMemory()
			if err != nil || got.Signatures[id] != redacted || !reflect.DeepEqual(got.Liveness, mem.Liveness) {
				t.Fatal("memory roundtrip failed", got, err)
			}
			if complete && got.Limits.Get(config.Claude).Reason != redacted {
				t.Fatal("loaded limit reason was not scrubbed")
			}
			after, err := out.root.ReadFile(observerMemory)
			if err != nil || !bytes.Equal(after, raw) {
				t.Fatal("loading modified memory", err)
			}
			if !complete && got.Limits.Harnesses != nil {
				t.Fatal("nil nested limits map changed")
			}
		})
	}
}

func TestObserverMemoryRejectsMalformedPresentStateWithoutWrites(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("a", 64)
	valid := `{"Liveness":{},"Signatures":{},"Limits":{}}`
	cases := map[string]string{
		"empty": "", "null": "null", "missing": "{}", "malformed": "{", "trailing": valid + " {}",
		"null-live":       `{"Liveness":null,"Signatures":{},"Limits":{}}`,
		"null-signatures": `{"Liveness":{},"Signatures":null,"Limits":{}}`,
		"null-limits":     `{"Liveness":{},"Signatures":{},"Limits":null}`,
		"unknown":         `{"Liveness":{"unexpected":true},"Signatures":{},"Limits":{}}`,
		"bad-identity":    `{"Liveness":{"slots":{"secret":{}}},"Signatures":{},"Limits":{}}`,
		"bad-signature":   `{"Liveness":{},"Signatures":{"secret":"value"},"Limits":{}}`,
		"null-slot":       fmt.Sprintf(`{"Liveness":{"slots":{%q:null}},"Signatures":{},"Limits":{}}`, id),
		"null-signature":  fmt.Sprintf(`{"Liveness":{},"Signatures":{%q:null},"Limits":{}}`, id),
		"bad-list":        `{"Liveness":{"list_key":"secret"},"Signatures":{},"Limits":{}}`,
		"bad-harness":     `{"Liveness":{},"Signatures":{},"Limits":{"harnesses":{"bad":{"kind":"ok"}}}}`,
		"bad-kind":        `{"Liveness":{},"Signatures":{},"Limits":{"harnesses":{"claude":{"kind":"bad"}}}}`,
		"missing-until":   `{"Liveness":{},"Signatures":{},"Limits":{"harnesses":{"claude":{"kind":"throttled-until"}}}}`,
		"oversize":        valid + strings.Repeat(" ", maxObserverMemoryBytes),
	}
	for _, mark := range []string{`"acted":true`, `"acted_seq":1`, `"sent":["2026-10-18T00:00:00Z"]`, `"blocked":true`, `"blocked_seq":1`, `"hang_reported":true`} {
		cases[mark] = fmt.Sprintf(`{"Liveness":{"slots":{%q:{%s}}},"Signatures":{},"Limits":{}}`, id, mark)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, paths := observerOutputFixture(t)
			out, err := openObserverOutput(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.close() }()
			if err := out.root.WriteFile(observerMemory, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := out.loadMemory(); err == nil {
				t.Fatal("invalid present memory accepted")
			} else if name == "oversize" && !strings.Contains(err.Error(), "bytes") {
				t.Fatal("size guard bypassed", err)
			}
			after, err := out.root.ReadFile(observerMemory)
			if err != nil || string(after) != data {
				t.Fatal("refusal changed memory", err)
			}
			if _, err := out.root.Stat(observerJournal); !os.IsNotExist(err) {
				t.Fatal("memory reader created journal", err)
			}
		})
	}
}
