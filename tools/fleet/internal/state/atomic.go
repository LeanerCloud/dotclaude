package state

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// syncFile and syncDir are the durability points; tests replace them to assert order.
var (
	syncFile = func(f *os.File) error { return f.Sync() }
	syncDir  = func(d *os.File) error { return d.Sync() }
)

// writeTmp writes data to a fresh temp file in path's directory and fsyncs it. The caller renames it.
// A crash before the rename leaves path untouched.
func writeTmp(path string, data []byte, perm os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	if err := writeSyncedFile(f, data, perm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

func writeSyncedFile(f *os.File, data []byte, perm os.FileMode) error {
	if err := f.Chmod(perm); err != nil {
		return fmt.Errorf("chmod %s: %w", f.Name(), err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", f.Name(), err)
	}
	if err := syncFile(f); err != nil {
		return fmt.Errorf("fsync %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", f.Name(), err)
	}
	return nil
}

// WriteFileAtomic replaces path with data: temp file in the same directory, fsync, rename, fsync of
// the directory. Readers see the old or the new content, never a partial file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := writeTmp(path, data, perm)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open dir: %w", err)
	}
	defer func() { _ = d.Close() }() // read-only handle; the fsync result is what matters
	if err := syncDir(d); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}

func WriteFileAtomicRoot(root *os.Root, name string, data []byte, perm os.FileMode) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("atomic target must be a single basename")
	}
	tmp := name + ".tmp-" + rand.Text()
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	if err := writeSyncedFile(f, data, perm); err != nil {
		_ = f.Close()
		_ = root.Remove(tmp)
		return err
	}
	if err := root.Rename(tmp, name); err != nil {
		_ = root.Remove(tmp)
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	d, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("open dir: %w", err)
	}
	defer func() { _ = d.Close() }() // read-only handle; the fsync result is what matters
	if err := syncDir(d); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}
