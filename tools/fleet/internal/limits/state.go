package limits

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

// Kind is a harness's availability.
type Kind string

const (
	OK        Kind = "ok"
	Throttled Kind = "throttled-until"
	Unknown   Kind = "unknown"
	Exhausted Kind = "exhausted"
)

func (k Kind) valid() bool { return k == OK || k == Throttled || k == Unknown || k == Exhausted }

// UnparsedWait is how long a throttle lasts when the screen shows no parsable reset time. The
// throttle then expires to unknown and is re-probed.
const UnparsedWait = time.Hour

// Entry is one harness's row of limits.json.
type Entry struct {
	Kind Kind `json:"kind"`
	// VerifiedAt is when ok was last established.
	VerifiedAt time.Time `json:"verified_at,omitzero"`
	// Until is when a throttle ends; Since is when it began (used by migrate_after).
	Until  time.Time `json:"until,omitzero"`
	Since  time.Time `json:"since,omitzero"`
	Reason string    `json:"reason,omitempty"`
}

// Set is limits.json. A harness without a row is unknown.
type Set struct {
	Harnesses map[config.Harness]Entry `json:"harnesses"`
}

// Get returns the harness's entry, unknown when none was recorded.
func (s *Set) Get(h config.Harness) Entry {
	if e, ok := s.Harnesses[h]; ok {
		return e
	}
	return Entry{Kind: Unknown}
}

// Put records e for h.
func (s *Set) Put(h config.Harness, e Entry) {
	if s.Harnesses == nil {
		s.Harnesses = map[config.Harness]Entry{}
	}
	s.Harnesses[h] = e
}

// Observe folds one tick's detection for one agent into the harness's state:
//   - a confirmed throttle sets throttled-until (the parsed reset, else now+UnparsedWait), keeping
//     the original Since while the throttle persists;
//   - a working agent with no match means the harness is usable: unknown (the first-run bootstrap)
//     or throttled become ok. Exhausted only clears through an explicit probe via Put.
func (s *Set) Observe(r Result, status string, now time.Time) {
	cur := s.Get(r.Harness)
	switch {
	case r.Confirmed:
		until := r.ResetAt
		if until.IsZero() || !until.After(now) {
			until = now.Add(UnparsedWait)
		}
		since := now
		if cur.Kind == Throttled && !cur.Since.IsZero() {
			since = cur.Since
		}
		s.Put(r.Harness, Entry{Kind: Throttled, Until: until, Since: since, Reason: r.Signature})
	case status == "working" && !r.Matched && (cur.Kind == Unknown || cur.Kind == Throttled):
		s.Put(r.Harness, Entry{Kind: OK, VerifiedAt: now})
	}
}

// ExpireThrottles turns every throttle whose reset time has passed into unknown and returns those
// harnesses. After a sleep or crash that is all that is known.
func (s *Set) ExpireThrottles(now time.Time) []config.Harness {
	var out []config.Harness
	for h, e := range s.Harnesses {
		if e.Kind == Throttled && !e.Until.After(now) {
			s.Put(h, Entry{Kind: Unknown, Reason: "throttle expired"})
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// MigrateDue reports whether h has been throttled for at least migrateAfter (config migrate_after).
func (s *Set) MigrateDue(h config.Harness, now time.Time, migrateAfter time.Duration) bool {
	e := s.Get(h)
	return e.Kind == Throttled && !e.Since.IsZero() && now.Sub(e.Since) >= migrateAfter
}

func (s *Set) validate() error {
	for h, e := range s.Harnesses {
		if !h.Valid() {
			return fmt.Errorf("unknown harness %q", h)
		}
		if !e.Kind.valid() {
			return fmt.Errorf("harness %s: unknown kind %q", h, e.Kind)
		}
		if e.Kind == Throttled && e.Until.IsZero() {
			return fmt.Errorf("harness %s: throttled-until needs until", h)
		}
	}
	return nil
}

// Load reads limits.json. A missing file is a first run and yields an empty set; anything else
// wrong with it is an error.
func Load(path string) (*Set, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Set{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read limits: %w", err)
	}
	var s Set
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", path, err)
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// Save writes limits.json atomically.
func (s *Set) Save(path string) error {
	if err := s.validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(path, append(data, '\n'), 0o600)
}
