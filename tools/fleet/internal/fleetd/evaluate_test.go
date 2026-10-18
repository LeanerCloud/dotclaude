package fleetd

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/limits"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/liveness"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/resources"
)

func evaluationFixture() (Observation, config.Config, time.Time) {
	id := agentIdentity(herdr.Agent{Name: "slot", PaneID: "pane", AgentSession: herdr.AgentSession{Kind: "id", Value: "native"}})
	return Observation{Complete: true, Inventory: []Inventory{{Identity: id, PaneID: "pane"}}, Slots: []SlotEvidence{{Name: "slot", Identity: id, Harness: "claude", Status: "idle", InteractiveReady: true, WorkLeft: true}}}, config.Config{Thresholds: config.Thresholds{HangMinutes: 45}}, time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
}

func TestEvaluatePreservesSafetyEvidence(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"nudge", "focus", "not-ready", "blocked", "unknown-repo", "heavy", "incomplete", "unmanaged", "stall", "pressure"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ob, cfg, now := evaluationFixture()
			mem := ObserverMemory{}
			want := liveness.Nudge
			switch mode {
			case "blocked":
				ob.Slots[0].Status = "blocked"
				want = liveness.Blocked
			case "focus":
				ob.Slots[0].Focused = true
				want = ""
			case "not-ready":
				ob.Slots[0].InteractiveReady = false
				want = ""
			case "unknown-repo", "heavy":
				ob.Slots[0].Status = "working"
				ob.Slots[0].HeavyWait = mode == "heavy"
				mem.Liveness = liveness.Memory{LastTick: now.Add(-30 * time.Second), Slots: map[string]liveness.SlotMemory{ob.Slots[0].Identity: {Progress: now.Add(-time.Hour)}}}
				want = ""
			case "incomplete":
				ob.Complete = false
				want = ""
			case "unmanaged":
				ob.Slots = nil
				want = ""
			case "stall":
				ob.Slots[0].Recent = "error: Network error sk-abcdefghijklmnopqrstuvwxyz123456"
				want = liveness.Continue
			case "pressure":
				ob.Resources.Overall = resources.Critical
			}
			before, _ := json.Marshal(mem)
			res, next := Evaluate(cfg, now, ob, mem)
			after, _ := json.Marshal(mem)
			if string(before) != string(after) {
				t.Fatal("input memory mutated")
			}
			if want == "" && len(res.Proposals) != 0 {
				t.Fatalf("unsafe proposal %+v", res)
			}
			if want != "" && (len(res.Proposals) != 1 || res.Proposals[0].Kind != want) {
				t.Fatalf("missing proposal %+v", res)
			}
			for _, sm := range next.Liveness.Slots {
				if sm.Acted || len(sm.Sent) > 0 || sm.Blocked || sm.HangReported {
					t.Fatal("dispatch dedupe consumed")
				}
			}
			if mode == "unknown-repo" && !strings.Contains(strings.Join(res.Notes, " "), "repo activity unknown") {
				t.Fatal("missing unknown evidence")
			}
			if mode == "pressure" && res.Resources.Overall != resources.Critical {
				t.Fatal("pressure lost")
			}
		})
	}
}

func TestEvaluateRecoveryUsesFullInventory(t *testing.T) {
	t.Parallel()
	ob, cfg, now := evaluationFixture()
	_, mem := Evaluate(cfg, now, ob, ObserverMemory{})
	_, real := liveness.Evaluate(liveness.Config{Interval: observerInterval}, now, []liveness.Input{{Agent: herdr.Agent{Name: ob.Inventory[0].Identity, PaneID: "pane"}}}, liveness.Memory{})
	if mem.Liveness.ListKey != real.ListKey {
		t.Fatal("real pane was replaced in inventory stability")
	}
	now = now.Add(11 * time.Minute)
	res, mem := Evaluate(cfg, now, ob, mem)
	if !res.Recovery || len(res.Proposals) != 0 {
		t.Fatal("gap not suppressed")
	}
	unnamed := herdr.Agent{PaneID: "foreign-pane"}
	ob.Inventory = append(ob.Inventory, Inventory{Identity: agentIdentity(unnamed), PaneID: unnamed.PaneID})
	res, mem = Evaluate(cfg, now.Add(30*time.Second), ob, mem)
	if !res.Recovery {
		t.Fatal("unnamed inventory change did not reset stability")
	}
	ob.Complete = false
	_, mem = Evaluate(cfg, now.Add(60*time.Second), ob, mem)
	ob.Complete = true
	res, mem = Evaluate(cfg, now.Add(90*time.Second), ob, mem)
	if !res.Recovery {
		t.Fatal("failed read advanced stable sequence")
	}
	res, _ = Evaluate(cfg, now.Add(120*time.Second), ob, mem)
	if res.Recovery || len(res.Proposals) != 1 {
		t.Fatal("two stable reads did not recover")
	}
	res, _ = Evaluate(cfg, now.Add(-time.Hour), ob, mem)
	if !res.Recovery || len(res.Proposals) != 0 {
		t.Fatal("backwards clock did not recover")
	}
	unnamed.AgentSession.Value = "native"
	if agentIdentity(unnamed) == ob.Inventory[1].Identity {
		t.Fatal("native presence lost")
	}
}

func TestEvaluateLimitsArePerIdentityAndDeterministic(t *testing.T) {
	t.Parallel()
	ob, cfg, now := evaluationFixture()
	originalID := ob.Slots[0].Identity
	text, err := os.ReadFile("../limits/testdata/claude/limit_prompt.txt")
	if err != nil {
		t.Fatal(err)
	}
	ob.Slots[0].Detection = strings.Replace(string(text), "You've hit your limit", "You've hit your limit sk-abcdefghijklmnopqrstuvwxyz123456", 1)
	_, mem := Evaluate(cfg, now, ob, ObserverMemory{})
	ob.Slots[0].Detection = strings.ReplaceAll(ob.Slots[0].Detection, "sk-abcdefghijklmnopqrstuvwxyz123456", "sk-abcdefghijklmnopqrstuvwxyz654321")
	res, _ := Evaluate(cfg, now.Add(30*time.Second), ob, mem)
	if !res.Limits[ob.Slots[0].Identity].Confirmed {
		t.Fatal("same native identity did not confirm")
	}
	foreign := ob.Slots[0]
	foreign.Identity = agentIdentity(herdr.Agent{Name: "slot", PaneID: "replacement"})
	ob.Slots = []SlotEvidence{foreign}
	ob.Inventory = []Inventory{{Identity: foreign.Identity}}
	res, _ = Evaluate(cfg, now.Add(30*time.Second), ob, mem)
	if res.Limits[foreign.Identity].Confirmed {
		t.Fatal("foreign identity borrowed throttle confirmation")
	}
	ob.Slots[0].Identity = originalID
	ob.Inventory[0].Identity = ob.Slots[0].Identity
	worker := foreign
	worker.Status = "working"
	worker.Detection = ""
	ob.Slots = append(ob.Slots, worker)
	ob.Inventory = append(ob.Inventory, Inventory{Identity: worker.Identity})
	since := now.Add(-2 * time.Hour)
	mem.Limits.Put(config.Claude, limits.Entry{Kind: limits.Throttled, Since: since, Until: now.Add(time.Hour)})
	_, a := Evaluate(cfg, now.Add(30*time.Second), ob, mem)
	ob.Slots[0], ob.Slots[1] = ob.Slots[1], ob.Slots[0]
	_, b := Evaluate(cfg, now.Add(30*time.Second), ob, mem)
	if a.Limits.Get(config.Claude).Kind != limits.Throttled || !a.Limits.Get(config.Claude).Since.Equal(since) || !a.Limits.MigrateDue(config.Claude, now, time.Hour) || !reflect.DeepEqual(a.Limits, b.Limits) {
		t.Fatal("worker cleared throttle, reset migration age or order changed state")
	}
	ob.Complete = false
	_, mem = Evaluate(cfg, now.Add(30*time.Second), ob, mem)
	if len(mem.Signatures) != 0 {
		t.Fatal("failed reads advanced consecutive signatures")
	}
	encoded, _ := json.Marshal(a)
	if strings.Contains(string(encoded), "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Fatal("raw token persisted")
	}
}

func TestEvaluateExpiresLimitsOnCompleteTicks(t *testing.T) {
	t.Parallel()
	for _, complete := range []bool{true, false} {
		ob, cfg, now := evaluationFixture()
		ob.Complete, ob.Inventory, ob.Slots = complete, nil, nil
		mem := ObserverMemory{Liveness: liveness.Memory{LastTick: now.Add(-30 * time.Second)}}
		mem.Limits.Put(config.Claude, limits.Entry{Kind: limits.Throttled, Since: now.Add(-time.Hour), Until: now.Add(-time.Second)})
		res, next := Evaluate(cfg, now, ob, mem)
		want := limits.Throttled
		if complete {
			want = limits.Unknown
		}
		if next.Limits.Get(config.Claude).Kind != want || len(res.Proposals) != 0 || mem.Limits.Get(config.Claude).Kind != limits.Throttled {
			t.Fatal("expiry ignored complete evidence or mutated input")
		}
	}
}

func TestEvaluateChoosesLatestConfirmedReset(t *testing.T) {
	t.Parallel()
	ob, cfg, now := evaluationFixture()
	text, err := os.ReadFile("../limits/testdata/claude/limit_prompt.txt")
	if err != nil {
		t.Fatal(err)
	}
	later := ob.Slots[0]
	later.Identity = agentIdentity(herdr.Agent{Name: "other", PaneID: "second"})
	earlyText := strings.Replace(string(text), "3pm (Europe/Bucharest)", "Oct 20, 9am (UTC)", 1)
	lateText := strings.Replace(string(text), "3pm (Europe/Bucharest)", "Oct 21, 9am (UTC)", 1)
	ob.Slots[0].Detection, later.Detection = earlyText, lateText
	if ob.Slots[0].Identity < later.Identity {
		ob.Slots[0].Detection, later.Detection = lateText, earlyText
	}
	ob.Slots = append(ob.Slots, later)
	ob.Inventory = append(ob.Inventory, Inventory{Identity: later.Identity})
	_, mem := Evaluate(cfg, now, ob, ObserverMemory{})
	_, next := Evaluate(cfg, now.Add(30*time.Second), ob, mem)
	want := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	if got := next.Limits.Get(config.Claude).Until; !got.Equal(want) {
		t.Fatalf("reset=%s want=%s", got, want)
	}
	before, _ := json.Marshal(next)
	_, _ = Evaluate(cfg, now.Add(time.Minute), ob, next)
	after, _ := json.Marshal(next)
	if string(before) != string(after) {
		t.Fatal("harness map aliases input")
	}
}

func TestEvaluateUnverifiedHarnessesStayUnverified(t *testing.T) {
	t.Parallel()
	for _, h := range []config.Harness{config.Kimi, config.Codex} {
		ob, cfg, now := evaluationFixture()
		ob.Slots[0].Harness = string(h)
		ob.Slots[0].Detection = "429 too many requests"
		_, mem := Evaluate(cfg, now, ob, ObserverMemory{})
		res, next := Evaluate(cfg, now.Add(30*time.Second), ob, mem)
		r := res.Limits[ob.Slots[0].Identity]
		if r.Confirmed || r.Verified || next.Limits.Get(h).Kind == limits.Throttled {
			t.Fatalf("%s evidence promoted: %+v", h, r)
		}
	}
}
