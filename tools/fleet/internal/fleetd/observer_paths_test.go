package fleetd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

func TestObserverPaths(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"fresh", "existing", "hardlink", "config-link", "ledger-link", "config-relative-link", "ledger-relative-link", "alias", "fifo", "oversize", "invalid-config", "invalid-ledger", "unknown-ledger", "trailing-ledger", "output-link", "input", "ancestor", "checkout", "checkout-ancestor", "handoff", "handoff-case", "live-config", "live-state", "live-STATE", "live-inbox", "live-events", "live-handoff", "live-ancestor", "missing-parent", "output-file", "symlink-parent"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			mkdir := func(p string) {
				t.Helper()
				if err := os.MkdirAll(p, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			put := func(p string, b []byte) {
				t.Helper()
				if err := os.WriteFile(p, b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			link := func(old, new string, hard bool) {
				t.Helper()
				var err error
				if hard {
					err = os.Link(old, new)
				} else {
					err = os.Symlink(old, new)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			home, inputs, checkout := filepath.Join(dir, "home"), filepath.Join(dir, "inputs"), filepath.Join(dir, "repos", "repo")
			mkdir(home)
			mkdir(inputs)
			mkdir(checkout)
			cfg := config.Config{Repos: []config.Repo{{Name: "repo", Owner: "owner", Checkout: checkout}}, Roles: map[config.Role]config.RolePolicy{}, Thresholds: config.Thresholds{HangMinutes: 45, MigrateAfter: config.Duration(1), DiskWarnGB: 30, DiskCriticalGB: 15, HeavySlots: 1}}
			for _, role := range []config.Role{config.Coordinator, config.Impl, config.Merge, config.Reviewer} {
				cfg.Roles[role] = config.RolePolicy{Prefer: []config.Harness{config.Claude}, MinTier: config.TierLow}
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			cp, lp, out := filepath.Join(inputs, "fleet.json"), filepath.Join(inputs, "slots.json"), filepath.Join(dir, "observer")
			put(cp, data)
			put(lp, []byte(`{"slots":[]}`))
			switch mode {
			case "existing":
				mkdir(out)
			case "hardlink":
				link(cp, filepath.Join(dir, "config-alias"), true)
				cp = filepath.Join(dir, "config-alias")
			case "config-link":
				link(cp, filepath.Join(dir, "link"), false)
				cp = filepath.Join(dir, "link")
			case "ledger-link":
				link(lp, filepath.Join(dir, "link"), false)
				lp = filepath.Join(dir, "link")
			case "config-relative-link":
				link("fleet.json", filepath.Join(inputs, "config-link"), false)
				cp = filepath.Join(inputs, "config-link")
			case "ledger-relative-link":
				link("slots.json", filepath.Join(inputs, "ledger-link"), false)
				lp = filepath.Join(inputs, "ledger-link")
			case "alias":
				link(cp, filepath.Join(dir, "alias"), true)
				lp = filepath.Join(dir, "alias")
			case "fifo":
				lp = filepath.Join(inputs, "fifo")
				if err := syscall.Mkfifo(lp, 0o600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				put(lp, append([]byte(`{"slots":[]}`), bytes.Repeat([]byte(" "), maxObserverInputBytes+1-len(`{"slots":[]}`))...))
			case "invalid-config":
				put(cp, []byte(`{}`))
			case "invalid-ledger":
				put(lp, []byte(`{"slots":[{"name":"bad name"}]}`))
			case "unknown-ledger":
				put(lp, []byte(`{"unknown":true}`))
			case "trailing-ledger":
				put(lp, []byte(`{"slots":[]} {}`))
			case "output-link":
				link(checkout, out, false)
			case "input":
				out = cp
			case "ancestor":
				out = inputs
			case "checkout":
				out = filepath.Join(checkout, "observer")
			case "checkout-ancestor":
				out = filepath.Dir(checkout)
			case "handoff":
				out = filepath.Join(inputs, "handoff")
			case "handoff-case":
				out = filepath.Join(inputs, "HANDOFF")
			case "live-config":
				out = filepath.Join(home, ".claude", "fleet", "fleet.json")
			case "live-state", "live-STATE", "live-inbox", "live-events", "live-handoff":
				out = filepath.Join(home, ".claude", "fleet", strings.TrimPrefix(mode, "live-"))
			case "live-ancestor":
				out = filepath.Join(home, ".claude", "fleet")
			case "missing-parent":
				out = filepath.Join(dir, "missing", "observer")
			case "output-file":
				put(out, []byte("retain"))
			case "symlink-parent":
				link(checkout, filepath.Join(dir, "alias-parent"), false)
				out = filepath.Join(dir, "alias-parent", "observer")
			}
			if strings.HasPrefix(mode, "live-") {
				mkdir(filepath.Dir(out))
			}
			beforeConfig, err := os.ReadFile(cp)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadObserverPaths(cp, lp, out, home)
			valid := mode == "fresh" || mode == "existing" || mode == "hardlink"
			if (err == nil) != valid {
				t.Fatalf("valid=%v accepted=%v err=%v", valid, got != nil, err)
			}
			if mode == "fifo" && !strings.Contains(err.Error(), "regular file") {
				t.Fatalf("FIFO bypassed regular-file guard: %v", err)
			}
			if mode == "oversize" && !strings.Contains(err.Error(), "bytes") {
				t.Fatalf("oversize input bypassed size guard: %v", err)
			}
			if !valid {
				return
			}
			if got.Config.Hash() != sha256.Sum256(beforeConfig) || !bytes.Equal(got.Config.Bytes(), beforeConfig) {
				t.Fatal("hash does not bind exact bytes")
			}
			copyBytes := got.Config.Bytes()
			copyBytes[0] = '!'
			put(cp, []byte("changed"))
			put(lp, []byte("changed"))
			if _, err := config.Parse(got.Config.Bytes()); err != nil {
				t.Fatal("snapshot changed", err)
			}
			if _, err := state.ParseLedger(got.Ledger.Bytes()); err != nil {
				t.Fatal("ledger snapshot changed", err)
			}
			if mode != "existing" {
				if _, err := os.Lstat(out); !os.IsNotExist(err) {
					t.Fatal("output created", err)
				}
			}
		})
	}
}
