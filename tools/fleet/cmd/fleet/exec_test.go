package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/lock"
)

// TestMain lets the test binary act as the fleet binary, so nested `fleet heavy` calls are real
// processes.
func TestMain(m *testing.M) {
	if os.Getenv("FLEET_TEST_AS_FLEET") == "1" {
		e, err := processEnv()
		if err != nil {
			os.Exit(1)
		}
		e.timeout, e.poll = 3*time.Second, 10*time.Millisecond
		os.Exit(dispatch(e, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func testEnv(t *testing.T, heavySlots int, extra ...string) (env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "fleet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"repos":[{"name":"r","owner":"o","checkout":"~","slots":{"impl":1}}],
"roles":{"coordinator":{"prefer":["opencode"],"min_tier":"low"},"impl":{"prefer":["kimi"],"min_tier":"medium"},
"merge":{"prefer":["claude"],"min_tier":"high"},"reviewer":{"prefer":["codex"],"min_tier":"high"}},
"thresholds":{"heavy_slots":` + string(rune('0'+heavySlots)) + `}}`
	if err := os.WriteFile(filepath.Join(dir, "fleet.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	return env{home: home, environ: append(os.Environ(), extra...), stdin: strings.NewReader(""), stdout: &out, stderr: &errb,
		timeout: 5 * time.Second, poll: 5 * time.Millisecond}, &out, &errb
}

func TestHeavyPropagatesExitCodeAndStdio(t *testing.T) {
	t.Parallel()
	e, out, _ := testEnv(t, 2)
	if got := runHeavy(e, []string{"--", "sh", "-c", "echo hi; exit 7"}); got != 7 {
		t.Fatalf("exit code %d, want 7", got)
	}
	if out.String() != "hi\n" {
		t.Fatalf("stdout %q", out.String())
	}
	if got := runHeavy(e, []string{"--", "true"}); got != 0 {
		t.Fatalf("exit code %d, want 0", got)
	}
	if got := runHeavy(e, []string{"--", "sh", "-c", "kill -TERM $$"}); got != 128+15 {
		t.Fatalf("signaled child exit %d, want 143", got)
	}
}

func TestHeavyExportsHeldAndKeepsEnv(t *testing.T) {
	t.Parallel()
	e, out, _ := testEnv(t, 1, "FLEET_MARK=x")
	if got := runHeavy(e, []string{"--", "sh", "-c", "echo $FLEET_HEAVY_HELD $FLEET_MARK"}); got != 0 || out.String() != "1 x\n" {
		t.Fatalf("exit %d out %q", got, out.String())
	}
}

func TestHeavyCommandNotFound(t *testing.T) {
	t.Parallel()
	e, _, errb := testEnv(t, 1)
	if got := runHeavy(e, []string{"--", "/nonexistent/binary"}); got != exitNotFound || errb.Len() == 0 {
		t.Fatalf("exit %d stderr %q", got, errb.String())
	}
}

func TestHeavyUsage(t *testing.T) {
	t.Parallel()
	e, _, _ := testEnv(t, 1)
	for _, args := range [][]string{nil, {"true"}, {"--"}, {"x", "--", "true"}} {
		if got := runHeavy(e, args); got != exitUsage {
			t.Errorf("%v: exit %d, want 2", args, got)
		}
	}
}

func TestHeavyRequiresValidConfigAndSlot(t *testing.T) {
	t.Parallel()
	e, _, errb := testEnv(t, 1)
	if err := os.Remove(filepath.Join(e.home, ".claude", "fleet", "fleet.json")); err != nil {
		t.Fatal(err)
	}
	if got := runHeavy(e, []string{"--", "true"}); got != exitFailure || !strings.Contains(errb.String(), "config") {
		t.Fatalf("missing config: exit %d %q", got, errb.String())
	}
	e2, _, errb2 := testEnv(t, 1, "FLEET_SLOT=../evil")
	if got := runHeavy(e2, []string{"--", "true"}); got != exitFailure || !strings.Contains(errb2.String(), "FLEET_SLOT") {
		t.Fatalf("bad slot: exit %d %q", got, errb2.String())
	}
}

func TestHeavyWaitsWhenAllSlotsHeldAndTimesOut(t *testing.T) {
	t.Parallel()
	e, _, errb := testEnv(t, 1, "FLEET_SLOT=cc-impl")
	e.timeout = 150 * time.Millisecond
	holder := lock.Heavy(filepath.Join(e.stateDir(), "locks"), 1, "")
	rel, err := holder.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	if got := runHeavy(e, []string{"--", "true"}); got != exitTimeout || !strings.Contains(errb.String(), "timed out") {
		t.Fatalf("exit %d %q", got, errb.String())
	}
	if _, err := os.Stat(filepath.Join(e.stateDir(), "heavy-wait", "cc-impl")); !os.IsNotExist(err) {
		t.Fatal("heavy-wait marker left after the timeout")
	}
}

func TestHeavyRunsOnlyNAtOnce(t *testing.T) {
	t.Parallel()
	e, _, _ := testEnv(t, 2)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Each of 3 jobs records start and end in a shared log; with 2 slots the third starts after one ends.
	log := filepath.Join(t.TempDir(), "log")
	script := `echo "start $1" >> ` + log + `; sleep 0.4; echo "end $1" >> ` + log
	var cmds []*exec.Cmd
	for _, id := range []string{"a", "b", "c"} {
		c := exec.Command(self, "heavy", "--", "sh", "-c", script, "sh", id)
		c.Env = append(e.environ, "FLEET_TEST_AS_FLEET=1", "HOME="+e.home)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, c)
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	running, peak := 0, 0
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(l, "start") {
			running++
			peak = max(peak, running)
		} else {
			running--
		}
	}
	if peak != 2 {
		t.Fatalf("peak concurrency %d, want 2:\n%s", peak, data)
	}
}

func TestNestedHeavyDoesNotDeadlockWithOneSlot(t *testing.T) {
	t.Parallel()
	e, _, _ := testEnv(t, 1)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command(self, "heavy", "--", self, "heavy", "--", self, "heavy", "--", "sh", "-c", "exit 3")
	c.Env = append(e.environ, "FLEET_TEST_AS_FLEET=1", "HOME="+e.home)
	done := make(chan error, 1)
	go func() { done <- c.Run() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 3 {
			t.Fatalf("want exit 3, got %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = c.Process.Kill()
		t.Fatal("nested fleet heavy deadlocked with heavy_slots=1")
	}
}

func TestLockSerializesAndValidates(t *testing.T) {
	t.Parallel()
	e, out, errb := testEnv(t, 1)
	for _, bad := range []string{"Bad", "a.b", "../x", "", "a b", strings.Repeat("a", 65)} {
		if got := runLock(e, []string{bad, "--", "true"}); got != exitUsage {
			t.Errorf("name %q: exit %d, want 2", bad, got)
		}
	}
	if !strings.Contains(errb.String(), "invalid lock name") {
		t.Fatalf("stderr %q", errb.String())
	}
	if got := runLock(e, []string{"push-repo", "--", "sh", "-c", "echo in; exit 5"}); got != 5 || out.String() != "in\n" {
		t.Fatalf("exit %d out %q", got, out.String())
	}
	for _, args := range [][]string{nil, {"x"}, {"--", "true"}, {"x", "y", "--", "true"}, {"x", "--"}} {
		if got := runLock(e, args); got != exitUsage {
			t.Errorf("%v: exit %d, want 2", args, got)
		}
	}
	rel, err := lock.Named(filepath.Join(e.stateDir(), "locks"), "push-repo").Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	e.timeout = 100 * time.Millisecond
	if got := runLock(e, []string{"push-repo", "--", "true"}); got != exitTimeout {
		t.Fatalf("held lock: exit %d, want %d", got, exitTimeout)
	}
}

func TestDispatchKeepsOtherSubcommandsStubbed(t *testing.T) {
	t.Parallel()
	var errb bytes.Buffer
	if got := dispatch(env{stderr: &errb}, []string{"tick"}); got != 2 || !strings.Contains(errb.String(), "not implemented") {
		t.Fatalf("exit %d %q", got, errb.String())
	}
}
