package liveness

import (
	"path/filepath"
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

func TestBlockedReportedOncePerEpoch(t *testing.T) {
	t.Parallel()
	in := Input{Agent: agent("cc-impl", "blocked", 3)}
	res, mem := run(t, Memory{}, t0, in)
	if !reflect.DeepEqual(kinds(res), []Kind{Blocked}) {
		t.Fatalf("got %v", kinds(res))
	}
	if res, mem = run(t, mem, t0.Add(5*time.Minute), in); len(res.Decisions) != 0 {
		t.Fatalf("blocked repeated: %v", res.Decisions)
	}
	in.Agent.StateChangeSeq = 4
	if res, _ = run(t, mem, t0.Add(10*time.Minute), in); len(res.Decisions) != 1 {
		t.Fatalf("a new blocked epoch must report: %v", res.Decisions)
	}
}

func TestFocusedPaneIsNeverActedOn(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"idle", "blocked", "working"} {
		in := Input{Agent: agent("cc-impl", status, 1), WorkLeft: true, StallSignature: "x"}
		in.Agent.Focused = true
		_, mem := run(t, Memory{}, t0, in)
		got, _, mem := hold(t, mem, t0, 180, in)
		if len(got) != 0 {
			t.Fatalf("%s: focused pane got %v", status, got)
		}
		// Once the owner leaves, the normal rules apply on the next tick.
		in.Agent.Focused = false
		if res, _ := run(t, mem, t0.Add(185*time.Minute), in); len(res.Decisions) != 1 {
			t.Fatalf("%s: unfocused pane must be handled: %v", status, res.Decisions)
		}
	}
}

func TestThreeDayGapEntersRecovery(t *testing.T) {
	t.Parallel()
	hung := Input{Agent: agent("a-impl", "working", 1)}
	hung.Agent.PaneID = "w1:p2"
	nudged := Input{Agent: agent("b-impl", "idle", 2), WorkLeft: true}
	nudged.Agent.PaneID = "w1:p3"
	// Before the machine sleeps: a hang report on a-impl and three nudges on b-impl.
	_, mem := run(t, Memory{}, t0, hung, nudged)
	for i := 1; i <= 3; i++ {
		n := nudged
		n.Agent.StateChangeSeq += int64(i)
		_, mem = run(t, mem, t0.Add(time.Duration(i)*time.Minute), hung, n)
	}
	_, _, mem = hold(t, mem, t0.Add(3*time.Minute), 45, hung, nudged)
	if !mem.Slots["a-impl"].HangReported || len(mem.Slots["b-impl"].Sent) != 3 {
		t.Fatalf("setup failed: %+v", mem.Slots)
	}

	wake := mem.LastTick.Add(72 * time.Hour)
	res, rec := run(t, mem, wake, hung, nudged)
	if !res.Recovery || !res.ExpireThrottles || len(res.Decisions) != 0 {
		t.Fatalf("gap tick: %+v", res)
	}
	if s := rec.Slots["a-impl"]; s.HangReported || !s.Progress.Equal(wake) {
		t.Fatalf("hang timer not reset: %+v", s)
	}
	if len(rec.Slots["b-impl"].Sent) != 0 {
		t.Fatalf("nudge timer not reset: %+v", rec.Slots["b-impl"])
	}
	// A repeat of the same list ends recovery. The idle agent got a new epoch while the machine was
	// off, so it is nudged again; the working one is not hung, because its timer restarted at wake.
	nudged.Agent.StateChangeSeq = 99
	res, _ = run(t, rec, wake.Add(5*time.Minute), hung, nudged)
	if res.Recovery || res.ExpireThrottles {
		t.Fatalf("two identical reads must end recovery: %+v", res)
	}
	if !reflect.DeepEqual(kinds(res), []Kind{Nudge}) {
		t.Fatalf("want one nudge, got %v", res.Decisions)
	}
}

func TestBackwardClockJumpEntersRecovery(t *testing.T) {
	t.Parallel()
	_, mem := run(t, Memory{}, t0, idle(1))
	res, mem := run(t, mem, t0.Add(-time.Minute), idle(1))
	if !res.Recovery || !res.ExpireThrottles || !mem.Recovering || len(res.Decisions) != 0 {
		t.Fatalf("now before LastTick must enter recovery: %+v", res)
	}
}

func TestRecoveryWaitsForStableAgentList(t *testing.T) {
	t.Parallel()
	a := Input{Agent: agent("a-impl", "idle", 1), WorkLeft: true}
	b := Input{Agent: agent("b-impl", "idle", 1), WorkLeft: true}
	b.Agent.PaneID = "w1:p2"
	_, mem := run(t, Memory{}, t0, a)
	wake := t0.Add(24 * time.Hour)
	// The gap tick sees one agent, the next sees a restore adding another, the third repeats it.
	res, mem := run(t, mem, wake, a)
	if !res.Recovery {
		t.Fatal("gap must enter recovery")
	}
	res, mem = run(t, mem, wake.Add(5*time.Minute), a, b)
	if !res.Recovery || len(res.Decisions) != 0 {
		t.Fatalf("a changing list keeps recovery on: %+v", res)
	}
	res, mem = run(t, mem, wake.Add(10*time.Minute), a, b) // a already got its nudge in this epoch
	if res.Recovery || len(res.Decisions) != 1 || res.Decisions[0].Slot != "b-impl" {
		t.Fatalf("a repeated list ends recovery and acts: %+v", res)
	}
	if mem.Recovering {
		t.Fatal("memory still recovering")
	}
}

func TestNoGapOnFirstRunOrWithinTwoIntervals(t *testing.T) {
	t.Parallel()
	res, mem := run(t, Memory{}, t0, idle(1))
	if res.Recovery {
		t.Fatal("first run is not recovery")
	}
	if res, _ = run(t, mem, t0.Add(10*time.Minute), idle(2)); res.Recovery {
		t.Fatal("exactly 2x the interval is not a gap")
	}
	if res, _ = run(t, mem, t0.Add(10*time.Minute+time.Second), idle(2)); !res.Recovery {
		t.Fatal("over 2x the interval is a gap")
	}
}

func TestVanishedSlotsAreForgotten(t *testing.T) {
	t.Parallel()
	_, mem := run(t, Memory{}, t0, idle(1))
	_, mem = run(t, mem, t0.Add(time.Minute))
	if len(mem.Slots) != 0 {
		t.Fatalf("stale memory kept: %v", mem.Slots)
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
		{"prose network error handling", "Network error handling is now retried\n", false},
		{"prose connection errors", "Connection errors are logged as warnings\n", false},
		{"prose econnreset", "Done. Added a test for ECONNRESET handling\n", false},
		{"dash bullet prose", "- socket hang up regression fixed\n", false},
		{"plural errors after glyph", "⎿  Connection errors are logged\n", false},
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

func TestMemoryRoundTrip(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "liveness.json")
	if m, err := Load(p); err != nil || !m.LastTick.IsZero() {
		t.Fatalf("missing file: %+v %v", m, err)
	}
	_, mem := run(t, Memory{}, t0, idle(10))
	if err := mem.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !got.LastTick.Equal(t0) || !got.Slots["cc-impl"].Acted {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}

// A gap resets the progress timers but must not forget nudges already sent: the per-hour cap counts
// wall time, which a sleep does not erase.
func TestGapKeepsNudgeCap(t *testing.T) {
	t.Parallel()
	nudged := Input{Agent: agent("b-impl", "idle", 2), WorkLeft: true}
	_, mem := run(t, Memory{}, t0, nudged)
	for i := 1; i <= 3; i++ {
		n := nudged
		n.Agent.StateChangeSeq += int64(i)
		_, mem = run(t, mem, t0.Add(time.Duration(i)*time.Minute), n)
	}
	if len(mem.Slots["b-impl"].Sent) != 3 {
		t.Fatalf("setup failed: %+v", mem.Slots["b-impl"])
	}
	wake := mem.LastTick.Add(20 * time.Minute)
	res, rec := run(t, mem, wake, nudged)
	if !res.Recovery {
		t.Fatalf("a 20 minute gap must enter recovery: %+v", res)
	}
	if len(rec.Slots["b-impl"].Sent) != 3 {
		t.Fatalf("gap forgot the sent nudges: %+v", rec.Slots["b-impl"])
	}
	nudged.Agent.StateChangeSeq = 99
	_, rec = run(t, rec, wake.Add(5*time.Minute), nudged)
	res, _ = run(t, rec, wake.Add(10*time.Minute), nudged)
	if res.Recovery || len(res.Decisions) != 0 {
		t.Fatalf("the cap of %d per hour must still hold after recovery: %+v", MaxPerHour, res)
	}
}
