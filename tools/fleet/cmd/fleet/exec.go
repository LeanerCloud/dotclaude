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
	"unsafe"

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
		return runWithSignals(e, nil, cmd, true)
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
	return runWithSignals(e, &sem, cmd, heavy)
}

// runWithSignals registers for termination signals before anything can block or start, so none is
// lost between taking the lock and starting the child. sem nil means no lock is taken (the child
// already holds a heavy slot through FLEET_HEAVY_HELD); the child is built and run the same way.
func runWithSignals(e env, sem *lock.Semaphore, argv []string, heavy bool) int {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	if sem != nil {
		release, code, ok := acquire(e, *sem, sigs)
		if !ok {
			return code
		}
		defer release()
	}
	return runChild(e, argv, heavy, sigs)
}

// acquire waits for the semaphore. A signal during the wait cancels it, removes the marker (Acquire
// does) and exits 128+signal; a signal that lands just as the lock is taken is honoured too, before
// any child starts. A signal the watcher did not consume stays in sigs for runChild to forward.
func acquire(e env, sem lock.Semaphore, sigs <-chan os.Signal) (release func(), code int, ok bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	caught := make(chan os.Signal, 1)
	stopWatch, watchDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case sig := <-sigs:
			caught <- sig
			cancel()
		case <-stopWatch:
		}
	}()
	release, err := sem.Acquire(ctx)
	close(stopWatch)
	<-watchDone
	sig := pending(caught, sigs)
	if sig != nil {
		if release != nil {
			release()
		}
		msg(e.stderr, "fleet: %v while waiting for a lock\n", sig)
		return nil, signalExit(sig), false
	}
	if errors.Is(err, lock.ErrTimeout) {
		msg(e.stderr, "fleet: %v\n", err)
		return nil, exitTimeout, false
	}
	if err != nil {
		msg(e.stderr, "fleet: %v\n", err)
		return nil, exitFailure, false
	}
	return release, 0, true
}

// pending returns the signal the watcher caught, else one still queued in sigs (the watcher may have
// stopped first), else nil. A signal arriving after this check is forwarded to the child instead.
func pending(caught <-chan os.Signal, sigs <-chan os.Signal) os.Signal {
	select {
	case sig := <-caught:
		return sig
	case sig := <-sigs:
		return sig
	default:
		return nil
	}
}

func signalExit(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return exitFailure
}

// pipeDrainGrace bounds how long Wait lingers on output pipes a stray grandchild still holds open.
const pipeDrainGrace = 2 * time.Second

// childEnv is the child's environment. A heavy child (one that holds a slot, taken now or inherited)
// gets exactly one FLEET_HEAVY_HELD=1 so nested `fleet heavy` calls run without taking another slot.
func childEnv(environ []string, heavy bool) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, lock.HeldEnv+"=") {
			out = append(out, kv)
		}
	}
	if heavy {
		out = append(out, lock.HeldEnv+"=1")
	}
	return out
}

// newChild builds the child for both the locked and the FLEET_HEAVY_HELD path, so any later
// sandboxing applies to both.
//
// Without a terminal on stdin (the agent case: harness shell tools run without a tty) the child
// leads its own process group, so signals and the final cleanup reach grandchildren too. With a
// terminal it stays in the caller's group, as any shell command does: ^C already reaches the whole
// job, and fleet never touches the terminal's foreground group (a group left in the background
// would be stopped by SIGTTIN on its next read, and restoring it is not fleet's business).
func newChild(e env, argv []string, heavy bool) *exec.Cmd {
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = e.stdin, e.stdout, e.stderr
	c.Env = childEnv(e.environ, heavy)
	if f, ok := e.stdin.(*os.File); !ok || !isTTY(f) {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	c.WaitDelay = pipeDrainGrace
	return c
}

// isTTY reports whether f is a terminal, by asking for its foreground process group.
func isTTY(f *os.File) bool {
	var pgrp int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&pgrp)))
	return errno == 0
}

// runChild runs cmd with the parent's stdio and returns its exit code. Termination signals from sigs
// are forwarded to the child's whole process group so nothing is orphaned holding a lock the
// parent is about to drop, and whatever the child left running in that group is killed when it exits.
func runChild(e env, argv []string, heavy bool, sigs <-chan os.Signal) int {
	c := newChild(e, argv, heavy)
	if err := c.Start(); err != nil {
		msg(e.stderr, "fleet: %v\n", err)
		return exitNotFound
	}
	grouped := c.SysProcAttr != nil && c.SysProcAttr.Setpgid
	pgid := c.Process.Pid // Setpgid made the child its group's leader
	done, forwarded := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(forwarded)
		for {
			select {
			case sig := <-sigs:
				s, ok := sig.(syscall.Signal)
				switch {
				case ok && grouped:
					_ = syscall.Kill(-pgid, s) // the group may already be gone
				case ok:
					_ = c.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()
	err := c.Wait()
	close(done)
	<-forwarded
	if grouped {
		_ = syscall.Kill(-pgid, syscall.SIGKILL) // ESRCH when nothing is left, which is the normal case
	}
	return exitCode(e, c, err)
}

func exitCode(e env, c *exec.Cmd, err error) int {
	if errors.Is(err, exec.ErrWaitDelay) && c.ProcessState != nil {
		// the child itself finished; only a stray holder of its pipes outlived it
		if ws, ok := c.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return c.ProcessState.ExitCode()
	}
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
