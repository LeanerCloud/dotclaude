package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/lock"
)

// TestMain lets the test binary act as the fleet binary, so nested `fleet heavy` calls are real
// processes.
func TestMain(m *testing.M) {
	if tickFixtureCommand() {
		return
	}
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
	if m, _ := filepath.Glob(filepath.Join(e.stateDir(), "heavy-wait", "cc-impl.*")); len(m) != 0 {
		t.Fatalf("heavy-wait marker left after the timeout: %v", m)
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
	if got := dispatch(env{stderr: &errb}, []string{"cleanup"}); got != 2 || !strings.Contains(errb.String(), "not implemented") {
		t.Fatalf("exit %d %q", got, errb.String())
	}
}

func TestExitCodeValues(t *testing.T) {
	t.Parallel()
	if exitTimeout != 75 || exitNotFound != 127 || exitUsage != 2 || exitFailure != 1 {
		t.Fatalf("exit codes are a contract: %d %d %d %d", exitTimeout, exitNotFound, exitUsage, exitFailure)
	}
}

// startFleet runs the test binary as `fleet <args>` in its own process, with stdout on a pipe.
func startFleet(t *testing.T, e env, extra []string, args ...string) (*exec.Cmd, *bufio.Reader) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command(self, args...)
	c.Env = append(append([]string(nil), e.environ...), "FLEET_TEST_AS_FLEET=1", "HOME="+e.home)
	c.Env = append(c.Env, extra...)
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill() })
	return c, bufio.NewReader(out)
}

func waitLine(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		l, _ := r.ReadString('\n')
		got <- strings.TrimSpace(l)
	}()
	select {
	case l := <-got:
		if l != want {
			t.Fatalf("read %q, want %q", l, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}

func exitOf(t *testing.T, c *exec.Cmd) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if err == nil {
			return 0
		}
		if !errors.As(err, &ee) {
			t.Fatal(err)
		}
		return ee.ExitCode()
	case <-time.After(15 * time.Second):
		t.Fatal("fleet did not exit")
	}
	return -1
}

func TestSignalsReachTheChild(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sig  syscall.Signal
		name string
	}{{syscall.SIGINT, "INT"}, {syscall.SIGTERM, "TERM"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, _, _ := testEnv(t, 1)
			script := `trap 'echo got-` + tc.name + `; exit 9' ` + tc.name + `; echo ready; while :; do sleep 0.05; done`
			c, out := startFleet(t, e, nil, "heavy", "--", "sh", "-c", script)
			waitLine(t, out, "ready")
			if err := c.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			waitLine(t, out, "got-"+tc.name)
			if got := exitOf(t, c); got != 9 {
				t.Fatalf("exit %d, want the child's 9", got)
			}
		})
	}
}

func processGone(pid int) bool {
	for range 200 {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestSIGTERMKillsGrandchildrenAndFreesTheSlot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		extra []string
		args  []string
	}{
		{"heavy", nil, []string{"heavy", "--"}},
		{"held", []string{lock.HeldEnv + "=1"}, []string{"heavy", "--"}},
		{"lock", nil, []string{"lock", "x", "--"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, _, _ := testEnv(t, 1)
			pidFile := filepath.Join(t.TempDir(), "pid")
			script := `sleep 30 & echo $! > ` + pidFile + `; echo ready; wait`
			c, out := startFleet(t, e, tc.extra, append(tc.args, "sh", "-c", script)...)
			waitLine(t, out, "ready")
			data, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if got := exitOf(t, c); got != 128+int(syscall.SIGTERM) {
				t.Fatalf("exit %d, want 143", got)
			}
			if !processGone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Fatalf("grandchild %d survived fleet", pid)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			rel, err := lock.Heavy(filepath.Join(e.stateDir(), "locks"), 1, "").Acquire(ctx)
			if err != nil {
				t.Fatalf("slot not free after fleet exit: %v", err)
			}
			rel()
		})
	}
}

func TestSIGTERMWhileWaitingExits143AndRemovesMarker(t *testing.T) {
	t.Parallel()
	e, _, _ := testEnv(t, 1, "FLEET_SLOT=cc-impl")
	rel, err := lock.Heavy(filepath.Join(e.stateDir(), "locks"), 1, "").Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	ran := filepath.Join(t.TempDir(), "ran")
	c, _ := startFleet(t, e, nil, "heavy", "--", "touch", ran)
	pattern := filepath.Join(e.stateDir(), "heavy-wait", "cc-impl."+strconv.Itoa(c.Process.Pid))
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(pattern); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("marker never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if got := exitOf(t, c); got != 143 {
		t.Fatalf("exit %d, want 128+SIGTERM", got)
	}
	if _, err := os.Stat(pattern); !os.IsNotExist(err) {
		t.Fatal("marker left after a cancelled wait")
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("the command ran although the wait was cancelled")
	}
}

func TestSignalDuringAcquireNeverStartsTheChild(t *testing.T) {
	t.Parallel()
	e, _, errb := testEnv(t, 1)
	ran := filepath.Join(t.TempDir(), "ran")
	sigs := make(chan os.Signal, 4)
	sigs <- syscall.SIGTERM // already pending when the lock is taken
	sem := e.tune(lock.Heavy(filepath.Join(e.stateDir(), "locks"), 1, ""))
	_, code, ok := acquire(e, sem, sigs)
	if ok || code != 143 || !strings.Contains(errb.String(), "terminated") {
		t.Fatalf("ok=%v code=%d %q", ok, code, errb.String())
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("child ran")
	}
	// The slot must have been released again.
	if rel, err := sem.Acquire(t.Context()); err != nil {
		t.Fatalf("slot leaked: %v", err)
	} else {
		rel()
	}
}

func TestHeldAndLockedPathsBuildTheChildTheSameWay(t *testing.T) {
	t.Parallel()
	environ := []string{"A=1", lock.HeldEnv + "=1", "B=2"}
	held := newChild(env{environ: environ}, []string{"true"}, true)
	locked := newChild(env{environ: []string{"A=1", "B=2"}}, []string{"true"}, true)
	norm := func(c *exec.Cmd) string {
		e := append([]string(nil), c.Env...)
		sort.Strings(e)
		return strings.Join(e, ",")
	}
	if norm(held) != norm(locked) || norm(held) != "A=1,B=2,"+lock.HeldEnv+"=1" {
		t.Fatalf("env differs: %q vs %q", norm(held), norm(locked))
	}
	for name, c := range map[string]*exec.Cmd{"held": held, "locked": locked} {
		if c.SysProcAttr == nil || !c.SysProcAttr.Setpgid {
			t.Errorf("%s: child must lead its own process group", name)
		}
	}
	plain := newChild(env{environ: environ}, []string{"true"}, false)
	if strings.Contains(strings.Join(plain.Env, ","), lock.HeldEnv) {
		t.Fatal("a non-heavy child must not carry the held marker")
	}
}

func TestSIGTERMIgnoringGrandchildDoesNotSurviveExit(t *testing.T) {
	t.Parallel()
	e, _, _ := testEnv(t, 1)
	pidFile := filepath.Join(t.TempDir(), "pid")
	// The subshell ignores TERM and outlives sh; only the final group SIGKILL can remove it.
	script := `(trap "" TERM; sleep 30) & echo $! > ` + pidFile + `; exit 0`
	startFleet(t, e, nil, "heavy", "--", "sh", "-c", script)
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("no grandchild pid")
	}
	if !processGone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("SIGTERM-ignoring grandchild %d survived fleet", pid)
	}
}

const ptyHelper = `import os, pty, sys

def feed(fd):
    try:
        os.write(fd, b"hello\n")
        return os.read(fd, 1024)
    except OSError:
        return b""

status = pty.spawn(["bash", "-c", sys.argv[1] + " heavy -- true; echo after; read x"], feed)
sys.exit(os.waitstatus_to_exitcode(status))
`

// TestNoTerminalGroupChangeOnPty runs `bash -c 'fleet heavy -- true; echo after; read x'` on a real
// pty: fleet must not leave the terminal's foreground group on a dead child, which would stop the
// following read with SIGTTIN.
func TestNoTerminalGroupChangeOnPty(t *testing.T) {
	t.Parallel()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found: the pty regression test needs it")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e, _, _ := testEnv(t, 1)
	hp := filepath.Join(t.TempDir(), "pty_helper.py")
	if err := os.WriteFile(hp, []byte(ptyHelper), 0o600); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(py, "-I", hp, self)
	c.Env = append(append([]string(nil), e.environ...), "FLEET_TEST_AS_FLEET=1", "HOME="+e.home)
	done := make(chan error, 1)
	go func() { done <- c.Run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bash on a pty did not finish cleanly (read stopped by SIGTTIN?): %v", err)
		}
	case <-time.After(20 * time.Second):
		_ = c.Process.Kill()
		t.Fatal("pty session hung")
	}
}
