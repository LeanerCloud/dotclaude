package limits

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxRolloutFiles bounds how many recent rollout files are searched for a rate_limits event; a
// session that has not produced a token_count yet has none.
const maxRolloutFiles = 5

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
	return &CodexWindow{UsedPercent: *w.UsedPercent, WindowMinutes: w.WindowMinutes, ResetsAt: time.Unix(w.ResetsAt, 0).UTC()}
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

func newestRollouts(dir string, n int) ([]string, error) {
	type entry struct {
		path string
		mod  time.Time
	}
	var all []entry
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.HasSuffix(p, ".jsonl") {
			fi, err := d.Info()
			if err != nil {
				return err
			}
			all = append(all, entry{p, fi.ModTime()})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan codex sessions: %w", err)
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

func lastRateLimits(path string) (*CodexLimits, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open rollout: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only
	var last *CodexLimits
	r := bufio.NewReader(f)
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

// Entry converts the limits into an availability entry. A window at 100% throttles the harness until
// the latest such reset. With no windows and no credits (the subscription is not metering and there
// is nothing to spend) the harness is exhausted. Anything else is ok as of now.
func (c *CodexLimits) Entry(now time.Time) Entry {
	var until time.Time
	for _, w := range []*CodexWindow{c.Primary, c.Secondary} {
		if w != nil && w.UsedPercent >= 100 && w.ResetsAt.After(until) {
			until = w.ResetsAt
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
