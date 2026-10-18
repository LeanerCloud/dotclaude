package fleetd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/lock"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/resources"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

type Observer struct {
	Run       herdr.Runner
	Resources resources.Sampler
	Home      string
}

type Inventory struct {
	Name, Harness, Status, Cwd, Identity, PaneID string
}

type SlotEvidence struct {
	Name, Identity, Harness, Status                string
	Seq, Revision                                  int64
	Focused, InteractiveReady, WorkLeft, HeavyWait bool
	Detection, Recent                              string
}

type Observation struct {
	Complete      bool
	Inventory     []Inventory
	Slots         []SlotEvidence
	Resources     resources.Snapshot
	Errors, Notes []string
}

func (o Observer) Collect(ctx context.Context, configPath string, cfg config.Config, ledger state.Ledger, now time.Time) Observation {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ob := Observation{Complete: true}
	fail := func(err error) { ob.Complete = false; ob.Errors = append(ob.Errors, state.Scrub(err.Error())) }
	if err := cfg.Validate(); err != nil {
		fail(err)
		return ob
	}
	if err := ledger.Validate(); err != nil {
		fail(err)
		return ob
	}
	path, err := canonical(configPath, o.Home)
	if err != nil {
		fail(err)
		return ob
	}
	root := filepath.Dir(path)
	sampler := o.Resources
	sampler.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return o.Resources.Run(ctx, name, args...)
	}
	ob.Resources, err = sampler.Sample(ctx, root, resources.DefaultThresholds(cfg.Thresholds))
	if err != nil {
		fail(err)
	}
	client := herdr.Client{Binary: "herdr", Timeout: 5 * time.Second, Run: o.Run}
	agents, err := client.ListAgents(ctx)
	if err != nil {
		fail(err)
		return ob
	}
	byName := map[string]herdr.Agent{}
	for _, a := range agents {
		ob.Inventory = append(ob.Inventory, Inventory{state.Scrub(a.Name), state.Scrub(a.Agent), state.Scrub(a.AgentStatus), state.Scrub(a.Cwd), agentIdentity(a), state.Scrub(a.PaneID)})
		if !config.Harness(a.Agent).Valid() {
			ob.Notes = append(ob.Notes, state.Scrub(a.Name+": unknown harness; inventory only"))
		}
		if a.Name == "" {
			ob.Notes = append(ob.Notes, "unnamed agent; inventory only")
			continue
		}
		if _, duplicate := byName[a.Name]; duplicate {
			fail(fmt.Errorf("duplicate agent name %q", a.Name))
			return ob
		}
		byName[a.Name] = a
	}
	for _, slot := range ledger.Slots {
		known := false
		for _, repo := range cfg.Repos {
			if repo.Name == slot.Repo {
				known = true
			}
		}
		if !known {
			fail(fmt.Errorf("slot %s has unknown repo", slot.Name))
			continue
		}
		a, found := byName[slot.Name]
		cwd, cwdErr := canonical(slot.Cwd, o.Home)
		agentCwd, agentErr := canonical(a.Cwd, o.Home)
		if !found || slot.Status != state.Active || slot.Pane == "" || slot.SessionID == "" || slot.Pane != a.PaneID || slot.SessionID != a.AgentSession.Value || a.AgentSession.Kind != "id" || a.AgentSession.Agent != string(slot.Harness) || string(slot.Harness) != a.Agent || cwdErr != nil || agentErr != nil || cwd != agentCwd {
			ob.Notes = append(ob.Notes, state.Scrub(slot.Name+": missing, inactive or mismatched identity; inventory only"))
			continue
		}
		e := SlotEvidence{Name: state.Scrub(slot.Name), Identity: agentIdentity(a), Harness: string(slot.Harness), Status: state.Scrub(a.AgentStatus), Seq: a.StateChangeSeq, Revision: a.Revision, Focused: a.Focused, InteractiveReady: a.InteractiveReady}
		for _, read := range []struct {
			source string
			dest   *string
		}{{"detection", &e.Detection}, {"recent-unwrapped", &e.Recent}} {
			text, err := client.ReadAgent(ctx, slot.Name, read.source, 120)
			if err != nil {
				fail(err)
			} else {
				*read.dest = state.Scrub(text)
			}
		}
		h, err := readHandoff(root, slot.Name)
		if err != nil {
			fail(err)
		} else {
			e.WorkLeft = !h.Idle()
		}
		e.HeavyWait = lock.HeavyWaiting(filepath.Join(o.Home, ".claude", "fleet", "state", "heavy-wait", slot.Name), now, lock.DefaultTimeout)
		ob.Notes = append(ob.Notes, state.Scrub(slot.Name+": repo activity unavailable; hang inference suppressed"))
		ob.Slots = append(ob.Slots, e)
	}
	return ob
}

func agentIdentity(a herdr.Agent) string {
	identity, _ := json.Marshal([]string{a.Name, a.PaneID, a.TabID, a.WorkspaceID, a.Agent, a.Cwd, a.AgentSession.Agent, a.AgentSession.Kind, a.AgentSession.Source, a.AgentSession.Value})
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:])
}

func canonical(path, home string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	abs, err := filepath.Abs(config.ExpandHome(path, home))
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func readHandoff(parent, name string) (handoff *state.Handoff, err error) {
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, parentRoot.Close()) }()
	info, err := parentRoot.Lstat("handoff")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("handoff root must be a directory, not a symlink")
	}
	root, err := parentRoot.OpenRoot("handoff")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	f, err := root.OpenFile(name+".md", os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("handoff must be a regular file")
	}
	// Handoffs are short WIP contracts; cap input before parsing untrusted text.
	const maxHandoffBytes = 64 << 10
	data, err := io.ReadAll(io.LimitReader(f, maxHandoffBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxHandoffBytes {
		return nil, fmt.Errorf("handoff exceeds 64 KiB")
	}
	h, err := state.ParseHandoff(state.Scrub(string(data)))
	if err != nil {
		return nil, err
	}
	if h.Slot != name {
		return nil, fmt.Errorf("handoff slot mismatch")
	}
	return h, nil
}
