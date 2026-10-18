package fleetd

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestObserverMarkerHidesInputPathsAndBindsTheirExactNames(t *testing.T) {
	t.Parallel()
	_, paths := observerOutputFixture(t)
	token := "gh" + "p_" + strings.Repeat("a", 24)
	paths.Config.path = filepath.Join(filepath.Dir(paths.Config.Path()), token+"-config.json")
	paths.Ledger.path = filepath.Join(filepath.Dir(paths.Ledger.Path()), token+"-ledger.json")
	out, err := openObserverOutput(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := out.root.ReadFile(observerMarker)
	if err != nil {
		t.Fatal(err)
	}
	if err := out.close(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{token, paths.Config.Path(), paths.Ledger.Path()} {
		if strings.Contains(string(marker), raw) {
			t.Fatal("marker persisted raw input path text")
		}
	}
	var binding map[string]any
	if err := json.Unmarshal(marker, &binding); err != nil {
		t.Fatal(err)
	}
	for key, path := range map[string]string{"config_path_hash": paths.Config.Path(), "ledger_path_hash": paths.Ledger.Path()} {
		if binding[key] != fmt.Sprintf("%x", sha256.Sum256([]byte(path))) {
			t.Fatalf("%s does not bind the canonical path", key)
		}
	}
	out, err = openObserverOutput(t.Context(), paths)
	if err != nil {
		t.Fatal("same inputs refused", err)
	}
	if err := out.close(); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"config", "ledger"} {
		changed := paths
		if input == "config" {
			changed.Config.path += "-renamed"
		} else {
			changed.Ledger.path += "-renamed"
		}
		if out, err := openObserverOutput(t.Context(), changed); err == nil {
			_ = out.close()
			t.Fatalf("changed %s path with identical content accepted", input)
		}
	}
	legacy := map[string]any{"version": 1, "config_path": paths.Config.Path(), "config_hash": fmt.Sprintf("%x", paths.Config.Hash()), "ledger_path": paths.Ledger.Path(), "ledger_hash": fmt.Sprintf("%x", paths.Ledger.Hash())}
	old, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(paths.Output, observerMarker)
	if err := os.WriteFile(markerPath, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := openObserverOutput(t.Context(), paths); err == nil {
		_ = out.close()
		t.Fatal("legacy raw-path marker accepted")
	}
	after, err := os.ReadFile(markerPath)
	if err != nil || string(after) != string(old) {
		t.Fatal("legacy marker changed on refusal", err)
	}
}

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
