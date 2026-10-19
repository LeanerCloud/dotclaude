// Package lock provides the counting semaphore behind `fleet heavy` and the exclusive named locks
// behind `fleet lock`. macOS has no flock(1), so both use flock(2) on lock files: the kernel drops
// the lock when the holder exits, even by kill -9, so a crash never leaves a stale lock.
//
// The heavy cap is honour-system: FLEET_HEAVY_HELD is an ordinary environment variable, so any
// process that sets it (or clears its inherited copy) can run without a slot. It stops accidental
// self-deadlock in nested `fleet heavy` calls, not a hostile or careless caller.
//
// The flock belongs to the fleet process, not to the child it runs: the lock file is close-on-exec,
// so kill -9 of fleet releases the slot while an orphaned child keeps running, and a later job can
// then run beside it (reproduced: 3 heavy children on 2 slots). Passing the descriptor to the child
// through ExtraFiles would keep the slot for the child's life, but every descendant inherits it
// too, so a daemon the build starts (an ssh ControlPersist master, a gradle or bazel server) would
// hold the slot for hours with no way to find it, and the timeout-based waiters would stall behind
// it. Losing the cap after a SIGKILL of fleet is the lesser failure, so it is accepted. The orphan
// is bounded by what it was already running, and fleet itself cleans up on every other exit path.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

const (
	// HeldEnv is exported by `fleet heavy` into its child. Any nested `fleet heavy` sees it and runs
	// without taking another slot, so `make test` calling `go test` cannot deadlock itself.
	HeldEnv = "FLEET_HEAVY_HELD"
	// SlotEnv names the slot for the heavy-wait marker.
	SlotEnv        = "FLEET_SLOT"
	DefaultTimeout = 60 * time.Minute
	DefaultPoll    = 250 * time.Millisecond
	UnknownSlot    = "unknown"
)

// ErrTimeout means no lock became free within the wait timeout.
var ErrTimeout = errors.New("timed out waiting for a lock")

var (
	nameRE = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	slotRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// ValidName checks a `fleet lock` name. Dots are excluded so a name can never collide with a
// heavy slot file (`heavy.<i>.lock`) or escape the lock directory.
func ValidName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid lock name %q: want [a-z0-9-]{1,64}", name)
	}
	return nil
}

// SlotName returns the slot from the SlotEnv value, UnknownSlot when empty. A set but malformed
// value is an error: it becomes a file name.
func SlotName(v string) (string, error) {
	if v == "" {
		return UnknownSlot, nil
	}
	if !slotRE.MatchString(v) {
		return "", fmt.Errorf("invalid %s %q: want a slot name like cc-impl", SlotEnv, v)
	}
	return v, nil
}

// Semaphore is a counting semaphore over the lock files Paths. At most len(Paths) holders run at once.
type Semaphore struct {
	Paths   []string
	Timeout time.Duration
	Poll    time.Duration
	// Marker, if set, is the base path of the heavy-wait marker. The file written is MarkerFile(Marker,
	// pid), so concurrent waiters never share or delete each other's marker. It is written when the
	// first attempt fails and removed before Acquire returns, so the hang detector can tell a queued
	// slot from a stuck one. Acquire first removes stale markers of the same base (dead pid, or older
	// than Timeout).
	Marker string
}

// Heavy returns the semaphore for n heavy slots, with files under locksDir.
func Heavy(locksDir string, n int, marker string) Semaphore {
	paths := make([]string, n)
	for i := range paths {
		paths[i] = filepath.Join(locksDir, fmt.Sprintf("heavy.%d.lock", i))
	}
	return Semaphore{Paths: paths, Timeout: DefaultTimeout, Poll: DefaultPoll, Marker: marker}
}

// Named returns an exclusive lock called name, which must pass ValidName.
func Named(locksDir, name string) Semaphore {
	return Semaphore{Paths: []string{filepath.Join(locksDir, name+".lock")}, Timeout: DefaultTimeout, Poll: DefaultPoll}
}

// Acquire takes one free slot, polling until Timeout or ctx ends. The returned release is idempotent.
func (s Semaphore) Acquire(ctx context.Context) (release func(), err error) {
	if len(s.Paths) == 0 {
		return nil, errors.New("semaphore has no lock files")
	}
	if err := os.MkdirAll(filepath.Dir(s.Paths[0]), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	marked := false
	own := MarkerFile(s.Marker, os.Getpid())
	if s.Marker != "" {
		reapStaleMarkers(s.Marker, time.Now(), s.Timeout, pidAlive)
	}
	defer func() {
		if marked {
			_ = os.Remove(own) // best effort: a stale marker is ignored by HeavyWaiting and reaped later
		}
	}()
	for {
		f, err := s.tryAll()
		if err != nil {
			return nil, err
		}
		if f != nil {
			return func() { _ = f.Close() }, nil // closing drops the flock
		}
		if s.Marker != "" && !marked {
			if err := writeMarker(own); err != nil {
				return nil, err
			}
			marked = true
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w (%s)", ErrTimeout, s.Timeout)
			}
			return nil, ctx.Err()
		case <-time.After(s.Poll):
		}
	}
}

func writeMarker(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create marker dir: %w", err)
	}
	body := fmt.Sprintf("pid %d since %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if err := state.WriteFileAtomic(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write heavy-wait marker: %w", err)
	}
	return nil
}

// MarkerFile is the heavy-wait marker of process pid for the marker base path.
func MarkerFile(base string, pid int) string { return fmt.Sprintf("%s.%d", base, pid) }

// markers lists the marker files of base as path and pid, skipping temp files and anything else
// whose suffix is not a pid.
func markers(base string) (paths []string, pids []int) {
	matches, _ := filepath.Glob(base + ".*") // the only error is a malformed pattern; base has none
	for _, m := range matches {
		pid, err := strconv.Atoi(strings.TrimPrefix(m, base+"."))
		if err == nil && pid > 0 {
			paths, pids = append(paths, m), append(pids, pid)
		}
	}
	return paths, pids
}

// pidAlive reports whether a process with this pid exists (kill(pid, 0)); EPERM means it does.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// live reports whether the marker file is from a live process and younger than maxAge.
func live(path string, pid int, now time.Time, maxAge time.Duration, alive func(int) bool) bool {
	fi, err := os.Stat(path)
	if err != nil || now.Sub(fi.ModTime()) >= maxAge {
		return false
	}
	return alive(pid)
}

// HeavyWaiting reports whether any live marker exists for the slot's marker base: its pid is alive
// and its age is under maxAge (the heavy wait timeout). Stale markers are ignored.
func HeavyWaiting(base string, now time.Time, maxAge time.Duration) bool {
	return heavyWaiting(base, now, maxAge, pidAlive)
}

func heavyWaiting(base string, now time.Time, maxAge time.Duration, alive func(int) bool) bool {
	paths, pids := markers(base)
	for i, p := range paths {
		if live(p, pids[i], now, maxAge, alive) {
			return true
		}
	}
	return false
}

func reapStaleMarkers(base string, now time.Time, maxAge time.Duration, alive func(int) bool) {
	paths, pids := markers(base)
	for i, p := range paths {
		if !live(p, pids[i], now, maxAge, alive) {
			_ = os.Remove(p) // best effort: HeavyWaiting ignores it either way
		}
	}
}

// tryAll attempts a non-blocking flock on each file in turn and returns the open, locked file, or
// nil when all are held.
func (s Semaphore) tryAll() (*os.File, error) {
	for _, p := range s.Paths {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open lock file: %w", err)
		}
		for {
			err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		switch {
		case err == nil:
			return f, nil
		case errors.Is(err, syscall.EWOULDBLOCK):
			_ = f.Close() // not ours; nothing to flush
		default:
			_ = f.Close()
			return nil, fmt.Errorf("flock %s: %w", p, err)
		}
	}
	return nil, nil
}
