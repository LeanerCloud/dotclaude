package liveness

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
)

var (
	t0  = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	cfg = Config{Interval: 5 * time.Minute, HangAfter: 45 * time.Minute}
)

func agent(name, status string, seq int64) herdr.Agent {
	return herdr.Agent{Name: name, PaneID: "w1:p1", AgentStatus: status, StateChangeSeq: seq, Revision: 1, InteractiveReady: true}
}

func idle(seq int64) Input { return Input{Agent: agent("cc-impl", "idle", seq), WorkLeft: true} }

func kinds(r Result) []Kind {
	var out []Kind
	for _, d := range r.Decisions {
		out = append(out, d.Kind)
	}
	return out
}

func run(t *testing.T, mem Memory, now time.Time, in ...Input) (Result, Memory) {
	t.Helper()
	return Evaluate(cfg, now, in, mem)
}

// hold ticks every 5 minutes after start up to start+minutes with unchanged inputs, returning every
// decision made on the way, the last result and the final memory.
func hold(t *testing.T, mem Memory, start time.Time, minutes int, in ...Input) ([]Decision, Result, Memory) {
	t.Helper()
	var all []Decision
	var res Result
	for m := 5; m <= minutes; m += 5 {
		res, mem = run(t, mem, start.Add(time.Duration(m)*time.Minute), in...)
		all = append(all, res.Decisions...)
	}
	return all, res, mem
}

func TestNudgeOnlyForReadyIdleAgentWithWork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Input)
		want   []Kind
	}{
		{"idle with work", func(*Input) {}, []Kind{Nudge}},
		{"done with work", func(i *Input) { i.Agent.AgentStatus = "done" }, []Kind{Nudge}},
		{"no work left", func(i *Input) { i.WorkLeft = false }, nil},
		{"working", func(i *Input) { i.Agent.AgentStatus = "working" }, nil},
		{"unknown status", func(i *Input) { i.Agent.AgentStatus = "" }, nil},
		{"not interactive ready", func(i *Input) { i.Agent.InteractiveReady = false }, nil},
		{"focused pane", func(i *Input) { i.Agent.Focused = true }, nil},
		{"stall wins over nudge", func(i *Input) { i.StallSignature = "abc" }, []Kind{Continue}},
		{"stall without work still continues", func(i *Input) { i.StallSignature = "abc"; i.WorkLeft = false }, []Kind{Continue}},
		{"stall on a working agent", func(i *Input) { i.StallSignature = "abc"; i.Agent.AgentStatus = "working" }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := idle(10)
			tt.mutate(&in)
			res, _ := run(t, Memory{}, t0, in)
			if got := kinds(res); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestOneActionPerReadyEpoch(t *testing.T) {
	t.Parallel()
	res, mem := run(t, Memory{}, t0, idle(10))
	if len(res.Decisions) != 1 {
		t.Fatal("first tick must nudge")
	}
	res, mem = run(t, mem, t0.Add(5*time.Minute), idle(10))
	if len(res.Decisions) != 0 {
		t.Fatalf("same epoch nudged twice: %v", res.Decisions)
	}
	res, _ = run(t, mem, t0.Add(10*time.Minute), idle(11))
	if len(res.Decisions) != 1 {
		t.Fatal("a new epoch must nudge again")
	}
}

func TestMemoryIsNotMutated(t *testing.T) {
	t.Parallel()
	_, mem := run(t, Memory{}, t0, idle(10))
	before := mem.Slots["cc-impl"]
	_, _ = run(t, mem, t0.Add(time.Minute), idle(11))
	if !reflect.DeepEqual(before, mem.Slots["cc-impl"]) {
		t.Fatal("Evaluate mutated its input memory")
	}
}

func TestHourlyCapPortedFromResumeTick(t *testing.T) {
	t.Parallel()
	var mem Memory
	sent := 0
	for seq := int64(10); seq < 15; seq++ {
		var res Result
		res, mem = run(t, mem, t0.Add(time.Duration(seq-10)*time.Minute), idle(seq))
		sent += len(res.Decisions)
	}
	if sent != MaxPerHour {
		t.Fatalf("want %d actions in an hour, got %d", MaxPerHour, sent)
	}
	res, mem := run(t, mem, t0.Add(5*time.Minute), idle(20))
	if len(res.Decisions) != 0 || len(res.Notes) == 0 {
		t.Fatalf("cap must hold and be noted: %+v", res)
	}
	// Keep ticking (every new epoch is capped) until the oldest send is an hour old.
	var got []Decision
	for m := 10; m <= 64; m += 5 {
		res, mem = run(t, mem, t0.Add(time.Duration(m)*time.Minute), idle(int64(20+m)))
		got = append(got, res.Decisions...)
	}
	if len(got) != 1 {
		t.Fatalf("cap must lapse once the earliest send is an hour old, got %v", got)
	}
}

func TestHangDetection(t *testing.T) {
	t.Parallel()
	work := func(seq, rev int64) Input {
		a := agent("cc-impl", "working", seq)
		a.Revision = rev
		return Input{Agent: a}
	}
	_, mem := run(t, Memory{}, t0, work(5, 1))
	got, _, mem := hold(t, mem, t0, 40, work(5, 1))
	if len(got) != 0 {
		t.Fatalf("early hang: %v", got)
	}
	// 45 minutes without change is the threshold. A branch with repo activity or a revision bump
	// must not hang; the main line does.
	active := work(5, 1)
	active.RepoActivity = t0.Add(30 * time.Minute)
	if res, _ := run(t, mem, t0.Add(45*time.Minute), active); len(res.Decisions) != 0 {
		t.Fatalf("repo activity must defer the hang: %v", res.Decisions)
	}
	if res, _ := run(t, mem, t0.Add(45*time.Minute), work(5, 2)); len(res.Decisions) != 0 {
		t.Fatalf("revision change must defer the hang: %v", res.Decisions)
	}
	res, mem := run(t, mem, t0.Add(45*time.Minute), work(5, 1))
	if got := kinds(res); !reflect.DeepEqual(got, []Kind{Hang}) {
		t.Fatalf("want hang, got %v", got)
	}
	// Reported once per stall.
	if got, _, mem2 := hold(t, mem, t0.Add(45*time.Minute), 30, work(5, 1)); len(got) != 0 {
		t.Fatalf("hang repeated: %v", got)
	} else {
		mem = mem2
	}
	// Progress re-arms it.
	_, mem = run(t, mem, t0.Add(80*time.Minute), work(6, 1))
	got, _, _ = hold(t, mem, t0.Add(80*time.Minute), 50, work(6, 1))
	if len(got) != 1 || got[0].Kind != Hang {
		t.Fatalf("hang must re-arm after progress: %v", got)
	}
}

func TestLongStaticTestRunWithHeavyWaitMarker(t *testing.T) {
	t.Parallel()
	in := Input{Agent: agent("go-impl", "working", 7), HeavyWait: true}
	_, mem := run(t, Memory{}, t0, in)
	got, _, mem := hold(t, mem, t0, 180, in)
	if len(got) != 0 {
		t.Fatalf("queued behind the heavy semaphore is not a hang: %v", got)
	}
	// Without the marker the same silent agent is a hang, measured from its unchanged progress time.
	in.HeavyWait = false
	res, _ := run(t, mem, t0.Add(185*time.Minute), in)
	if !reflect.DeepEqual(kinds(res), []Kind{Hang}) {
		t.Fatalf("hang must surface after the marker is removed: %v", res.Decisions)
	}
}

func TestStallSignature(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		stalled    bool
	}{
		{"claude api error", "⏺ Working\n  ⎿  API Error: Connection reset\n\n╭────╮\n│ > │\n╰────╯\n", true},
		{"claude glyph connection error", "  ⎿  Connection error\n❯ \n", true},
		{"request timed out", "⏺ Request timed out\n", true},
		{"econnreset anywhere in line", "  ⎿  read tcp: ECONNRESET by peer\n", true},
		{"overloaded", "⎿ {\"type\":\"overloaded_error\"}\n", true},
		{"stream idle timeout", "⎿  Stream idle timeout\n", true},
		{"plain label without glyph", "Error: Network error talking to the API\n", true},
		{"bullet glyph", "● fetch failed\n", true},
		{"codex stream disconnected", "■ stream disconnected before completion\n", true},
		{"error scrolled above the tail", "⎿  API Error\n" + strings.Repeat("line\n", 8), false},
		{"prose mentioning an error", "I fixed the API Error handling in the client\n", false},
		{"clean", "⏺ Done\n❯ \n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StallSignature(tt.text)
			if (got != "") != tt.stalled {
				t.Fatalf("got %q, stalled=%v", got, tt.stalled)
			}
		})
	}
	a, b := StallSignature("⎿  API Error\n"), StallSignature("⎿  API Error\n")
	if a == "" || a != b || a == StallSignature("⎿  API Error: other\n") {
		t.Fatalf("signature must be stable and tail-specific: %q %q", a, b)
	}
}
