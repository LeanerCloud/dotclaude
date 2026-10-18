package fleetd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

const (
	observerMarker         = "observer.json"
	observerLock           = "observer.lock"
	observerMemory         = "memory.json"
	observerJournal        = "journal.jsonl"
	maxObserverMarkerBytes = 16 << 10
	maxObserverMemoryBytes = 4 << 20
)

type observerBinding struct {
	Version        int    `json:"version"`
	ConfigPathHash string `json:"config_path_hash"`
	ConfigHash     string `json:"config_hash"`
	LedgerPathHash string `json:"ledger_path_hash"`
	LedgerHash     string `json:"ledger_hash"`
}

func expectedObserverBinding(paths ObserverPaths) observerBinding {
	return observerBinding{
		Version:        1,
		ConfigPathHash: fmt.Sprintf("%x", sha256.Sum256([]byte(paths.Config.Path()))),
		ConfigHash:     fmt.Sprintf("%x", paths.Config.Hash()),
		LedgerPathHash: fmt.Sprintf("%x", sha256.Sum256([]byte(paths.Ledger.Path()))),
		LedgerHash:     fmt.Sprintf("%x", paths.Ledger.Hash()),
	}
}

func inspectObserverOutput(root *os.Root, paths ObserverPaths) (initialized bool, err error) {
	dir, err := root.Open(".")
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	info, err := dir.Stat()
	if err != nil {
		return false, err
	}
	if info.Mode() != os.ModeDir|0o700 {
		return false, fmt.Errorf("observer root must be a private 0700 directory")
	}
	entries, err := dir.ReadDir(5)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	if len(entries) > 4 {
		return false, fmt.Errorf("unexpected observer artifacts")
	}
	seen := map[string]bool{}
	var marker []byte
	for _, entry := range entries {
		name := entry.Name()
		switch name {
		case observerMarker, observerLock, observerMemory, observerJournal:
		default:
			return false, fmt.Errorf("unexpected observer artifact %q", name)
		}
		seen[name] = true
		data, err := readObserverArtifact(root, name)
		if err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		if name == observerMarker {
			marker = data
		}
	}
	if !seen[observerMarker] {
		if len(entries) == 0 || len(entries) == 1 && seen[observerLock] {
			return false, nil
		}
		return false, fmt.Errorf("partial observer output without binding marker")
	}
	if !seen[observerLock] {
		return false, fmt.Errorf("partial observer output without lock")
	}
	var binding observerBinding
	decoder := json.NewDecoder(bytes.NewReader(marker))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return false, fmt.Errorf("decode observer binding: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("trailing observer binding data")
	}
	if binding != expectedObserverBinding(paths) {
		return false, fmt.Errorf("observer input binding changed")
	}
	return true, nil
}

func observerFileInfo(f *os.File) (os.FileInfo, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode() != 0o600 || stat.Nlink != 1 {
		return nil, fmt.Errorf("observer artifact must be a private 0600 regular single-link file")
	}
	return info, nil
}

func openObserverArtifact(root *os.Root, name string, flags int) (file *os.File, info os.FileInfo, err error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("observer artifact must be a regular file without links")
	}
	f, err := root.OpenFile(name, flags|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Close())
		}
	}()
	info, err = observerFileInfo(f)
	if err != nil {
		return nil, nil, err
	}
	after, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, info) || !os.SameFile(info, after) {
		return nil, nil, fmt.Errorf("observer artifact changed while opening")
	}
	return f, info, nil
}

func readObserverArtifact(root *os.Root, name string) (data []byte, err error) {
	f, info, err := openObserverArtifact(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if name == observerLock && info.Size() != 0 {
		return nil, fmt.Errorf("observer lock must be empty")
	}
	bound := maxObserverMarkerBytes
	if name == observerMemory {
		bound = maxObserverMemoryBytes
	} else if name != observerMarker {
		return nil, nil
	}
	if info.Size() > int64(bound) {
		return nil, fmt.Errorf("observer artifact exceeds %d bytes", bound)
	}
	data, err = io.ReadAll(io.LimitReader(f, int64(bound)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > bound {
		return nil, fmt.Errorf("observer artifact exceeds %d bytes", bound)
	}
	return data, nil
}
