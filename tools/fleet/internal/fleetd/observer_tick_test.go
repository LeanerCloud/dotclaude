package fleetd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/resources"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

func tickFixture(t *testing.T) (Observer, ObserverPaths, time.Time) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inputs", "checkout", "inputs/handoff"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{Repos: []config.Repo{{Name: "repo", Owner: "owner", Checkout: filepath.Join(dir, "checkout")}}, Roles: map[config.Role]config.RolePolicy{}, Thresholds: config.Thresholds{HangMinutes: 45, MigrateAfter: config.Duration(time.Hour), DiskWarnGB: 30, DiskCriticalGB: 15, HeavySlots: 1}}
	for _, role := range []config.Role{config.Coordinator, config.Impl, config.Merge, config.Reviewer} {
		cfg.Roles[role] = config.RolePolicy{Prefer: []config.Harness{config.Claude}, MinTier: config.TierLow}
	}
	slot := state.Slot{Name: "repo-impl", Repo: "repo", Role: config.Impl, Harness: config.Claude, Status: state.Active, Pane: "p1", SessionID: "native", Cwd: cfg.Repos[0].Checkout}
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	h := state.Handoff{Slot: slot.Name, Updated: now, CurrentItem: "working", NextAction: "continue"}
	handoff, err := h.Format()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"fleet.json": cfg, "slots.json": state.Ledger{Slots: []state.Slot{slot}}} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "inputs", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs/handoff/repo-impl.md"), []byte(handoff), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := ReadObserverPaths(filepath.Join(dir, "inputs/fleet.json"), filepath.Join(dir, "inputs/slots.json"), filepath.Join(dir, "observer"), dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := herdr.Agent{Name: slot.Name, Agent: "claude", AgentStatus: "idle", Cwd: slot.Cwd, PaneID: slot.Pane, InteractiveReady: true, AgentSession: herdr.AgentSession{Kind: "id", Agent: "claude", Value: slot.SessionID}}
	list, err := json.Marshal(map[string]any{"id": "list", "result": map[string]any{"agents": []herdr.Agent{agent}}})
	if err != nil {
		t.Fatal(err)
	}
	o := Observer{Home: dir, Resources: resources.Sampler{Cores: 1, Statfs: func(_ string, st *syscall.Statfs_t) error { st.Bavail = 100; st.Bsize = 1 << 30; return nil }}}
	o.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("unbounded Herdr call")
		}
		if name == "herdr" && reflect.DeepEqual(args, []string{"agent", "list"}) {
			return list, nil
		}
		if name != "herdr" || (!reflect.DeepEqual(args, []string{"agent", "read", slot.Name, "--source", "detection", "--lines", "120"}) && !reflect.DeepEqual(args, []string{"agent", "read", slot.Name, "--source", "recent-unwrapped", "--lines", "120"})) {
			t.Fatalf("unexpected or mutating argv %s %v", name, args)
		}
		return []byte("ready"), nil
	}
	o.Resources.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("unbounded resource call")
		}
		switch name + " " + strings.Join(args, " ") {
		case "sysctl -n vm.loadavg":
			return []byte("{ 0 0 0 }"), nil
		case "memory_pressure -Q":
			return []byte("free percentage: 50%"), nil
		case "sysctl -n vm.swapusage":
			return []byte("total = 0M used = 0M"), nil
		}
		t.Fatal("unexpected resource argv", name, args)
		return nil, nil
	}
	return o, *paths, now
}

func TestObserverTickPersistsRepeatedRecoveryEvidence(t *testing.T) {
	t.Parallel()
	o, paths, now := tickFixture(t)
	for i, delta := range []time.Duration{0, time.Minute, 12 * time.Minute, 13 * time.Minute} {
		got, err := o.Tick(t.Context(), paths, now.Add(delta))
		wantRecovery := i == 2
		if err != nil || !got.Complete || got.Recovery != wantRecovery || (len(got.Proposals) == 0) != wantRecovery {
			t.Fatal("tick evidence incorrect", i, got, err)
		}
		for _, input := range []InputSnapshot{paths.Config, paths.Ledger} {
			data, err := os.ReadFile(input.Path())
			if err != nil || !bytes.Equal(data, input.Bytes()) {
				t.Fatal("tick modified input", err)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(paths.Output, observerJournal))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 4 {
		t.Fatal("missing journal records", len(lines))
	}
	for _, line := range lines {
		var record observerRecord
		if err := json.Unmarshal(line, &record); err != nil || len(record.Observation.Inventory) != 1 || record.Observation.Resources.DiskFreeGB != 100 || record.Evaluation.Resources.Cores != 1 {
			t.Fatal("journal lost actual collected evidence", err)
		}
	}
	out, err := openObserverOutput(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.close() }()
	mem, err := out.loadMemory()
	if err != nil || !mem.Liveness.LastTick.Equal(now.Add(13*time.Minute)) {
		t.Fatal("memory not advanced", err)
	}
	for _, slot := range mem.Liveness.Slots {
		if slot.Acted || slot.ActedSeq != 0 || len(slot.Sent) != 0 || slot.Blocked || slot.BlockedSeq != 0 || slot.HangReported {
			t.Fatal("observer consumed dispatch marks")
		}
	}
}

func TestObserverTickUsesImmutableBytes(t *testing.T) {
	t.Parallel()
	o, paths, now := tickFixture(t)
	for _, input := range []InputSnapshot{paths.Config, paths.Ledger} {
		if err := os.WriteFile(input.Path(), []byte("changed input"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := o.Tick(t.Context(), paths, now); err != nil || !got.Complete || len(got.Proposals) != 1 {
		t.Fatal("tick reopened mutable input", got, err)
	}
	for _, input := range []InputSnapshot{paths.Config, paths.Ledger} {
		if data, err := os.ReadFile(input.Path()); err != nil || string(data) != "changed input" {
			t.Fatal("tick overwrote changed input", err)
		}
	}
}

func TestObserverTickFailuresReturnNoProposals(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"config", "ledger", "memory", "list", "read", "sample", "journal", "late-memory", "cancel", "cancel-during"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			o, paths, now := tickFixture(t)
			healthy := o
			ctx := t.Context()
			secret := "sk-abcdefghijklmnopqrstuvwxyz123456"
			switch mode {
			case "config":
				paths.Config.data = "{}"
			case "ledger":
				paths.Ledger.data = `{"unexpected":true}`
			case "memory", "journal":
				out, err := openObserverOutput(ctx, paths)
				if err != nil {
					t.Fatal(err)
				}
				name := observerMemory
				if mode == "journal" {
					name = observerJournal
				}
				err = out.root.WriteFile(name, []byte("invalid"), 0o600)
				if closeErr := out.close(); err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			case "sample":
				o.Resources.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New(secret) }
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			default:
				run := o.Run
				var cancel context.CancelFunc
				if mode == "cancel-during" {
					ctx, cancel = context.WithCancel(ctx)
					defer cancel()
				}
				o.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if cancel != nil {
						cancel()
					}
					if mode == args[1] {
						return nil, errors.New(secret)
					}
					if mode == "late-memory" && args[1] == "list" {
						if err := os.Mkdir(filepath.Join(paths.Output, observerMemory), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					return run(ctx, name, args...)
				}
			}
			got, err := o.Tick(ctx, paths, now)
			if err == nil || got.Complete || len(got.Proposals) != 0 || len(got.Errors) == 0 || strings.Contains(strings.Join(got.Errors, " "), secret) {
				t.Fatal("failure returned unsafe evidence", got, err)
			}
			if mode == "config" || mode == "ledger" || mode == "cancel" {
				if _, err := os.Lstat(paths.Output); !os.IsNotExist(err) {
					t.Fatal("early refusal created output", err)
				}
			}
			if mode == "list" || mode == "read" || mode == "sample" {
				data, err := os.ReadFile(filepath.Join(paths.Output, observerJournal))
				var record observerRecord
				if err != nil || json.Unmarshal(data, &record) != nil || record.Observation.Complete || record.Evaluation.Complete || len(record.Evaluation.Proposals) != 0 {
					t.Fatal("incomplete observation not journaled", err)
				}
				for i := 1; i <= 2; i++ {
					recovered, err := healthy.Tick(t.Context(), paths, now.Add(time.Duration(i)*time.Minute))
					if err != nil || !recovered.Complete || recovered.Recovery != (i == 1) || (len(recovered.Proposals) == 0) != (i == 1) {
						t.Fatal("failed read did not require two stable recovery ticks", recovered, err)
					}
				}
			}
			if mode == "memory" || mode == "journal" || mode == "cancel-during" {
				name := observerJournal
				if mode == "journal" {
					name = observerMemory
				}
				if _, err := os.Lstat(filepath.Join(paths.Output, name)); !os.IsNotExist(err) {
					t.Fatal("failure unexpectedly persisted artifact", name, err)
				}
			}
			if mode == "late-memory" && !strings.Contains(err.Error(), "journal appended but memory persistence uncertain") {
				t.Fatal("late failure uncertainty lost", err)
			}
		})
	}
}

func TestObserverTickSerializesCollectionAndPersistence(t *testing.T) {
	t.Parallel()
	o, paths, now := tickFixture(t)
	run := o.Run
	var active atomic.Int32
	o.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if args[1] == "list" && !active.CompareAndSwap(0, 1) {
			t.Error("concurrent collection under output lock")
		}
		if args[1] == "read" && args[4] == "recent-unwrapped" {
			defer active.Store(0)
		}
		return run(ctx, name, args...)
	}
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			if got, err := o.Tick(t.Context(), paths, now); err != nil || !got.Complete || len(got.Proposals) != 1 {
				t.Error("concurrent tick failed", got, err)
			}
		})
	}
	workers.Wait()
	data, err := os.ReadFile(filepath.Join(paths.Output, observerJournal))
	if err != nil || bytes.Count(data, []byte("\n")) != 4 || active.Load() != 0 {
		t.Fatal("concurrent records lost", err)
	}
}
