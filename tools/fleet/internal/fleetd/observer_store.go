package fleetd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/liveness"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

type observerRecord struct {
	Time        time.Time
	Observation Observation
	Evaluation  Evaluation
}

func encodeObserverRecord(record observerRecord) ([]byte, error) {
	ob, ev := &record.Observation, &record.Evaluation
	ob.Inventory = slices.Clone(ob.Inventory)
	for i := range ob.Inventory {
		v := &ob.Inventory[i]
		if !observerDigest(v.Identity) {
			return nil, fmt.Errorf("invalid journal inventory identity")
		}
		v.Name, v.Harness, v.Status = state.Scrub(v.Name), state.Scrub(v.Harness), state.Scrub(v.Status)
		v.Cwd, v.PaneID = state.Scrub(v.Cwd), state.Scrub(v.PaneID)
	}
	ob.Slots = slices.Clone(ob.Slots)
	for i := range ob.Slots {
		v := &ob.Slots[i]
		if !observerDigest(v.Identity) {
			return nil, fmt.Errorf("invalid journal slot identity")
		}
		v.Name, v.Harness, v.Status = state.Scrub(v.Name), state.Scrub(v.Harness), state.Scrub(v.Status)
		v.Detection, v.Recent = state.Scrub(v.Detection), state.Scrub(v.Recent)
	}
	ev.Proposals = slices.Clone(ev.Proposals)
	for i := range ev.Proposals {
		v := &ev.Proposals[i]
		if !observerDigest(v.Identity) {
			return nil, fmt.Errorf("invalid journal proposal identity")
		}
		switch v.Kind {
		case liveness.Nudge, liveness.Continue, liveness.Hang, liveness.Blocked:
		default:
			return nil, fmt.Errorf("invalid journal proposal kind")
		}
		v.Slot, v.Reason = state.Scrub(v.Slot), state.Scrub(v.Reason)
	}
	ev.Limits = maps.Clone(ev.Limits)
	for id, v := range ev.Limits {
		if !observerDigest(id) || !v.Harness.Valid() {
			return nil, fmt.Errorf("invalid journal limit identity or harness")
		}
		v.Signature, v.ResetText, v.Reason = state.Scrub(v.Signature), state.Scrub(v.ResetText), state.Scrub(v.Reason)
		ev.Limits[id] = v
	}
	for _, values := range []*[]string{&ob.Errors, &ob.Notes, &ev.Errors, &ev.Notes} {
		*values = slices.Clone(*values)
		for i := range *values {
			(*values)[i] = state.Scrub((*values)[i])
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (out *observerOutput) persist(record observerRecord, memory ObserverMemory) error {
	mem, err := encodeObserverMemory(memory)
	if err != nil {
		return err
	}
	line, err := encodeObserverRecord(record)
	if err != nil {
		return err
	}
	if out.lock == nil {
		return fmt.Errorf("observer persistence requires output lock")
	}
	if err := out.checkIdentity(); err != nil {
		return err
	}
	if err := out.appendJournal(line); err != nil {
		return fmt.Errorf("observer journal persistence uncertain: %w", err)
	}
	if err := out.checkIdentity(); err != nil {
		return fmt.Errorf("observer journal appended but memory persistence uncertain: %w", err)
	}
	if err := state.WriteFileAtomicRoot(out.root, observerMemory, mem, 0o600); err != nil {
		return fmt.Errorf("observer journal appended but memory persistence uncertain: %w", err)
	}
	return nil
}

func (out *observerOutput) appendJournal(line []byte) (err error) {
	_, err = out.root.Lstat(observerJournal)
	created := errors.Is(err, os.ErrNotExist)
	var f *os.File
	var info os.FileInfo
	if created {
		f, err = out.root.OpenFile(observerJournal, os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			info, err = observerFileInfo(f)
		}
	} else if err == nil {
		f, info, err = openObserverArtifact(out.root, observerJournal, os.O_RDWR|os.O_APPEND)
	}
	if f != nil {
		defer func() { err = errors.Join(err, f.Close()) }()
	}
	if err != nil {
		return err
	}
	named, err := out.root.Lstat(observerJournal)
	if err != nil {
		return err
	}
	if !named.Mode().IsRegular() || !os.SameFile(info, named) {
		return fmt.Errorf("observer journal changed while opening")
	}
	if info.Size() > 0 {
		var tail [1]byte
		if _, err := f.ReadAt(tail[:], info.Size()-1); err != nil {
			return err
		}
		if tail[0] != '\n' {
			return fmt.Errorf("observer journal has partial trailing record")
		}
	}
	if err := out.checkIdentity(); err != nil {
		return err
	}
	n, err := f.Write(line)
	if err != nil {
		return err
	}
	if n != len(line) {
		return io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if created {
		dir, err := out.root.Open(".")
		if err != nil {
			return err
		}
		return errors.Join(dir.Sync(), dir.Close())
	}
	return nil
}
