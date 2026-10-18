package fleetd

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

const maxObserverInputBytes = 4 << 20

type InputSnapshot struct {
	path, data string
}

func (s InputSnapshot) Path() string            { return s.path }
func (s InputSnapshot) Hash() [sha256.Size]byte { return sha256.Sum256([]byte(s.data)) }
func (s InputSnapshot) Bytes() []byte           { return []byte(s.data) }

type ObserverPaths struct {
	Config, Ledger InputSnapshot
	Output         string
}

func ReadObserverPaths(configPath, ledgerPath, outputPath, home string) (*ObserverPaths, error) {
	c, ci, err := readObserverInput(configPath, home)
	if err != nil {
		return nil, err
	}
	l, li, err := readObserverInput(ledgerPath, home)
	if err != nil {
		return nil, err
	}
	if os.SameFile(ci, li) {
		return nil, fmt.Errorf("config and ledger alias the same file")
	}
	cfg, err := config.Parse(c.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Path(), err)
	}
	if err := cfg.ValidateCheckouts(home); err != nil {
		return nil, err
	}
	if _, err := state.ParseLedger(l.Bytes()); err != nil {
		return nil, fmt.Errorf("%s: %w", l.Path(), err)
	}
	out, err := observerOutputPath(outputPath, home)
	if err != nil {
		return nil, err
	}
	protected := []string{c.Path(), l.Path(), filepath.Join(filepath.Dir(c.Path()), "handoff")}
	for _, repo := range cfg.Repos {
		protected = append(protected, config.ExpandHome(repo.Checkout, home))
	}
	live := filepath.Join(home, ".claude", "fleet")
	for _, name := range []string{"fleet.json", "state", "inbox", "events", "handoff"} {
		protected = append(protected, filepath.Join(live, name))
	}
	for _, path := range protected {
		p, err := observerNamespace(path, home)
		if err != nil {
			return nil, err
		}
		if pathContains(out, p) || pathContains(p, out) {
			return nil, fmt.Errorf("observer output overlaps protected path %s", p)
		}
	}
	return &ObserverPaths{Config: c, Ledger: l, Output: out}, nil
}

func readObserverInput(path, home string) (snapshot InputSnapshot, info os.FileInfo, err error) {
	if path == "" {
		return snapshot, nil, fmt.Errorf("empty observer input path")
	}
	abs, err := filepath.Abs(config.ExpandHome(path, home))
	if err != nil {
		return snapshot, nil, err
	}
	parent, err := canonical(filepath.Dir(abs), home)
	if err != nil {
		return snapshot, nil, err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return snapshot, nil, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	// Root resolves relative symlinks even with O_NOFOLLOW.
	name := filepath.Base(abs)
	before, err := root.Lstat(name)
	if err != nil {
		return snapshot, nil, err
	}
	if !before.Mode().IsRegular() {
		return snapshot, nil, fmt.Errorf("observer input must be a regular file without symlinks")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return snapshot, nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err = f.Stat()
	if err != nil {
		return snapshot, nil, err
	}
	after, err := root.Lstat(name)
	if err != nil {
		return snapshot, nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, info) || !os.SameFile(info, after) {
		return snapshot, nil, fmt.Errorf("observer input changed while opening")
	}
	if !info.Mode().IsRegular() || info.Size() > maxObserverInputBytes {
		return snapshot, nil, fmt.Errorf("observer input must be a regular file of at most %d bytes", maxObserverInputBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxObserverInputBytes+1))
	if err != nil {
		return snapshot, nil, err
	}
	if len(data) > maxObserverInputBytes {
		return snapshot, nil, fmt.Errorf("observer input exceeds %d bytes", maxObserverInputBytes)
	}
	return InputSnapshot{path: filepath.Join(parent, name), data: string(data)}, info, nil
}

func observerOutputPath(path, home string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty observer output path")
	}
	abs, err := filepath.Abs(config.ExpandHome(path, home))
	if err != nil {
		return "", err
	}
	parent, err := canonical(filepath.Dir(abs), home)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("output parent must be an existing directory")
	}
	out := filepath.Join(parent, filepath.Base(abs))
	info, err = os.Lstat(out)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("observer output must be a directory, not a link or special file")
	}
	return out, nil
}

func observerNamespace(path, home string) (string, error) {
	p, err := canonical(path, home)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	abs, err := filepath.Abs(config.ExpandHome(path, home))
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("unsafe protected path %s", abs)
	}
	parent, err := observerNamespace(filepath.Dir(abs), home)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(strings.ToLower(parent), strings.ToLower(child))
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	info, err := os.Stat(parent)
	if err != nil {
		return false
	}
	for {
		if other, err := os.Stat(child); err == nil && os.SameFile(info, other) {
			return true
		}
		next := filepath.Dir(child)
		if next == child {
			return false
		}
		child = next
	}
}
