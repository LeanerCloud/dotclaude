// Package state holds fleetd's durable data types: the slot ledger, handoff files, inbox commands
// and the secret scrubber.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
)

type Status string

const (
	AdoptedUnverified Status = "adopted-unverified"
	Active            Status = "active"
	Paused            Status = "paused"
	Draining          Status = "draining"
	Quiescing         Status = "quiescing"
	Retired           Status = "retired"
)

var statuses = []Status{AdoptedUnverified, Active, Paused, Draining, Quiescing, Retired}

func (s Status) Valid() bool {
	for _, v := range statuses {
		if s == v {
			return true
		}
	}
	return false
}

// slotNameRE: stable, harness-free slot names such as cc-impl or go-pr-merge-2.
var slotNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Slot is one row of slots.json.
type Slot struct {
	Name         string         `json:"name"`
	Repo         string         `json:"repo"`
	Role         config.Role    `json:"role"`
	Harness      config.Harness `json:"harness"`
	SessionID    string         `json:"session_id"`
	Pane         string         `json:"pane"`
	Cwd          string         `json:"cwd"`
	HandoffPath  string         `json:"handoff_path"`
	Status       Status         `json:"status"`
	LastSeq      int64          `json:"last_seq"`
	LastRevision int64          `json:"last_revision"`
	LastProgress time.Time      `json:"last_progress"`
	Nudges       int            `json:"nudges"`
	Migrations   int            `json:"migrations"`
}

type Ledger struct {
	Slots []Slot `json:"slots"`
}

func (l *Ledger) Validate() error {
	seen := map[string]bool{}
	for i, s := range l.Slots {
		if !slotNameRE.MatchString(s.Name) {
			return fmt.Errorf("slots[%d]: invalid name %q", i, s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("slots[%d]: duplicate name %q", i, s.Name)
		}
		seen[s.Name] = true
		if !s.Status.Valid() {
			return fmt.Errorf("slots[%s]: unknown status %q", s.Name, s.Status)
		}
		if s.Repo == "" {
			return fmt.Errorf("slots[%s]: repo is required", s.Name)
		}
	}
	return nil
}

// ReadLedger loads and validates slots.json. A missing file is an error; the caller decides whether
// a first run may start from an empty ledger.
func ReadLedger(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ledger: %w", err)
	}
	var l Ledger
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return nil, fmt.Errorf("decode ledger %s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode ledger %s: trailing data", path)
	}
	if err := l.Validate(); err != nil {
		return nil, fmt.Errorf("ledger %s: %w", path, err)
	}
	return &l, nil
}

// WriteLedger validates and atomically replaces slots.json.
func WriteLedger(path string, l *Ledger) error {
	if err := l.Validate(); err != nil {
		return fmt.Errorf("refusing to write invalid ledger: %w", err)
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ledger: %w", err)
	}
	return WriteFileAtomic(path, append(data, '\n'), 0o600)
}
