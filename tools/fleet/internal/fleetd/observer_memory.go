package fleetd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/limits"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/liveness"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

func (out *observerOutput) loadMemory() (ObserverMemory, error) {
	if err := out.checkIdentity(); err != nil {
		return ObserverMemory{}, err
	}
	if _, err := out.root.Lstat(observerMemory); errors.Is(err, os.ErrNotExist) {
		return ObserverMemory{}, nil
	} else if err != nil {
		return ObserverMemory{}, err
	}
	data, err := readObserverArtifact(out.root, observerMemory)
	if err != nil {
		return ObserverMemory{}, err
	}
	var wire *struct {
		Liveness *struct {
			liveness.Memory
			Slots map[string]*liveness.SlotMemory `json:"slots"`
		}
		Signatures *map[string]*string
		Limits     *limits.Set
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ObserverMemory{}, fmt.Errorf("decode observer memory: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ObserverMemory{}, fmt.Errorf("trailing observer memory data")
	}
	if wire == nil || wire.Liveness == nil || wire.Signatures == nil || wire.Limits == nil {
		return ObserverMemory{}, fmt.Errorf("observer memory requires nonnull Liveness, Signatures and Limits")
	}
	mem := ObserverMemory{Liveness: wire.Liveness.Memory, Signatures: map[string]string{}, Limits: *wire.Limits}
	for id, signature := range *wire.Signatures {
		if signature == nil {
			return ObserverMemory{}, fmt.Errorf("null observer signature value")
		}
		mem.Signatures[id] = *signature
	}
	if wire.Liveness.Slots != nil {
		mem.Liveness.Slots = make(map[string]liveness.SlotMemory, len(wire.Liveness.Slots))
	}
	for id, slot := range wire.Liveness.Slots {
		if slot == nil {
			return ObserverMemory{}, fmt.Errorf("null observer slot value")
		}
		mem.Liveness.Slots[id] = *slot
	}
	return prepareObserverMemory(mem)
}

func observerDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func prepareObserverMemory(mem ObserverMemory) (ObserverMemory, error) {
	if mem.Signatures == nil {
		return ObserverMemory{}, fmt.Errorf("observer memory requires nonnull Signatures")
	}
	if err := mem.Limits.Validate(); err != nil {
		return ObserverMemory{}, err
	}
	if mem.Liveness.ListKey != "" && !observerDigest(mem.Liveness.ListKey) {
		return ObserverMemory{}, fmt.Errorf("invalid observer liveness digest")
	}
	for id, slot := range mem.Liveness.Slots {
		if !observerDigest(id) {
			return ObserverMemory{}, fmt.Errorf("invalid observer slot identity")
		}
		if slot.Acted || slot.ActedSeq != 0 || len(slot.Sent) != 0 || slot.Blocked || slot.BlockedSeq != 0 || slot.HangReported {
			return ObserverMemory{}, fmt.Errorf("observer memory contains dispatch marks")
		}
	}
	next := mem
	next.Signatures = maps.Clone(mem.Signatures)
	for id, signature := range mem.Signatures {
		if !observerDigest(id) {
			return ObserverMemory{}, fmt.Errorf("invalid observer signature identity")
		}
		next.Signatures[id] = state.Scrub(signature)
	}
	next.Limits.Harnesses = maps.Clone(mem.Limits.Harnesses)
	for harness, entry := range mem.Limits.Harnesses {
		entry.Reason = state.Scrub(entry.Reason)
		next.Limits.Harnesses[harness] = entry
	}
	return next, nil
}

func encodeObserverMemory(mem ObserverMemory) ([]byte, error) {
	clean, err := prepareObserverMemory(mem)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	if len(data)+1 > maxObserverMemoryBytes {
		return nil, fmt.Errorf("observer memory exceeds %d bytes", maxObserverMemoryBytes)
	}
	return append(data, '\n'), nil
}
