package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/lock"
)

// Exit codes beyond the child's own: usage errors, a child that could not start, and a lock wait that
// timed out (EX_TEMPFAIL).
const (
	exitFailure  = 1
	exitUsage    = 2
	exitTimeout  = 75
	exitNotFound = 127
)

// env is everything the exec subcommands read from the process, so tests can substitute it.
type env struct {
	home    string
	environ []string
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	// Zero values mean the lock package defaults.
	timeout, poll time.Duration
}

func processEnv() (env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return env{}, err
	}
	return env{home: home, environ: os.Environ(), stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}, nil
}

func (e env) get(key string) string {
	for i := len(e.environ) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(e.environ[i], key+"="); ok {
			return v
		}
	}
	return ""
}

func (e env) stateDir() string { return filepath.Join(e.home, ".claude", "fleet", "state") }

func (e env) tune(s lock.Semaphore) lock.Semaphore {
	if e.timeout > 0 {
		s.Timeout = e.timeout
	}
	if e.poll > 0 {
		s.Poll = e.poll
	}
	return s
}

// splitDash returns the arguments before and after the first "--", and false when there is none or
// nothing follows it.
func splitDash(args []string) (before, cmd []string, ok bool) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:], i+1 < len(args)
		}
	}
	return args, nil, false
}

// runHeavy implements `fleet heavy -- <cmd...>`: wait for one of heavy_slots slots, run the command
// with inherited stdio, return its exit code.
func runHeavy(e env, args []string) int {
	before, cmd, ok := splitDash(args)
	if !ok || len(before) != 0 {
		msg(e.stderr, "usage: fleet heavy -- <cmd...>\n")
		return exitUsage
	}
	if e.get(lock.HeldEnv) != "" {
		return runChild(e, cmd, false)
	}
	cfg, err := config.Load(config.DefaultPath(e.home), e.home)
	if err != nil {
		msg(e.stderr, "fleet heavy: %v\n", err)
		return exitFailure
	}
	slot, err := lock.SlotName(e.get(lock.SlotEnv))
	if err != nil {
		msg(e.stderr, "fleet heavy: %v\n", err)
		return exitFailure
	}
	marker := filepath.Join(e.stateDir(), "heavy-wait", slot)
	sem := e.tune(lock.Heavy(filepath.Join(e.stateDir(), "locks"), cfg.Thresholds.HeavySlots, marker))
	return runLocked(e, sem, cmd, true)
}

// runLock implements `fleet lock <name> -- <cmd...>`: an exclusive named lock around the command.
func runLock(e env, args []string) int {
	before, cmd, ok := splitDash(args)
	if !ok || len(before) != 1 {
		msg(e.stderr, "usage: fleet lock <name> -- <cmd...>\n")
		return exitUsage
	}
	if err := lock.ValidName(before[0]); err != nil {
		msg(e.stderr, "fleet lock: %v\n", err)
		return exitUsage
	}
	sem := e.tune(lock.Named(filepath.Join(e.stateDir(), "locks"), before[0]))
	return runLocked(e, sem, cmd, false)
}

func runLocked(e env, sem lock.Semaphore, cmd []string, heavy bool) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	release, err := sem.Acquire(ctx)
	if errors.Is(err, lock.ErrTimeout) {
		msg(e.stderr, "fleet: %v\n", err)
		return exitTimeout
	}
	if err != nil {
		msg(e.stderr, "fleet: %v\n", err)
		return exitFailure
	}
	defer release()
	stop() // from here signals go to the child, not to the wait
	return runChild(e, cmd, heavy)
}

// runChild runs cmd with the parent's stdio and returns its exit code. Termination signals are
// forwarded so the child is not orphaned holding a lock the parent already dropped. A heavy child
// gets FLEET_HEAVY_HELD so nested `fleet heavy` calls run without taking another slot.
func runChild(e env, argv []string, heavy bool) int {
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = e.stdin, e.stdout, e.stderr
	c.Env = e.environ
	if heavy {
		c.Env = append(append([]string(nil), e.environ...), lock.HeldEnv+"=1")
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	if err := c.Start(); err != nil {
		msg(e.stderr, "fleet: %v\n", err)
		return exitNotFound
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				_ = c.Process.Signal(s) // the child may already be gone
			case <-done:
				return
			}
		}
	}()
	err := c.Wait()
	close(done)
	return exitCode(e, err)
}

func exitCode(e env, err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		msg(e.stderr, "fleet: %v\n", err)
		return exitFailure
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ee.ExitCode()
}
