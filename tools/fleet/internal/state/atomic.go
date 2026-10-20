package state

import (
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
	fail := func(step string, err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("%s %s: %w", step, tmp, err)
	}
	if err := f.Chmod(perm); err != nil {
		return fail("chmod", err)
	}
	if _, err := f.Write(data); err != nil {
		return fail("write", err)
	}
	if err := syncFile(f); err != nil {
		return fail("fsync", err)
	}
	if err := f.Close(); err != nil {
		return fail("close", err)
	}
	return tmp, nil
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
