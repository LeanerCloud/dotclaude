// Package liveness decides, from successive herdr agent snapshots and repo activity, which slots need
// a nudge, a network-stall continue, or a hang or blocked event. It is pure: the caller reads herdr,
// git and the heavy-wait markers, and persists the returned Memory before acting on the decisions.
package liveness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

// MaxPerHour caps nudges plus continues per slot per rolling hour.
const MaxPerHour = 3

// StableReads is how many consecutive identical agent lists end recovery mode.
const StableReads = 2

type Kind string

const (
	// Nudge: idle or done with work left; the caller sends the "continue from your handoff" template.
	Nudge Kind = "nudge"
	// Continue: the last turn stopped on a network or API error.
	Continue Kind = "continue"
	// Hang: working with no state change and no repo activity for HangAfter.
	Hang Kind = "hang"
	// Blocked: the agent waits on a dialog; the caller raises an event.
	Blocked Kind = "blocked"
)

type Decision struct {
	Slot   string
	Kind   Kind
	Reason string
}

// Input is one slot's snapshot for this tick.
type Input struct {
	Agent herdr.Agent
	// WorkLeft: the slot's handoff shows an item in flight or claims held.
	WorkLeft bool
	// RepoActivity is the newest commit, push or PR comment by the slot; zero if unknown.
	RepoActivity time.Time
	// HeavyWait: the slot's heavy-wait marker file exists, so it is queued on a build slot.
	HeavyWait bool
	// StallSignature is StallSignature(recent-unwrapped text), "" when no stall line shows. Only read
	// for idle or done agents.
	StallSignature string
}

type Config struct {
	// Interval is the tick period; a gap over twice this enters recovery mode.
	Interval  time.Duration
	HangAfter time.Duration
}

// SlotMemory is what is remembered about one slot between ticks.
type SlotMemory struct {
	Seq      int64 `json:"seq"`
	Revision int64 `json:"revision"`
	// Progress is when Seq or Revision last changed (or the slot was first seen or recovered).
	Progress     time.Time `json:"progress"`
	HangReported bool      `json:"hang_reported,omitempty"`
	// Acted marks the ready epoch (Seq) that already got a nudge or continue: one action per epoch.
	Acted    bool        `json:"acted,omitempty"`
	ActedSeq int64       `json:"acted_seq,omitempty"`
	Sent     []time.Time `json:"sent,omitempty"`
	// BlockedSeq is the Seq a blocked event was already raised for.
	Blocked    bool  `json:"blocked,omitempty"`
	BlockedSeq int64 `json:"blocked_seq,omitempty"`
}

// Memory is the whole tracker state, stored between ticks.
type Memory struct {
	LastTick   time.Time             `json:"last_tick"`
	Recovering bool                  `json:"recovering,omitempty"`
	Stable     int                   `json:"stable,omitempty"`
	ListKey    string                `json:"list_key,omitempty"`
	Slots      map[string]SlotMemory `json:"slots"`
}

type Result struct {
	// Recovery: the gap guard is active. Decisions is empty and the caller spawns and nudges nothing.
	Recovery bool
	// ExpireThrottles is set on the tick that enters recovery: call limits.Set.ExpireThrottles.
	ExpireThrottles bool
	Decisions       []Decision
	Notes           []string
}

func listKey(inputs []Input) string {
	parts := make([]string, 0, len(inputs))
	for _, in := range inputs {
		parts = append(parts, in.Agent.Name+"|"+in.Agent.PaneID)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

// Evaluate computes this tick's decisions. It never mutates mem; persist the returned Memory (with
// its Acted and Sent marks) before dispatching any decision, so a crash mid-dispatch cannot repeat it.
func Evaluate(cfg Config, now time.Time, inputs []Input, mem Memory) (Result, Memory) {
	var res Result
	next := Memory{LastTick: now, Recovering: mem.Recovering, Stable: mem.Stable, ListKey: mem.ListKey,
		Slots: make(map[string]SlotMemory, len(inputs))}
	key := listKey(inputs)

	gap := !mem.LastTick.IsZero() && now.Sub(mem.LastTick) > 2*cfg.Interval
	switch {
	case gap:
		next.Recovering, next.Stable, next.ListKey = true, 1, key
		res.ExpireThrottles = true
		res.Notes = append(res.Notes, fmt.Sprintf("gap of %s since the last tick: recovery mode", now.Sub(mem.LastTick).Round(time.Second)))
	case next.Recovering:
		if key == next.ListKey {
			next.Stable++
		} else {
			next.Stable, next.ListKey = 1, key
		}
		if next.Stable >= StableReads {
			next.Recovering, next.Stable = false, 0
		}
	default:
		next.ListKey = key
	}

	for _, in := range inputs {
		a := in.Agent
		sm, seen := mem.Slots[a.Name]
		switch {
		case !seen || gap:
			sm = SlotMemory{Seq: a.StateChangeSeq, Revision: a.Revision, Progress: now, Acted: sm.Acted, ActedSeq: sm.ActedSeq,
				Blocked: sm.Blocked, BlockedSeq: sm.BlockedSeq}
		case sm.Seq != a.StateChangeSeq || sm.Revision != a.Revision:
			sm.Seq, sm.Revision, sm.Progress, sm.HangReported = a.StateChangeSeq, a.Revision, now, false
		}
		sm.Sent = recent(sm.Sent, now)
		if !next.Recovering && !a.Focused {
			if d, ok := decide(cfg, now, in, &sm, &res); ok {
				res.Decisions = append(res.Decisions, d)
			}
		}
		next.Slots[a.Name] = sm
	}
	res.Recovery = next.Recovering
	return res, next
}

func recent(sent []time.Time, now time.Time) []time.Time {
	var out []time.Time
	for _, t := range sent {
		if now.Sub(t) < time.Hour {
			out = append(out, t)
		}
	}
	return out
}

func decide(cfg Config, now time.Time, in Input, sm *SlotMemory, res *Result) (Decision, bool) {
	a := in.Agent
	switch a.AgentStatus {
	case "blocked":
		if sm.Blocked && sm.BlockedSeq == a.StateChangeSeq {
			return Decision{}, false
		}
		sm.Blocked, sm.BlockedSeq = true, a.StateChangeSeq
		return Decision{a.Name, Blocked, "agent is blocked on a dialog"}, true
	case "working":
		progress := sm.Progress
		if in.RepoActivity.After(progress) {
			progress = in.RepoActivity
		}
		if in.HeavyWait || sm.HangReported || now.Sub(progress) < cfg.HangAfter {
			return Decision{}, false
		}
		sm.HangReported = true
		return Decision{a.Name, Hang, fmt.Sprintf("working for %s with no state change or repo activity", now.Sub(progress).Round(time.Minute))}, true
	case "idle", "done":
		return ready(now, in, sm, res)
	}
	return Decision{}, false
}

// ready ports resume_tick.py ready_epoch: only an idle or done, interactive-ready agent is prompted,
// once per state_change_seq epoch.
func ready(now time.Time, in Input, sm *SlotMemory, res *Result) (Decision, bool) {
	a := in.Agent
	if !a.InteractiveReady || (sm.Acted && sm.ActedSeq == a.StateChangeSeq) {
		return Decision{}, false
	}
	var d Decision
	switch {
	case in.StallSignature != "":
		d = Decision{a.Name, Continue, "last turn stopped on a network or API error (" + in.StallSignature + ")"}
	case in.WorkLeft:
		d = Decision{a.Name, Nudge, "idle with work left in the handoff"}
	default:
		return Decision{}, false
	}
	if len(sm.Sent) >= MaxPerHour {
		res.Notes = append(res.Notes, fmt.Sprintf("%s: hit %d/hour cap, leaving it", a.Name, MaxPerHour))
		return Decision{}, false
	}
	sm.Acted, sm.ActedSeq = true, a.StateChangeSeq
	sm.Sent = append(sm.Sent, now)
	return d, true
}

// Load reads the stored memory; a missing file is a first run.
func Load(path string) (Memory, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Memory{}, nil
	}
	if err != nil {
		return Memory{}, fmt.Errorf("read liveness memory: %w", err)
	}
	var m Memory
	if err := json.Unmarshal(data, &m); err != nil {
		return Memory{}, fmt.Errorf("%s: decode: %w", path, err)
	}
	return m, nil
}

// Save writes the memory atomically.
func (m Memory) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(path, append(data, '\n'), 0o600)
}
