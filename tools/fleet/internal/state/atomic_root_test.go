package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAtomicRootFailureAndSyncOrder(t *testing.T) {
	for _, mode := range []string{"success", "file-sync", "close", "rename", "dir-sync", "closed-root"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if mode == "rename" {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "retained")
			}
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "state.json.tmp-retained"), []byte("unrelated"), 0o600); err != nil {
				t.Fatal(err)
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
			originalFile, originalDir := syncFile, syncDir
			t.Cleanup(func() { syncFile, syncDir = originalFile, originalDir })
			var events []string
			var temporary *os.File
			read := func() string {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			syncFile = func(f *os.File) error {
				temporary = f
				events = append(events, "file:"+read())
				info, err := f.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o640 {
					t.Fatal("requested permissions not applied before sync")
				}
				data, err := os.ReadFile(f.Name())
				if err != nil || string(data) != "new" {
					t.Fatalf("incomplete temp: %q %v", data, err)
				}
				if mode == "file-sync" {
					return os.ErrInvalid
				}
				if mode == "close" {
					return f.Close()
				}
				return originalFile(f)
			}
			syncDir = func(d *os.File) error {
				events = append(events, "dir:"+read())
				if _, err := temporary.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("temporary not closed before publication")
				}
				if mode == "dir-sync" {
					return os.ErrInvalid
				}
				return originalDir(d)
			}
			if mode == "closed-root" {
				if err := root.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = WriteFileAtomicRoot(root, "state.json", []byte("new"), 0o640)
			if (err == nil) != (mode == "success") {
				t.Fatalf("unexpected result: %v", err)
			}
			want := "old"
			if mode == "success" || mode == "dir-sync" {
				want = "new"
			}
			if read() != want {
				t.Fatalf("contents after %s: %q", mode, read())
			}
			if mode == "success" || mode == "dir-sync" {
				if strings.Join(events, ",") != "file:old,dir:new" {
					t.Fatal(events)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("cleanup changed unrelated files or retained fresh temp: %v", entries)
			}
			retained, err := os.ReadFile(filepath.Join(dir, "state.json.tmp-retained"))
			if err != nil || string(retained) != "unrelated" {
				t.Fatal("unrelated temp changed", err)
			}
		})
	}
}

func TestAtomicRootRetainsDirectoryAcrossReplacement(t *testing.T) {
	for _, when := range []string{"before-temp", "before-rename"} {
		t.Run(when, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "output")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "state.json"), []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
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
			originalFile, originalDir := syncFile, syncDir
			t.Cleanup(func() { syncFile, syncDir = originalFile, originalDir })
			moved := dir + "-retained"
			replace := func() error {
				if err := os.Rename(dir, moved); err != nil {
					return err
				}
				return os.Symlink(outside, dir)
			}
			if when == "before-temp" {
				if err := replace(); err != nil {
					t.Fatal(err)
				}
			}
			syncFile = func(f *os.File) error {
				if err := originalFile(f); err != nil {
					return err
				}
				if when == "before-rename" {
					return replace()
				}
				return nil
			}
			synced := false
			syncDir = func(d *os.File) error {
				got, err := d.Stat()
				if err != nil {
					return err
				}
				want, err := os.Stat(moved)
				if err != nil {
					return err
				}
				if !os.SameFile(got, want) {
					t.Fatal("directory sync followed replaced pathname")
				}
				synced = true
				return originalDir(d)
			}
			if err := WriteFileAtomicRoot(root, "state.json", []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			if !synced {
				t.Fatal("directory not synced")
			}
			for path, want := range map[string]string{filepath.Join(moved, "state.json"): "new", filepath.Join(outside, "state.json"): "outside"} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Fatalf("wrong root contents: %q %v", data, err)
				}
			}
			for _, path := range []string{moved, outside} {
				entries, err := os.ReadDir(path)
				if err != nil || len(entries) != 1 {
					t.Fatalf("misplaced temp: %v %v", entries, err)
				}
			}
		})
	}
}

func TestAtomicRootReplacesLinksAndSpecialTargetsWithoutFollowing(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			dir, outside := t.TempDir(), filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "state.json")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(outside, target)
			case "hardlink":
				err = os.Link(outside, target)
			case "fifo":
				err = syscall.Mkfifo(target, 0o600)
			}
			if err != nil {
				t.Fatal(err)
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
			if err := WriteFileAtomicRoot(root, "state.json", []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(target)
			if err != nil || !info.Mode().IsRegular() {
				t.Fatal("target not replaced by regular file", err)
			}
			for path, want := range map[string]string{target: "new", outside: "outside"} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Fatalf("target followed: %q %v", data, err)
				}
			}
		})
	}
}

func TestAtomicRootBasenameBoundary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, name := range []string{"", ".", "..", "../outside", "sub/file", filepath.Join(t.TempDir(), "outside")} {
		if err := WriteFileAtomicRoot(root, name, []byte("bad"), 0o600); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid name created files: %v %v", entries, err)
	}
}

func TestWriteSyncedFileReportsWriteAndChmodFailures(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSyncedFile(f, []byte("new"), 0o600); err == nil || !strings.HasPrefix(err.Error(), "write ") {
		t.Fatalf("write failure hidden: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeSyncedFile(f, []byte("new"), 0o600); err == nil || !strings.HasPrefix(err.Error(), "chmod ") {
		t.Fatalf("chmod failure hidden: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatal("failed write mutated old contents", err)
	}
}
