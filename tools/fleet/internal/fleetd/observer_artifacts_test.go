package fleetd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func observerArtifactFixture(t *testing.T) (string, ObserverPaths, []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	inputs, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := ObserverPaths{Config: InputSnapshot{path: filepath.Join(inputs, "config.json"), data: "original config"}, Ledger: InputSnapshot{path: filepath.Join(inputs, "slots.json"), data: "original ledger"}, Output: dir}
	marker, err := json.Marshal(map[string]any{"version": 1, "config_path": paths.Config.Path(), "config_hash": fmt.Sprintf("%x", sha256.Sum256([]byte("original config"))), "ledger_path": paths.Ledger.Path(), "ledger_hash": fmt.Sprintf("%x", sha256.Sum256([]byte("original ledger")))})
	if err != nil {
		t.Fatal(err)
	}
	return dir, paths, marker
}

func TestObserverArtifactValidationIsReadOnly(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"initialized", "full", "empty", "lock-only", "marker-only", "memory-only", "missing-marker", "missing-lock", "config-path", "ledger-path", "config-hash", "ledger-hash", "unknown", "too-many", "root-mode", "file-mode", "symlink", "inside-link", "hardlink", "alias", "fifo", "directory", "lock-data", "unknown-key", "trailing", "malformed", "missing-key", "version", "oversize", "closed-root"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir, paths, marker := observerArtifactFixture(t)
			files := map[string][]byte{"observer.json": marker, "observer.lock": nil}
			switch mode {
			case "full":
				files["memory.json"] = []byte("memory parsing belongs to caller")
				files["journal.jsonl"] = []byte("journal parsing belongs to caller")
			case "empty":
				files = map[string][]byte{}
			case "lock-only":
				files = map[string][]byte{"observer.lock": nil}
			case "marker-only":
				delete(files, "observer.lock")
			case "memory-only":
				files = map[string][]byte{"memory.json": []byte("retain")}
			case "missing-marker":
				delete(files, "observer.json")
				files["memory.json"] = []byte("retain")
			case "missing-lock":
				delete(files, "observer.lock")
				files["journal.jsonl"] = []byte("retain")
			case "config-path":
				paths.Config.path += "-changed"
			case "ledger-path":
				paths.Ledger.path += "-changed"
			case "config-hash":
				paths.Config.data += "changed"
			case "ledger-hash":
				paths.Ledger.data += "changed"
			case "unknown":
				files["other"] = []byte("retain")
			case "too-many":
				for _, name := range []string{"memory.json", "journal.jsonl", "other"} {
					files[name] = []byte("retain")
				}
			case "lock-data":
				files = map[string][]byte{"observer.lock": []byte("valuable prior content")}
			case "trailing":
				files["observer.json"] = append(marker, []byte(" {}")...)
			case "malformed":
				files["observer.json"] = []byte("invalid")
			case "oversize":
				files["observer.json"] = append(marker, []byte(strings.Repeat(" ", maxObserverMarkerBytes+1-len(marker)))...)
			case "unknown-key", "missing-key", "version":
				var obj map[string]any
				if err := json.Unmarshal(marker, &obj); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "unknown-key":
					obj["unexpected"] = true
				case "missing-key":
					delete(obj, "ledger_hash")
				case "version":
					obj["version"] = 2
				}
				data, err := json.Marshal(obj)
				if err != nil {
					t.Fatal(err)
				}
				files["observer.json"] = data
			}
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("retain outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "root-mode":
				err = os.Chmod(dir, 0o750)
			case "file-mode":
				err = os.WriteFile(filepath.Join(dir, "memory.json"), []byte("retain"), 0o644)
			case "symlink":
				err = os.Symlink(outside, filepath.Join(dir, "memory.json"))
			case "inside-link":
				err = os.Symlink("observer.lock", filepath.Join(dir, "memory.json"))
			case "hardlink":
				err = os.Link(outside, filepath.Join(dir, "memory.json"))
			case "alias":
				err = os.Link(filepath.Join(dir, "observer.lock"), filepath.Join(dir, "memory.json"))
			case "fifo":
				err = syscall.Mkfifo(filepath.Join(dir, "memory.json"), 0o600)
			case "directory":
				err = os.Mkdir(filepath.Join(dir, "memory.json"), 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			capture := func() map[string]string {
				t.Helper()
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				result := map[string]string{}
				for _, entry := range entries {
					info, err := os.Lstat(filepath.Join(dir, entry.Name()))
					if err != nil {
						t.Fatal(err)
					}
					value := fmt.Sprintf("%v:%d:%d", info.Mode(), info.Size(), info.ModTime().UnixNano())
					if info.Mode().IsRegular() {
						data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
						if err != nil {
							t.Fatal(err)
						}
						value += fmt.Sprintf(":%x", sha256.Sum256(data))
					}
					result[entry.Name()] = value
				}
				return result
			}
			before := capture()
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			})
			if mode == "closed-root" {
				if err := root.Close(); err != nil {
					t.Fatal(err)
				}
			}
			initialized, err := inspectObserverOutput(root, paths)
			valid := mode == "initialized" || mode == "full" || mode == "empty" || mode == "lock-only"
			if (err == nil) != valid {
				t.Fatalf("valid=%v initialized=%v err=%v", valid, initialized, err)
			}
			if err == nil && initialized != (mode == "initialized" || mode == "full") {
				t.Fatalf("wrong initialization state: %v", initialized)
			}
			if mode == "oversize" && !strings.Contains(err.Error(), "bytes") {
				t.Fatalf("marker bound bypassed: %v", err)
			}
			if mode == "fifo" && !strings.Contains(err.Error(), "regular") {
				t.Fatalf("FIFO regular-file guard bypassed: %v", err)
			}
			if !reflect.DeepEqual(before, capture()) {
				t.Fatal("read-only validation changed artifacts")
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != "retain outside" {
				t.Fatal("outside contents changed", err)
			}
		})
	}
}

func TestObserverArtifactsUseRetainedRoot(t *testing.T) {
	t.Parallel()
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			t.Parallel()
			dir, paths, marker := observerArtifactFixture(t)
			for name, data := range map[string][]byte{"observer.json": marker, "observer.lock": nil} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if !valid {
				if err := os.WriteFile(filepath.Join(dir, "unexpected"), []byte("retain"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := os.Rename(dir, dir+"-retained"); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if err := os.Symlink(outside, dir); err != nil {
				t.Fatal(err)
			}
			initialized, err := inspectObserverOutput(root, paths)
			if (err == nil) != valid || initialized != valid {
				t.Fatalf("reopened replaced directory: %v %v", initialized, err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("replacement changed", err)
			}
		})
	}
}
