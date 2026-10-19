package limits

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxRolloutFiles bounds how many recent rollout files are searched for a rate_limits event; a
// session that has not produced a token_count yet has none.
const maxRolloutFiles = 5

// rolloutTailBytes is how much of a rollout's end is read; the newest rate_limits event is near it.
const rolloutTailBytes = 64 << 10

// CodexMaxAge is how old a rate_limits reading may be and still describe the present.
const CodexMaxAge = 30 * time.Minute

// CodexMaxResetAhead bounds how far ahead a plausible resets_at lies; the longest window is weekly.
// A later reset is corrupt and is treated as an unknown one, so it cannot throttle for years.
const CodexMaxResetAhead = 8 * 24 * time.Hour

// maxResetUnix is year 9999, the last second time.Unix converts without overflow into nonsense.
const maxResetUnix = 253402300799

// ErrNoRateLimits means no recent rollout file contained a rate_limits event.
var ErrNoRateLimits = errors.New("no rate_limits event in the newest codex rollout files")

// CodexWindow is one rate-limit window of the rollout's rate_limits event.
type CodexWindow struct {
	UsedPercent   float64
	WindowMinutes int
	ResetsAt      time.Time
}

// CodexLimits is the newest rate_limits event of the newest rollout.
type CodexLimits struct {
	Primary, Secondary *CodexWindow
	HasCredits         bool
	Unlimited          bool
	Balance            string
	ObservedAt         time.Time
	File               string
}

type rawWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes int      `json:"window_minutes"`
	ResetsAt      int64    `json:"resets_at"`
}

type rawEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Payload   struct {
		RateLimits *struct {
			Primary   *rawWindow `json:"primary"`
			Secondary *rawWindow `json:"secondary"`
			Credits   *struct {
				HasCredits bool   `json:"has_credits"`
				Unlimited  bool   `json:"unlimited"`
				Balance    string `json:"balance"`
			} `json:"credits"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

func (w *rawWindow) window() *CodexWindow {
	if w == nil || w.UsedPercent == nil {
		return nil
	}
	cw := &CodexWindow{UsedPercent: *w.UsedPercent, WindowMinutes: w.WindowMinutes}
	if w.ResetsAt > 0 && w.ResetsAt <= maxResetUnix { // absent (0) or corrupt means unknown, not 1970
		cw.ResetsAt = time.Unix(w.ResetsAt, 0).UTC()
	}
	return cw
}

// ReadCodexRateLimits reads the newest rate_limits event from the newest rollout JSONL under
// sessionsDir (normally ~/.codex/sessions). A window with a null used_percent is reported as nil.
func ReadCodexRateLimits(sessionsDir string) (*CodexLimits, error) {
	files, err := newestRollouts(sessionsDir, maxRolloutFiles)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		lim, err := lastRateLimits(f)
		if err != nil {
			return nil, err
		}
		if lim != nil {
			return lim, nil
		}
	}
	return nil, ErrNoRateLimits
}

// newestRollouts returns up to n rollout files, newest first, from the newest day directory only.
// Codex lays sessions out as YYYY/MM/DD/rollout-*.jsonl, so descending name order is newest first
// and a tick lists a handful of directories instead of walking the whole history. A directory with
// no rollouts (an empty day) falls through to the previous one.
func newestRollouts(dir string, n int) ([]string, error) {
	files, err := newestDirFiles(dir)
	if err != nil {
		return nil, fmt.Errorf("scan codex sessions: %w", err)
	}
	type entry struct {
		path string
		mod  time.Time
	}
	all := make([]entry, 0, len(files))
	for _, p := range files {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("scan codex sessions: %w", err)
		}
		all = append(all, entry{p, fi.ModTime()})
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].mod.Equal(all[j].mod) {
			return all[i].mod.After(all[j].mod)
		}
		return all[i].path > all[j].path
	})
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].path)
	}
	return out, nil
}

// newestDirFiles returns the .jsonl files directly in dir plus those of its newest non-empty
// subtree.
func newestDirFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir) // sorted by name
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range ents {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	for i := len(ents) - 1; i >= 0; i-- {
		if !ents[i].IsDir() {
			continue
		}
		sub, err := newestDirFiles(filepath.Join(dir, ents[i].Name()))
		if err != nil {
			return nil, err
		}
		if len(sub) > 0 {
			return append(files, sub...), nil
		}
	}
	return files, nil
}

func lastRateLimits(path string) (*CodexLimits, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open rollout: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat rollout: %w", err)
	}
	r := bufio.NewReader(f)
	if fi.Size() > rolloutTailBytes {
		if _, err := f.Seek(fi.Size()-rolloutTailBytes, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek rollout %s: %w", path, err)
		}
		r.Reset(f)
		if _, err := r.ReadBytes('\n'); err != nil { // drop the line the cut landed in
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			return nil, fmt.Errorf("read rollout %s: %w", path, err)
		}
	}
	var last *CodexLimits
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"rate_limits"`)) {
			var ev rawEvent
			if json.Unmarshal(line, &ev) == nil && ev.Payload.RateLimits != nil {
				rl := ev.Payload.RateLimits
				lim := &CodexLimits{Primary: rl.Primary.window(), Secondary: rl.Secondary.window(), ObservedAt: ev.Timestamp, File: path}
				if rl.Credits != nil {
					lim.HasCredits, lim.Unlimited, lim.Balance = rl.Credits.HasCredits, rl.Credits.Unlimited, rl.Credits.Balance
				}
				last = lim
			}
		}
		if errors.Is(err, io.EOF) {
			return last, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read rollout %s: %w", path, err)
		}
	}
}

// Entry converts the limits into an availability entry. A reading older than CodexMaxAge (or with
// no timestamp, or one in the future) says nothing about now, so it is unknown. Otherwise a window at
// 100% throttles the harness until the latest such reset still ahead of now; a window whose reset has
// passed no longer counts. A full window with no (or an implausibly distant) reset time throttles for UnparsedWait. With no windows and no credits (the subscription is not metering and there is nothing to
// spend) the harness is exhausted. Anything else is ok as of now.
func (c *CodexLimits) Entry(now time.Time) Entry {
	if c.ObservedAt.IsZero() || c.ObservedAt.After(now) || now.Sub(c.ObservedAt) > CodexMaxAge {
		return Entry{Kind: Unknown, Reason: "codex rate_limits reading is stale"}
	}
	var until time.Time
	for _, w := range []*CodexWindow{c.Primary, c.Secondary} {
		if w == nil || w.UsedPercent < 100 {
			continue
		}
		reset := w.ResetsAt
		if reset.IsZero() || reset.After(now.Add(CodexMaxResetAhead)) {
			reset = now.Add(UnparsedWait)
		}
		if reset.After(now) && reset.After(until) {
			until = reset
		}
	}
	switch {
	case !until.IsZero():
		return Entry{Kind: Throttled, Until: until, Since: now, Reason: "codex window at 100%"}
	case c.Primary == nil && c.Secondary == nil && !c.HasCredits && !c.Unlimited:
		return Entry{Kind: Exhausted, Since: now, Reason: "codex reports no windows and no credits"}
	}
	return Entry{Kind: OK, VerifiedAt: now}
}
