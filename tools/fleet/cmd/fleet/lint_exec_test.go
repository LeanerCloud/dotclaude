package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/lock"
)

func lintTestEnv(t *testing.T, slots int, extra ...string) (env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	return testEnv(t, slots, append([]string{lock.HeldEnv + "=", lock.SlotEnv + "=lint-fixture"}, extra...)...)
}

func fakeLint(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "golangci-lint")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec /bin/sh \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitLintFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("file never appeared: %s", path)
}

func lintMarker(e env, c *exec.Cmd) string {
	return lock.MarkerFile(filepath.Join(e.stateDir(), "heavy-wait", e.get(lock.SlotEnv)), c.Process.Pid)
}

func releaseLint(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLintEnvironmentAndExitCodes(t *testing.T) {
	t.Parallel()
	lint := fakeLint(t)
	e, out, _ := lintTestEnv(t, 2, "GOGC=400", "GOGC=800", "KEEP=yes")
	original := append([]string(nil), e.environ...)
	if got := runHeavy(e, []string{"--", lint, "-c", `printf '%s %s %s\n' "$GOGC" "$KEEP" "$FLEET_HEAVY_HELD"; exit 7`}); got != 7 || out.String() != "50 yes 1\n" {
		t.Fatalf("exit %d output %q", got, out.String())
	}
	if !reflect.DeepEqual(e.environ, original) {
		t.Fatal("parent environment changed")
	}
	out.Reset()
	if got := runHeavy(e, []string{"--", "sh", "-c", `printf '%s\n' "$GOGC"`}); got != 0 || out.String() != "800\n" {
		t.Fatalf("non-lint heavy exit %d output %q", got, out.String())
	}
	for _, argv := range [][]string{{"sh", "-c", "golangci-lint"}, {"golangci-lint-wrapper"}} {
		c := newChild(e, argv, true)
		if !reflect.DeepEqual(c.Env, childEnv(original, true)) {
			t.Fatalf("non-lint environment changed: %v", c.Env)
		}
	}
	if got := runHeavy(e, []string{"--", lint, "-c", "kill -TERM $$"}); got != 143 {
		t.Fatalf("signaled lint exit %d", got)
	}
	if got := runHeavy(e, []string{"--", filepath.Join(t.TempDir(), "golangci-lint")}); got != exitNotFound {
		t.Fatalf("missing lint exit %d", got)
	}
	if got := runHeavy(e, []string{"--", lint, "-c", "exit 0"}); got != 0 {
		t.Fatalf("locks leaked after exit: %d", got)
	}
}

func TestLintDirectCallsSerialize(t *testing.T) {
	t.Parallel()
	e, _, _ := lintTestEnv(t, 2)
	lint := fakeLint(t)
	gate := filepath.Join(t.TempDir(), "release")
	first, out := startFleet(t, e, nil, "heavy", "--", lint, "-c", `echo ready; while [ ! -f "$1" ]; do sleep 0.01; done`, "sh", gate)
	waitLine(t, out, "ready")
	ran := filepath.Join(t.TempDir(), "ran")
	second, _ := startFleet(t, e, nil, "heavy", "--", lint, "-c", `touch "$1"`, "sh", ran)
	waitLintFile(t, lintMarker(e, second))
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatalf("second lint started while first held lock: %v", err)
	}
	releaseLint(t, gate)
	if got := exitOf(t, first); got != 0 {
		t.Fatalf("first exit %d", got)
	}
	if got := exitOf(t, second); got != 0 {
		t.Fatalf("second exit %d", got)
	}
	waitLintFile(t, ran)
}

func TestLintInheritedHeavyWithoutConfigStillLocks(t *testing.T) {
	t.Parallel()
	e := env{home: t.TempDir(), environ: append(os.Environ(), lock.HeldEnv+"=1", lock.SlotEnv+"=lint-fixture"), stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard, timeout: 100 * time.Millisecond, poll: time.Millisecond}
	sem := lock.Named(filepath.Join(e.stateDir(), "locks"), "golangci-lint")
	release, err := sem.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	lint := fakeLint(t)
	if got := runHeavy(e, []string{"--", lint, "-c", "exit 0"}); got != exitTimeout {
		t.Fatalf("inherited heavy bypassed lint singleton: exit %d", got)
	}
	if got := runHeavy(e, []string{"--", "sh", "-c", "exit 9"}); got != 9 {
		t.Fatalf("non-lint inherited path read config: %d", got)
	}
	release()
	if got := runHeavy(e, []string{"--", lint, "-c", "exit 4"}); got != 4 {
		t.Fatalf("lint inherited path read config or leaked lock: %d", got)
	}
}

func TestLintCancellationReleasesHeavyAndMarker(t *testing.T) {
	t.Parallel()
	e, _, _ := lintTestEnv(t, 1)
	sem := lock.Named(filepath.Join(e.stateDir(), "locks"), "golangci-lint")
	release, err := sem.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ran := filepath.Join(t.TempDir(), "ran")
	c, _ := startFleet(t, e, nil, "heavy", "--", fakeLint(t), "-c", `touch "$1"`, "sh", ran)
	marker := lintMarker(e, c)
	waitLintFile(t, marker)
	if err := c.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if got := exitOf(t, c); got != 143 {
		t.Fatalf("cancelled lint exit %d", got)
	}
	for _, path := range []string{marker, ran} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected file after cancellation %s: %v", path, err)
		}
	}
	e.timeout = 100 * time.Millisecond
	if got := runHeavy(e, []string{"--", "true"}); got != 0 {
		t.Fatalf("general heavy slot leaked: %d", got)
	}
}

func TestLintAcquiresHeavyBeforeSingleton(t *testing.T) {
	t.Parallel()
	e, _, _ := lintTestEnv(t, 1)
	heavy, err := lock.Heavy(filepath.Join(e.stateDir(), "locks"), 1, "").Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer heavy()
	lint := fakeLint(t)
	queued, _ := startFleet(t, e, nil, "heavy", "--", lint, "-c", "exit 0")
	waitLintFile(t, lintMarker(e, queued))
	gate := filepath.Join(t.TempDir(), "release")
	inherited, out := startFleet(t, e, []string{lock.HeldEnv + "=1"}, "heavy", "--", lint, "-c", `echo inherited; while [ ! -f "$1" ]; do sleep 0.01; done`, "sh", gate)
	waitLine(t, out, "inherited")
	heavy()
	releaseLint(t, gate)
	if got := exitOf(t, inherited); got != 0 {
		t.Fatalf("inherited exit %d", got)
	}
	if got := exitOf(t, queued); got != 0 {
		t.Fatalf("queued exit %d", got)
	}
}

func TestLintInteroperatesWithMacOSLockf(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS lockf")
	}
	t.Parallel()
	t.Run("lockf-holder", func(t *testing.T) {
		e, _, _ := lintTestEnv(t, 1)
		dir := filepath.Join(e.stateDir(), "locks")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		holder := exec.Command("/usr/bin/lockf", "-k", filepath.Join(dir, "golangci-lint.lock"), "sh", "-c", "echo locked; read release")
		in, err := holder.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = in.Close() }()
		out, err := holder.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := holder.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = holder.Process.Kill() })
		waitLine(t, bufio.NewReader(out), "locked")
		ran := filepath.Join(t.TempDir(), "ran")
		c, _ := startFleet(t, e, nil, "heavy", "--", fakeLint(t), "-c", `touch "$1"`, "sh", ran)
		waitLintFile(t, lintMarker(e, c))
		if _, err := os.Stat(ran); !os.IsNotExist(err) {
			t.Fatalf("fleet ignored lockf holder: %v", err)
		}
		if _, err := io.WriteString(in, "release\n"); err != nil {
			t.Fatal(err)
		}
		if err := holder.Wait(); err != nil {
			t.Fatal(err)
		}
		if got := exitOf(t, c); got != 0 {
			t.Fatalf("fleet exit %d", got)
		}
		waitLintFile(t, ran)
	})
	t.Run("fleet-holder", func(t *testing.T) {
		e, _, _ := lintTestEnv(t, 1)
		gate := filepath.Join(t.TempDir(), "release")
		c, out := startFleet(t, e, nil, "heavy", "--", fakeLint(t), "-c", `echo locked; while [ ! -f "$1" ]; do sleep 0.01; done`, "sh", gate)
		waitLine(t, out, "locked")
		path := filepath.Join(e.stateDir(), "locks", "golangci-lint.lock")
		probe := func(want int) {
			t.Helper()
			cmd := exec.Command("/usr/bin/lockf", "-k", "-n", "-t", "0", path, "true")
			err := cmd.Run()
			got := 0
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					got = ee.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if got != want {
				t.Fatalf("lockf exit %d, want %d", got, want)
			}
		}
		probe(75)
		releaseLint(t, gate)
		if got := exitOf(t, c); got != 0 {
			t.Fatalf("fleet exit %d", got)
		}
		probe(0)
	})
}
