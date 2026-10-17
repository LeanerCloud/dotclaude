package state

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// handoffHeadings are the exact "## " headings of the handoff contract, in order.
var handoffHeadings = []string{
	"Current item", "Worktree", "Branch", "PR", "Head SHA", "Claims held", "Next action", "Blockers", "Queue",
}

var (
	titleRE     = regexp.MustCompile(`^# ([a-z0-9][a-z0-9-]{0,63}) handoff$`)
	issueClaim  = regexp.MustCompile(`^issue ([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#([0-9]+)$`)
	prClaimFile = regexp.MustCompile(`^claim-pr-(.+)-([0-9]+)\.txt$`)
)

type ClaimKind string

const (
	ClaimIssue ClaimKind = "issue"
	ClaimPR    ClaimKind = "pr"
)

// Claim is one line of "Claims held". Issue claims set Repo to "owner/repo"; PR claims set Repo to
// the bare repo name used in the bus file name.
type Claim struct {
	Kind ClaimKind
	Repo string
	N    int
}

func (c Claim) String() string {
	if c.Kind == ClaimIssue {
		return fmt.Sprintf("issue %s#%d", c.Repo, c.N)
	}
	return fmt.Sprintf("claim-pr-%s-%d.txt", c.Repo, c.N)
}

// ParseClaim accepts "issue owner/repo#n" and "claim-pr-<repo>-<n>.txt".
func ParseClaim(s string) (Claim, error) {
	if m := issueClaim.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[2])
		return Claim{ClaimIssue, m[1], n}, err
	}
	if m := prClaimFile.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[2])
		return Claim{ClaimPR, m[1], n}, err
	}
	return Claim{}, fmt.Errorf("unrecognised claim %q (want \"issue owner/repo#n\" or \"claim-pr-<repo>-<n>.txt\")", s)
}

// Handoff is the parsed WIP contract. Empty strings mean "none".
type Handoff struct {
	Slot        string
	Updated     time.Time
	CurrentItem string
	Worktree    string
	Branch      string
	PR          string
	HeadSHA     string
	Claims      []Claim
	NextAction  string
	Blockers    string
	Queue       []string
}

// Idle reports whether the slot has no item and no claims, so it may be retired or reassigned.
func (h *Handoff) Idle() bool { return h.CurrentItem == "" && len(h.Claims) == 0 }

// normalizeNewlines turns CRLF and lone CR into LF, so every line-based rule sees the same lines.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func noneToEmpty(s string) string {
	if strings.EqualFold(s, "none") {
		return ""
	}
	return s
}

// ParseHandoff requires the title, the Updated line and every heading exactly once, in order.
func ParseHandoff(text string) (*Handoff, error) {
	lines := strings.Split(normalizeNewlines(text), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return nil, errors.New("handoff is empty")
	}
	m := titleRE.FindStringSubmatch(lines[i])
	if m == nil {
		return nil, fmt.Errorf("line %d: want title \"# <slot> handoff\", got %q", i+1, lines[i])
	}
	h := &Handoff{Slot: m[1]}
	i++
	if i >= len(lines) || !strings.HasPrefix(lines[i], "Updated: ") {
		return nil, errors.New("missing \"Updated: <ISO-8601 UTC>\" line after the title")
	}
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(lines[i], "Updated: ")))
	if err != nil {
		return nil, fmt.Errorf("Updated line: %w", err)
	}
	h.Updated = ts.UTC()
	i++

	sections := map[string][]string{}
	var order []string
	cur := ""
	for ; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			cur = strings.TrimSpace(strings.TrimPrefix(lines[i], "## "))
			if _, dup := sections[cur]; dup {
				return nil, fmt.Errorf("duplicate heading %q", cur)
			}
			sections[cur] = nil
			order = append(order, cur)
			continue
		}
		if cur == "" {
			if strings.TrimSpace(lines[i]) != "" {
				return nil, fmt.Errorf("line %d: text before the first heading", i+1)
			}
			continue
		}
		sections[cur] = append(sections[cur], lines[i])
	}
	for _, want := range handoffHeadings {
		if _, ok := sections[want]; !ok {
			return nil, fmt.Errorf("missing heading %q", want)
		}
	}
	for _, got := range order {
		if !contains(handoffHeadings, got) {
			return nil, fmt.Errorf("unknown heading %q", got)
		}
	}
	if len(order) != len(handoffHeadings) || !equal(order, handoffHeadings) {
		return nil, fmt.Errorf("headings out of order: got %v, want %v", order, handoffHeadings)
	}
	body := func(name string) string { return strings.TrimSpace(strings.Join(sections[name], "\n")) }
	h.CurrentItem = noneToEmpty(body("Current item"))
	h.Worktree = noneToEmpty(body("Worktree"))
	h.Branch = noneToEmpty(body("Branch"))
	h.PR = noneToEmpty(body("PR"))
	h.HeadSHA = noneToEmpty(body("Head SHA"))
	h.NextAction = body("Next action")
	h.Blockers = noneToEmpty(body("Blockers"))
	if h.NextAction == "" {
		return nil, errors.New("Next action must not be empty")
	}
	for _, l := range bullets(sections["Claims held"]) {
		c, err := ParseClaim(l)
		if err != nil {
			return nil, fmt.Errorf("Claims held: %w", err)
		}
		h.Claims = append(h.Claims, c)
	}
	h.Queue = bullets(sections["Queue"])
	return h, nil
}

// bullets returns the text of "- " lines, skipping blanks and a lone "none".
func bullets(lines []string) []string {
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.EqualFold(l, "none") || strings.EqualFold(l, "- none") {
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(l, "- ")))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// Format renders the handoff in the exact contract format.
func (h *Handoff) Format() (string, error) {
	if !slotNameRE.MatchString(h.Slot) {
		return "", fmt.Errorf("invalid slot name %q", h.Slot)
	}
	if strings.TrimSpace(h.NextAction) == "" {
		return "", errors.New("Next action must not be empty")
	}
	for name, v := range map[string]string{"Current item": h.CurrentItem, "Worktree": h.Worktree, "Branch": h.Branch,
		"PR": h.PR, "Head SHA": h.HeadSHA, "Next action": h.NextAction, "Blockers": h.Blockers} {
		if n := normalizeNewlines(v); strings.HasPrefix(n, "## ") || strings.Contains(n, "\n## ") {
			return "", fmt.Errorf("%s contains a line starting with \"## \"", name)
		}
	}
	for _, c := range h.Claims {
		if strings.ContainsAny(c.Repo, "\n\r") {
			return "", fmt.Errorf("claim repo %q contains a line break", c.Repo)
		}
		if back, err := ParseClaim(c.String()); err != nil || back != c {
			return "", fmt.Errorf("claim %q does not survive a parse round trip", c.String())
		}
	}
	for _, q := range h.Queue {
		if strings.ContainsAny(q, "\n\r") {
			return "", fmt.Errorf("queue item %q contains a line break", q)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s handoff\nUpdated: %s\n", h.Slot, h.Updated.UTC().Format(time.RFC3339))
	sec := func(name, body string) { fmt.Fprintf(&b, "## %s\n%s\n", name, body) }
	sec("Current item", orNone(h.CurrentItem))
	sec("Worktree", orNone(h.Worktree))
	sec("Branch", orNone(h.Branch))
	sec("PR", orNone(h.PR))
	sec("Head SHA", orNone(h.HeadSHA))
	claims := "none"
	if len(h.Claims) > 0 {
		var ls []string
		for _, c := range h.Claims {
			ls = append(ls, "- "+c.String())
		}
		claims = strings.Join(ls, "\n")
	}
	sec("Claims held", claims)
	sec("Next action", h.NextAction)
	sec("Blockers", orNone(h.Blockers))
	queue := "none"
	if len(h.Queue) > 0 {
		var ls []string
		for _, q := range h.Queue {
			ls = append(ls, "- "+q)
		}
		queue = strings.Join(ls, "\n")
	}
	sec("Queue", queue)
	return b.String(), nil
}
