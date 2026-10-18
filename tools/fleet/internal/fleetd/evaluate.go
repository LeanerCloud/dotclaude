package fleetd

import (
	"sort"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/limits"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/liveness"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/resources"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

type ObserverMemory struct {
	Liveness   liveness.Memory
	Signatures map[string]string
	Limits     limits.Set
}

type Proposal struct {
	Identity, Slot string
	Kind           liveness.Kind
	Reason         string
}

type Evaluation struct {
	Complete, Recovery bool
	Proposals          []Proposal
	Limits             map[string]limits.Result
	Resources          resources.Snapshot
	Notes, Errors      []string
}

const observerInterval = 5 * time.Minute

func Evaluate(cfg config.Config, now time.Time, ob Observation, mem ObserverMemory) (Evaluation, ObserverMemory) {
	res := Evaluation{Complete: ob.Complete, Resources: ob.Resources, Limits: map[string]limits.Result{}}
	for _, note := range ob.Notes {
		res.Notes = append(res.Notes, state.Scrub(note))
	}
	for _, err := range ob.Errors {
		res.Errors = append(res.Errors, state.Scrub(err))
	}
	next := ObserverMemory{Signatures: map[string]string{}, Limits: limits.Set{Harnesses: map[config.Harness]limits.Entry{}}}
	for h, e := range mem.Limits.Harnesses {
		if h.Valid() {
			e.Reason = state.Scrub(e.Reason)
			next.Limits.Put(h, e)
		}
	}
	if !ob.Complete {
		next.Liveness = liveness.Memory{LastTick: mem.Liveness.LastTick, Recovering: true}
		res.Recovery = true
		return res, next
	}
	eligible := map[string]SlotEvidence{}
	for _, e := range ob.Slots {
		eligible[e.Identity] = e
	}
	var inputs []liveness.Input
	for _, inv := range ob.Inventory {
		in := liveness.Input{Agent: herdr.Agent{Name: inv.Identity, PaneID: inv.PaneID}}
		if e, ok := eligible[inv.Identity]; ok {
			in.Agent.AgentStatus = state.Scrub(e.Status)
			in.Agent.StateChangeSeq, in.Agent.Revision = e.Seq, e.Revision
			in.Agent.Focused, in.Agent.InteractiveReady = e.Focused, e.InteractiveReady
			in.WorkLeft, in.HeavyWait = e.WorkLeft, e.HeavyWait
			in.StallSignature = liveness.StallSignature(state.Scrub(e.Recent))
			if e.Status == "working" {
				in.Agent.AgentStatus = ""
				res.Notes = append(res.Notes, state.Scrub(e.Name+": repo activity unknown; hang suppressed"))
			}
		}
		inputs = append(inputs, in)
	}
	activity, live := liveness.Evaluate(liveness.Config{Interval: observerInterval, HangAfter: time.Duration(cfg.Thresholds.HangMinutes) * time.Minute}, now, inputs, mem.Liveness)
	res.Recovery = activity.Recovery
	res.Notes = append(res.Notes, activity.Notes...)
	for _, d := range activity.Decisions {
		if e, ok := eligible[d.Slot]; ok {
			res.Proposals = append(res.Proposals, Proposal{Identity: e.Identity, Slot: state.Scrub(e.Name), Kind: d.Kind, Reason: state.Scrub(d.Reason)})
		}
	}
	for id, sm := range live.Slots {
		live.Slots[id] = liveness.SlotMemory{Seq: sm.Seq, Revision: sm.Revision, Progress: sm.Progress}
	}
	next.Liveness = live
	next.Limits.ExpireThrottles(now)
	slots := append([]SlotEvidence(nil), ob.Slots...)
	sort.Slice(slots, func(i, j int) bool { return slots[i].Identity < slots[j].Identity })
	confirmed := map[config.Harness]limits.Result{}
	confirmedUntil := map[config.Harness]time.Time{}
	for _, e := range slots {
		previous := ""
		if !res.Recovery && !activity.ExpireThrottles {
			previous = state.Scrub(mem.Signatures[e.Identity])
		}
		r := limits.Detect(limits.Input{Harness: config.Harness(e.Harness), Status: state.Scrub(e.Status), Detection: state.Scrub(e.Detection), PrevSignature: previous, Now: now})
		if !res.Recovery {
			next.Signatures[e.Identity] = r.Signature
		}
		res.Limits[e.Identity] = r
		if r.Confirmed && !res.Recovery {
			candidate := limits.Set{}
			candidate.Observe(r, e.Status, now)
			until := candidate.Get(r.Harness).Until
			if !until.Before(confirmedUntil[r.Harness]) {
				confirmed[r.Harness], confirmedUntil[r.Harness] = r, until
			}
		}
	}
	for _, e := range slots {
		r := res.Limits[e.Identity]
		if _, throttled := confirmed[r.Harness]; !throttled && !res.Recovery {
			next.Limits.Observe(r, e.Status, now)
		}
	}
	for _, r := range confirmed {
		next.Limits.Observe(r, "", now)
	}
	return res, next
}
