package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/fleetd"
)

func tickFixtureCommand() bool {
	if os.Getenv("FLEET_TEST_TICK_COMMANDS") != "1" {
		return false
	}
	command := filepath.Base(os.Args[0]) + " " + strings.Join(os.Args[1:], " ")
	var response string
	switch command {
	case "herdr agent list":
		if os.Getenv("FLEET_TEST_TICK_MODE") == "wait" {
			if err := os.WriteFile(os.Getenv("FLEET_TEST_TICK_READY"), []byte("waiting"), 0o600); err != nil {
				os.Exit(8)
			}
			time.Sleep(time.Hour)
		}
		if os.Getenv("FLEET_TEST_TICK_MODE") == "error" {
			fmt.Fprintln(os.Stderr, "sk-abcdefghijklmnopqrstuvwxyz123456")
			os.Exit(9)
		}
		response = `{"id":"fixture","result":{"agents":[]}}`
	case "sysctl -n vm.loadavg":
		response = "{ 0 0 0 }"
	case "memory_pressure -Q":
		response = "free percentage: 50%"
	case "sysctl -n vm.swapusage":
		response = "total = 0M used = 0M"
	default:
		if name := filepath.Base(os.Args[0]); name != "herdr" && name != "sysctl" && name != "memory_pressure" {
			return false
		}
		fmt.Fprintln(os.Stderr, "unexpected fixture command", command)
		os.Exit(10)
	}
	if _, err := fmt.Fprintln(os.Stdout, response); err != nil {
		os.Exit(8)
	}
	return true
}

func tickCLIPaths(t *testing.T) (env, []string, string) {
	t.Helper()
	e, _, _ := testEnv(t, 1)
	configPath := filepath.Join(e.home, ".claude/fleet/fleet.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repos[0].Checkout = t.TempDir()
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ledgerPath, output := filepath.Join(e.home, "slots.json"), filepath.Join(t.TempDir(), "observer")
	if err := os.WriteFile(ledgerPath, []byte(`{"slots":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, name := range []string{"herdr", "sysctl", "memory_pressure"} {
		if err := os.Symlink(self, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	e.environ = append(e.environ, "PATH="+bin, "HOME="+e.home, "FLEET_TEST_AS_FLEET=1", "FLEET_TEST_TICK_COMMANDS=1")
	return e, []string{"tick", "--dry-run", "--config", configPath, "--ledger", ledgerPath, "--output-dir", output}, output
}

func tickCLICommand(t *testing.T, e env, args []string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = e.environ
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	return cmd, &stdout, &stderr
}

func TestTickUsageRefusesBeforeSideEffects(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"tick"}, {"tick", "--dry-run"},
		{"tick", "--config", "c", "--ledger", "l", "--output-dir", "o"},
		{"tick", "--dry-run=false", "--config", "c", "--ledger", "l", "--output-dir", "o"},
		{"tick", "--dry-run", "--config", "c", "--output-dir", "o"},
		{"tick", "--dry-run", "--config", "c", "--ledger", "l"},
		{"tick", "--dry-run", "--ledger", "l", "--output-dir", "o"},
		{"tick", "--dry-run", "--config", "c", "--ledger", "l", "--output-dir", "o", "extra"},
		{"tick", "--dry-run", "--config", "c", "--ledger", "l", "--output-dir", "o", "--yes"},
	} {
		home := t.TempDir()
		var stdout, stderr bytes.Buffer
		if code := dispatch(env{home: home, stdout: &stdout, stderr: &stderr}, args); code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage:") {
			t.Fatal("invalid flags accepted", args, code, stdout.String(), stderr.String())
		}
		if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
			t.Fatal("usage failure created state", err)
		}
	}
}

func TestTickCLIUsesRealDefaultsWithFixtureCommands(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"complete", "error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			e, args, output := tickCLIPaths(t)
			e.environ = append(e.environ, "FLEET_TEST_TICK_MODE="+mode)
			before := map[string][sha256.Size]byte{}
			for _, path := range []string{args[3], args[5]} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = sha256.Sum256(data)
			}
			cmd, stdout, stderr := tickCLICommand(t, e, args)
			err := cmd.Run()
			if (err == nil) != (mode == "complete") {
				t.Fatal("incorrect CLI status", err, stderr.String())
			}
			var result fleetd.Evaluation
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Complete != (mode == "complete") || len(result.Proposals) != 0 || result.Resources.Cores <= 0 {
				t.Fatal("invalid structured result", stdout.String(), err)
			}
			if mode == "error" && (len(result.Errors) == 0 || stderr.Len() == 0) {
				t.Fatal("incomplete status has no error evidence")
			}
			journal, err := os.ReadFile(filepath.Join(output, "journal.jsonl"))
			if err != nil || !json.Valid(bytes.TrimSpace(journal)) || bytes.Contains(journal, []byte("sk-abcdefghijklmnopqrstuvwxyz123456")) || strings.Contains(stdout.String()+stderr.String(), "sk-abcdefghijklmnopqrstuvwxyz123456") {
				t.Fatal("missing or unsafe journal/diagnostics", err)
			}
			for path, hash := range before {
				data, err := os.ReadFile(path)
				if err != nil || sha256.Sum256(data) != hash {
					t.Fatal("CLI modified input", err)
				}
			}
		})
	}
}

func TestTickCLIPathDiagnosticsAreScrubbed(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	secret := "gh" + "p_" + strings.Repeat("x", 24)
	e := env{home: t.TempDir(), stdout: &stdout, stderr: &stderr}
	code := dispatch(e, []string{"tick", "--dry-run", "--config", filepath.Join(e.home, secret), "--ledger", "missing", "--output-dir", "missing"})
	var result fleetd.Evaluation
	if code != exitFailure || json.Unmarshal(stdout.Bytes(), &result) != nil || result.Complete || len(result.Errors) == 0 || len(result.Proposals) != 0 || strings.Contains(stdout.String()+stderr.String(), secret) {
		t.Fatal("unsafe path diagnostic", code, stdout.String(), stderr.String())
	}
}

func TestTickCLIOutputWriteFailureReturnsNonzero(t *testing.T) {
	t.Parallel()
	e, args, output := tickCLIPaths(t)
	cmd, _, stderr := tickCLICommand(t, e, args)
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	cmd.Stdout = dir
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "encode result") {
		t.Fatal("output failure hidden", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(output, "memory.json")); err != nil {
		t.Fatal("fixture did not reach successful persistence before output failure", err)
	}
}

func TestTickCLISignalCancelsRead(t *testing.T) {
	t.Parallel()
	e, args, output := tickCLIPaths(t)
	ready := filepath.Join(t.TempDir(), "ready")
	e.environ = append(e.environ, "FLEET_TEST_TICK_MODE=wait", "FLEET_TEST_TICK_READY="+ready)
	cmd, stdout, stderr := tickCLICommand(t, e, args)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("fixture read did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var exit *exec.ExitError
	var result fleetd.Evaluation
	if !errors.As(err, &exit) || exit.ExitCode() != exitFailure || json.Unmarshal(stdout.Bytes(), &result) != nil || result.Complete || len(result.Proposals) != 0 || len(result.Errors) == 0 {
		t.Fatal("signal did not produce explicit incomplete result", err, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(output, "journal.jsonl")); !os.IsNotExist(err) {
		t.Fatal("canceled CLI persisted success", err)
	}
}
