// Package lock provides the counting semaphore behind `fleet heavy` and the exclusive named locks
// behind `fleet lock`. macOS has no flock(1), so both use flock(2) on lock files: the kernel drops
// the lock when the holder exits, even by kill -9, so a crash never leaves a stale lock.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// Marker, if set, is written when the first attempt fails and removed before Acquire returns, so
	// the hang detector can tell a queued slot from a stuck one.
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
	defer func() {
		if marked {
			_ = os.Remove(s.Marker) // best effort: a leftover marker only delays one hang report
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
			if err := writeMarker(s.Marker); err != nil {
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
