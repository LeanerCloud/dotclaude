package fleetd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func observerOutputFixture(t *testing.T) (string, ObserverPaths) {
	t.Helper()
	dir, paths, _ := observerArtifactFixture(t)
	var err error
	paths.Output, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return paths.Output, paths
}

func TestObserverOutputRefusesRedirectBeforeCreation(t *testing.T) {
	t.Parallel()
	dir, paths := observerOutputFixture(t)
	outside := t.TempDir()
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	paths.Output = filepath.Join(dir, "new")
	if out, err := openObserverOutput(t.Context(), paths); err == nil {
		_ = out.close()
		t.Fatal("redirected parent accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("redirected directory changed", err)
	}
}

func TestObserverOutputConcurrentInitializationAndReuse(t *testing.T) {
	t.Parallel()
	dir, paths := observerOutputFixture(t)
	paths.Output = filepath.Join(dir, "fresh")
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		workers.Go(func() {
			<-start
			out, err := openObserverOutput(t.Context(), paths)
			if err != nil {
				t.Error(err)
				return
			}
			if ok, err := inspectObserverOutput(out.root, paths); err != nil || !ok {
				t.Error("uninitialized output", err)
			}
			if err := out.close(); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	workers.Wait()
	before, err := os.ReadFile(filepath.Join(paths.Output, observerMarker))
	if err != nil {
		t.Fatal(err)
	}
	paths.Config.data += "changed"
	if out, err := openObserverOutput(t.Context(), paths); err == nil {
		_ = out.close()
		t.Fatal("changed inputs accepted")
	}
	after, err := os.ReadFile(filepath.Join(paths.Output, observerMarker))
	if err != nil || string(before) != string(after) {
		t.Fatal("changed binding overwritten", err)
	}
}

func TestObserverOutputWaitsForLockBeforeInspection(t *testing.T) {
	t.Parallel()
	dir, paths := observerOutputFixture(t)
	parent, err := os.OpenRoot(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	root, err := parent.OpenRoot(filepath.Base(dir))
	if err != nil {
		t.Fatal(err)
	}
	holder := &observerOutput{root: root, parent: parent, name: filepath.Base(dir)}
	if err := holder.openLock(paths); err != nil {
		t.Fatal(err)
	}
	if err := waitObserverLock(t.Context(), holder.lock); err != nil {
		t.Fatal(err)
	}
	temp := observerMarker + ".tmp-active"
	if err := root.WriteFile(temp, []byte("in flight"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if out, err := openObserverOutput(ctx, paths); !errors.Is(err, context.DeadlineExceeded) {
		if out != nil {
			_ = out.close()
		}
		t.Fatalf("did not wait on active writer: %v", err)
	}
	if _, err := root.Stat(observerMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("waiter wrote marker", err)
	}
	if err := root.Remove(temp); err != nil {
		t.Fatal(err)
	}
	if err := holder.close(); err != nil {
		t.Fatal(err)
	}
	out, err := openObserverOutput(t.Context(), paths)
	if err != nil {
		t.Fatal("lock-only initialization did not resume", err)
	}
	if err := out.close(); err != nil {
		t.Fatal(err)
	}
}

func TestObserverOutputRefusesIdentitySwapsAndPartialState(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"parent", "root", "lock", "link", "partial"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir, paths := observerOutputFixture(t)
			out, err := openObserverOutput(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := out.close(); err != nil {
					t.Error(err)
				}
			}()
			before, err := out.root.ReadFile(observerMarker)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "parent":
				parent := filepath.Dir(dir)
				outside := t.TempDir()
				err = os.Rename(parent, parent+"-saved")
				if err == nil {
					err = os.Symlink(outside, parent)
				}
			case "root":
				err = os.Rename(dir, dir+"-saved")
				if err == nil {
					err = os.Mkdir(dir, 0o700)
				}
			case "lock", "link":
				err = out.root.Rename(observerLock, observerMemory)
				if err == nil && mode == "lock" {
					err = out.root.WriteFile(observerLock, nil, 0o600)
				} else if err == nil {
					err = out.root.Symlink(observerMemory, observerLock)
				}
			case "partial":
				err = out.root.Rename(observerMarker, "saved-marker")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := out.initialize(paths); err == nil {
				t.Fatal("unsafe initialization accepted")
			}
			name := observerMarker
			if mode == "partial" {
				name = "saved-marker"
			}
			after, err := out.root.ReadFile(name)
			if err != nil || string(after) != string(before) {
				t.Fatal("existing state changed", err)
			}
		})
	}
}
