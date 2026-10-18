package fleetd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/lock"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/resources"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

func TestCollectReadOnlyEvidence(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"active", "not-ready", "empty-ledger", "session-kind", "session-agent", "empty-session", "empty-pane", "foreign", "pane", "harness", "cwd", "unverified", "paused", "draining", "quiescing", "retired", "missing", "error", "envelope", "malformed", "duplicate", "handoff", "invalid-handoff", "escape", "root-escape", "fifo", "oversize", "sample", "repo", "heavy"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			put := func(path, data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "fleet.json")
			put(path, "{}")
			if err := os.Mkdir(filepath.Join(dir, "handoff"), 0o700); err != nil {
				t.Fatal(err)
			}
			h := state.Handoff{Slot: "repo-impl", Updated: time.Now(), CurrentItem: "working", NextAction: "continue"}
			text, err := h.Format()
			if err != nil {
				t.Fatal(err)
			}
			if mode != "handoff" && mode != "escape" && mode != "fifo" {
				put(filepath.Join(dir, "handoff", "repo-impl.md"), text)
			}
			if mode == "fifo" {
				if err := syscall.Mkfifo(filepath.Join(dir, "handoff", "repo-impl.md"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "oversize" {
				put(filepath.Join(dir, "handoff", "repo-impl.md"), text+strings.Repeat("x", 64<<10))
			}
			if mode == "escape" {
				put(filepath.Join(dir, "outside.md"), text)
				if err := os.Symlink("../outside.md", filepath.Join(dir, "handoff", "repo-impl.md")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "root-escape" {
				if err := os.Rename(filepath.Join(dir, "handoff"), filepath.Join(dir, "outside")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, "outside"), filepath.Join(dir, "handoff")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "invalid-handoff" {
				put(filepath.Join(dir, "handoff", "repo-impl.md"), "invalid handoff")
			}
			slot := state.Slot{Name: "repo-impl", Repo: "repo", Role: config.Impl, Harness: config.Claude, Status: state.Active, Pane: "p1", SessionID: "session", Cwd: dir, HandoffPath: filepath.Join(dir, "outside.md")}
			a := herdr.Agent{Name: slot.Name, Agent: "claude", AgentStatus: "idle", Cwd: dir, PaneID: "p1", InteractiveReady: mode != "not-ready", AgentSession: herdr.AgentSession{Kind: "id", Agent: "claude", Value: "session"}}
			inventoryOnly := map[string]bool{"empty-ledger": true, "session-kind": true, "session-agent": true, "empty-session": true, "empty-pane": true, "foreign": true, "pane": true, "harness": true, "cwd": true, "unverified": true, "paused": true, "draining": true, "quiescing": true, "retired": true, "missing": true}[mode]
			switch mode {
			case "session-kind":
				a.AgentSession.Kind = "path"
			case "session-agent":
				a.AgentSession.Agent = "codex"
			case "empty-session":
				slot.SessionID, a.AgentSession.Value = "", ""
			case "empty-pane":
				slot.Pane, a.PaneID = "", ""
			case "foreign":
				a.AgentSession.Value = "foreign"
			case "pane":
				a.PaneID = "foreign"
			case "harness":
				a.Agent = "unknown"
			case "cwd":
				a.Cwd = t.TempDir()
			case "unverified":
				slot.Status = state.AdoptedUnverified
			case "paused", "draining", "quiescing", "retired":
				slot.Status = state.Status(mode)
			case "heavy":
				base := filepath.Join(dir, ".claude", "fleet", "state", "heavy-wait")
				if err := os.MkdirAll(base, 0o700); err != nil {
					t.Fatal(err)
				}
				put(filepath.Join(base, slot.Name+"."+strconv.Itoa(os.Getpid())), "")
			}
			if mode == "repo" {
				slot.Repo = "unknown"
			}
			secret := "sk-abcdefghijklmnopqrstuvwxyz123456"
			calls := 0
			run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("unbounded read")
				}
				if name != "herdr" {
					t.Fatalf("unexpected binary %s", name)
				}
				if reflect.DeepEqual(args, []string{"agent", "list"}) {
					if mode == "malformed" {
						return []byte(`{"id":"list","result":{}}`), nil
					}
					if mode == "envelope" {
						return []byte(`{"id":"list","error":{"code":"bad","message":"` + secret + `"}}`), nil
					}
					agents := []herdr.Agent{a, {Name: secret, Agent: secret}, {}, {}}
					if mode == "duplicate" {
						agents = append(agents, a)
					}
					if mode == "missing" {
						agents = agents[1:]
					}
					data, _ := json.Marshal(map[string]any{"id": "list", "result": map[string]any{"agents": agents}})
					return data, nil
				}
				if !reflect.DeepEqual(args, []string{"agent", "read", slot.Name, "--source", "detection", "--lines", "120"}) && !reflect.DeepEqual(args, []string{"agent", "read", slot.Name, "--source", "recent-unwrapped", "--lines", "120"}) {
					t.Fatalf("mutating/unexpected argv %v", args)
				}
				calls++
				if mode == "error" {
					return nil, errors.New(secret)
				}
				return []byte(secret), nil
			}
			sampler := resources.Sampler{Cores: 1, Statfs: func(_ string, s *syscall.Statfs_t) error { s.Bavail = 1; s.Bsize = 1; return nil }, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("unbounded resource call")
				}
				if mode == "sample" {
					return nil, errors.New(secret)
				}
				switch name + " " + strings.Join(args, " ") {
				case "sysctl -n vm.loadavg":
					return []byte("{ 0 0 0 }"), nil
				case "memory_pressure -Q":
					return []byte("free percentage: 50%"), nil
				case "sysctl -n vm.swapusage":
					return []byte("total = 0M used = 0M"), nil
				}
				t.Fatal("unexpected resource command")
				return nil, nil
			}}
			cfg := config.Config{Repos: []config.Repo{{Name: "repo", Owner: "owner", Checkout: dir}}, Roles: map[config.Role]config.RolePolicy{}, Thresholds: config.Thresholds{HangMinutes: 45, MigrateAfter: config.Duration(time.Hour), DiskWarnGB: 30, DiskCriticalGB: 15, HeavySlots: 1}}
			for _, role := range []config.Role{config.Coordinator, config.Impl, config.Merge, config.Reviewer} {
				cfg.Roles[role] = config.RolePolicy{Prefer: []config.Harness{config.Claude}, MinTier: config.TierLow}
			}
			ledger := state.Ledger{Slots: []state.Slot{slot}}
			if mode == "empty-ledger" {
				ledger.Slots = nil
			}
			ob := (Observer{Run: run, Resources: sampler, Home: dir}).Collect(t.Context(), path, cfg, ledger, time.Now())
			encoded, err := json.Marshal(ob)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), secret) {
				t.Fatal("unscrubbed evidence")
			}
			wantComplete := mode == "active" || mode == "not-ready" || mode == "heavy" || inventoryOnly
			if ob.Complete != wantComplete {
				t.Fatalf("complete=%v errors=%v", ob.Complete, ob.Errors)
			}
			if (mode == "active" || mode == "not-ready") && (len(ob.Inventory) != 4 || len(ob.Slots) != 1 || ob.Slots[0].InteractiveReady != (mode != "not-ready") || !ob.Slots[0].WorkLeft || ob.Resources.Overall != resources.Critical) {
				t.Fatalf("missing evidence: %+v", ob)
			}
			if inventoryOnly && (calls != 0 || len(ob.Slots) != 0) {
				t.Fatal("inventory agent read or adopted")
			}
			if mode == "heavy" && (!ob.Slots[0].HeavyWait || !lock.HeavyWaiting(filepath.Join(dir, ".claude", "fleet", "state", "heavy-wait", slot.Name), time.Now(), lock.DefaultTimeout)) {
				t.Fatal("heavy evidence missing or marker mutated")
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != "{}" {
				t.Fatal("input mutated")
			}
		})
	}
}
