// Package limits detects harness usage throttles from pane snapshots, and tracks per-harness
// availability in limits.json.
package limits

import (
	"regexp"
	"strings"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
)

// BottomLines is how many trailing lines of the detection snapshot are searched. Limit text that
// scrolled further up is history, not the current state of the pane.
const BottomLines = 30

// Input is one pane snapshot for one agent on one tick.
type Input struct {
	Harness config.Harness
	// Status is herdr's agent_status.
	Status string
	// Detection is the text of `herdr agent read --source detection`.
	Detection string
	// PrevSignature is the Signature of the match seen on the previous tick, "" if none.
	PrevSignature string
	Now           time.Time
}

// Result describes what Detect found. Only Confirmed may drive a state change, and only for a
// harness whose detector is Verified.
type Result struct {
	Harness config.Harness
	// Verified is false for detectors built from binary strings or docs without a captured screen.
	Verified bool
	Matched  bool
	// Chrome reports that the harness's prompt chrome is on screen. Unverified detectors know no
	// chrome, so it stays false for them.
	Chrome bool
	// Confirmed: status gate, bottom-line match, chrome and the previous-tick match all hold.
	Confirmed bool
	// Candidate: an unverified detector would have confirmed. Log the screen as a fixture
	// candidate; never act on it.
	Candidate bool
	Signature string
	ResetAt   time.Time
	ResetText string
	Reason    string
}

type detector struct {
	verified bool
	// find returns the matching line, if any, among the bottom lines.
	find   func(lines []string) (string, bool)
	chrome func(lines []string) bool
}

var detectors = map[config.Harness]detector{
	config.Claude:   {verified: true, find: claudeFind, chrome: claudeChrome},
	config.Kimi:     {find: patternFinder(kimiPatterns)},
	config.OpenCode: {find: patternFinder(opencodePatterns)},
}

func statusAllowed(s string) bool { return s == "blocked" || s == "idle" || s == "done" }

// Detect applies the throttle rule for the input's harness. Codex has no screen detector: its limits
// come from ReadCodexRateLimits.
func Detect(in Input) Result {
	d, ok := detectors[in.Harness]
	r := Result{Harness: in.Harness, Verified: ok && d.verified}
	if !ok {
		r.Reason = "no screen detector for " + string(in.Harness)
		return r
	}
	lines := bottom(in.Detection, BottomLines)
	line, found := d.find(lines)
	if !found {
		r.Reason = "no limit text in the bottom lines"
		return r
	}
	r.Matched = true
	r.Signature = signature(in.Harness, line)
	r.ResetText, r.ResetAt = parseReset(line, in.Now)
	if d.chrome != nil {
		r.Chrome = d.chrome(lines)
	}
	switch {
	case !statusAllowed(in.Status):
		r.Reason = "agent status " + in.Status + " is not blocked, idle or done"
	case d.verified && !r.Chrome:
		r.Reason = "prompt chrome absent"
	case in.PrevSignature != r.Signature:
		r.Reason = "same match not seen on the previous tick"
	case d.verified:
		r.Confirmed = true
	default:
		r.Candidate = true
		r.Reason = "detector unverified"
	}
	return r
}

func bottom(text string, n int) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

var spaceRE = regexp.MustCompile(`\s+`)

func signature(h config.Harness, line string) string {
	return string(h) + ":" + strings.ToLower(spaceRE.ReplaceAllString(strings.TrimSpace(line), " "))
}

func patternFinder(res []*regexp.Regexp) func([]string) (string, bool) {
	return func(lines []string) (string, bool) {
		for i := len(lines) - 1; i >= 0; i-- {
			for _, re := range res {
				if re.MatchString(lines[i]) {
					return lines[i], true
				}
			}
		}
		return "", false
	}
}

// Kimi patterns come from the provider error classifier strings inside kimi-code 2.2.0
// (dist/main.mjs: PROVIDER_RATE_LIMIT_MESSAGE_PATTERNS, KIMI_QUOTA_EXHAUSTED_MESSAGE_PATTERNS and
// the goal pause reason). No screen capture exists, so the detector stays unverified.
var kimiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)paused after provider rate limit`),
	regexp.MustCompile(`(?i)\b429\b.*too many requests`),
	regexp.MustCompile(`(?i)provider\.rate_limit`),
	regexp.MustCompile(`(?i)reached .*max rpm`),
	regexp.MustCompile(`(?i)exceeded your current (token )?quota`),
	regexp.MustCompile(`(?i)insufficient balance|check your account balance`),
}

// opencode patterns come from identifiers and messages inside the opencode binary (FreeUsageLimitError,
// RateLimitError, "Check your account billing and usage limits"). Unverified for the same reason.
var opencodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)FreeUsageLimitError`),
	regexp.MustCompile(`\bRateLimitError\b`),
	regexp.MustCompile(`(?i)check your account billing and usage limits`),
}
