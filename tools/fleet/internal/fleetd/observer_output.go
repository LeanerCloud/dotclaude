package fleetd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/lock"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

// maxObserverEntries bounds how many directory entries the stale-temp sweep reads.
const maxObserverEntries = 256

type observerOutput struct {
	root, parent *os.Root
	lock         *os.File
	name         string
}

func openObserverOutput(ctx context.Context, paths ObserverPaths) (out *observerOutput, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(filepath.Dir(paths.Output))
	if err != nil {
		return nil, err
	}
	out = &observerOutput{parent: parent, name: filepath.Base(paths.Output)}
	defer func() {
		if err != nil {
			err = errors.Join(err, out.close())
			out = nil
		}
	}()
	if err = out.checkParent(); err != nil {
		return out, err
	}
	if err = parent.Mkdir(out.name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return out, err
	}
	out.root, err = parent.OpenRoot(out.name)
	if err != nil {
		return out, err
	}
	if err = out.checkIdentity(); err != nil {
		return out, err
	}
	if err = out.openLock(paths); err != nil {
		return out, err
	}
	if err = waitObserverLock(ctx, out.lock); err != nil {
		return out, err
	}
	if err = out.initialize(paths); err != nil {
		return out, err
	}
	return out, nil
}

func (out *observerOutput) openLock(paths ObserverPaths) error {
	before, err := out.root.Lstat(observerLock)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := inspectObserverOutput(out.root, paths); err != nil {
			if _, lockErr := out.root.Lstat(observerLock); lockErr == nil {
				return out.openLock(paths)
			}
			return err
		}
		if err := out.checkIdentity(); err != nil {
			return err
		}
		out.lock, err = out.root.OpenFile(observerLock, os.O_RDONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return out.checkIdentity()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		before, err = out.root.Lstat(observerLock)
	}
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("observer lock must be a regular file without links")
	}
	out.lock, err = out.root.OpenFile(observerLock, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	info, err := observerFileInfo(out.lock)
	if err != nil {
		return err
	}
	if !os.SameFile(before, info) {
		return fmt.Errorf("observer lock changed while opening")
	}
	return out.checkIdentity()
}

func (out *observerOutput) checkParent() error {
	name := out.parent.Name()
	canonical, err := filepath.EvalSymlinks(name)
	if err != nil {
		return err
	}
	named, err := os.Stat(name)
	if err != nil {
		return err
	}
	info, err := out.parent.Stat(".")
	if err != nil {
		return err
	}
	if canonical != name || !os.SameFile(named, info) {
		return fmt.Errorf("observer parent identity changed")
	}
	return nil
}

func (out *observerOutput) checkIdentity() error {
	if err := out.checkParent(); err != nil {
		return err
	}
	named, err := out.parent.Lstat(out.name)
	if err != nil {
		return err
	}
	info, err := out.root.Stat(".")
	if err != nil {
		return err
	}
	if named.Mode() != os.ModeDir|0o700 || !os.SameFile(named, info) {
		return fmt.Errorf("observer root identity changed")
	}
	if out.lock == nil {
		return nil
	}
	info, err = observerFileInfo(out.lock)
	if err != nil {
		return err
	}
	named, err = out.root.Lstat(observerLock)
	if err != nil {
		return err
	}
	if info.Size() != 0 || !named.Mode().IsRegular() || !os.SameFile(info, named) {
		return fmt.Errorf("observer lock identity changed or not empty")
	}
	return nil
}

func (out *observerOutput) initialize(paths ObserverPaths) error {
	if err := out.checkIdentity(); err != nil {
		return err
	}
	if err := removeStaleObserverTemps(out.root); err != nil {
		return err
	}
	initialized, err := inspectObserverOutput(out.root, paths)
	if err != nil || initialized {
		return err
	}
	data, err := json.Marshal(expectedObserverBinding(paths))
	if err != nil {
		return err
	}
	if err := out.checkIdentity(); err != nil {
		return err
	}
	return state.WriteFileAtomicRoot(out.root, observerMarker, data, 0o600)
}

// removeStaleObserverTemps deletes the "<artifact>.tmp-<suffix>" files a crashed atomic write leaves
// behind. It runs only with the observer flock held, and every atomic write happens under that flock,
// so no live writer owns them. Only regular files named exactly after the two atomically written
// artifacts are removed; anything else is left for inspectObserverOutput to reject.
func removeStaleObserverTemps(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(maxObserverEntries)
	if err = errors.Join(err, dir.Close()); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	for _, entry := range entries {
		if !isObserverTemp(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		if err := root.Remove(entry.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func isObserverTemp(name string) bool {
	for _, artifact := range []string{observerMarker, observerMemory} {
		if suffix, ok := strings.CutPrefix(name, artifact+".tmp-"); ok && suffix != "" {
			return strings.Trim(suffix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") == ""
		}
	}
	return false
}

func waitObserverLock(ctx context.Context, f *os.File) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("observer flock: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lock.DefaultPoll):
		}
	}
}

func (out *observerOutput) close() error {
	var err error
	if out.lock != nil {
		err = out.lock.Close()
	}
	if out.root != nil {
		err = errors.Join(err, out.root.Close())
	}
	return errors.Join(err, out.parent.Close())
}
