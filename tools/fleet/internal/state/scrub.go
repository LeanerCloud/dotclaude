package state

import (
	"math"
	"regexp"
	"strings"
)

// EntropyThreshold is the minimum Shannon entropy (bits per character) for a long token to be
// redacted. Random base64 or alphanumeric secrets of 32+ characters score about 4.5 to 5; identifiers,
// paths and prose score about 3 to 4.
const EntropyThreshold = 4.2

// MinEntropyLen is the shortest token considered for the entropy rule.
const MinEntropyLen = 32

type rule struct {
	name string
	re   *regexp.Regexp
	repl string // regexp template; defaults to [REDACTED:<name>]
}

var scrubRules = []rule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY( BLOCK)?-----.*?(?:-----END [A-Z ]*PRIVATE KEY( BLOCK)?-----|\z)`), ""},
	{"github-token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`), ""},
	{"sk-key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`), ""},
	{"aws-key-id", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), ""},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), ""},
	{"bearer", regexp.MustCompile(`(?i)(\bAuthorization:[ \t]*Bearer[ \t]+)[^\s\[]\S*`), "${1}[REDACTED:bearer]"},
	{"bearer", regexp.MustCompile(`(?i)(\bBearer[ \t]+)[A-Za-z0-9._~+/=-]{20,}`), "${1}[REDACTED:bearer]"},
}

var (
	// tokenRE deliberately excludes "~", so a home-relative path or "~secret" splits at the tilde.
	tokenRE = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)
	// b64Run40 finds 40-character base64 runs (AWS secret access keys); redactSlashKeys checks the
	// neighbours by hand because RE2 has no lookaround.
	b64Run40 = regexp.MustCompile(`[A-Za-z0-9+/]{40}`)
	hexRE    = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// Scrub replaces secrets in s with [REDACTED:<kind>].
//
// The entropy rule splits each 32+ character token on "/", "-", "_" and "." and redacts it only if one
// single segment of at least MinEntropyLen characters has upper case, lower case, a digit and entropy at
// or above EntropyThreshold. That keeps branch names, identifiers, UUIDs and paths intact.
//
// Known gaps, accepted so commit hashes and paths survive in the journal: pure hex secrets (64-character
// hex keys, hex digests) are never redacted, and a base64 or base64url secret that is not exactly 40
// characters and is split by "/", "-" or "_" into segments shorter than MinEntropyLen escapes the
// entropy rule (40-character keys containing "+" or "/", such as AWS secret access keys, have their
// own rule; the prefix rules catch most vendor tokens).
func Scrub(s string) string {
	for _, r := range scrubRules {
		repl := r.repl
		if repl == "" {
			repl = "[REDACTED:" + r.name + "]"
		}
		s = r.re.ReplaceAllString(s, repl)
	}
	s = redactSlashKeys(s)
	return tokenRE.ReplaceAllStringFunc(s, func(tok string) string {
		for _, seg := range strings.FieldsFunc(tok, func(r rune) bool { return r == '/' || r == '-' || r == '_' || r == '.' }) {
			if len(seg) >= MinEntropyLen && looksSecret(seg) {
				return "[REDACTED:high-entropy]"
			}
		}
		return tok
	})
}

// redactSlashKeys redacts standalone 40-character base64 runs containing "+" or "/" that look random.
// The per-segment entropy rule cannot see them because "/" splits them.
func redactSlashKeys(s string) string {
	const tokenChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=_.-"
	var b strings.Builder
	last := 0
	for _, m := range b64Run40.FindAllStringIndex(s, -1) {
		start, end := m[0], m[1]
		if start > 0 && strings.IndexByte(tokenChars, s[start-1]) >= 0 {
			continue
		}
		if end < len(s) && strings.IndexByte(tokenChars, s[end]) >= 0 {
			continue
		}
		if !strings.ContainsAny(s[start:end], "+/") || !looksSecret(s[start:end]) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString("[REDACTED:high-entropy]")
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func looksSecret(tok string) bool {
	if hexRE.MatchString(tok) {
		return false
	}
	var up, low, dig bool
	for _, r := range tok {
		switch {
		case r >= 'A' && r <= 'Z':
			up = true
		case r >= 'a' && r <= 'z':
			low = true
		case r >= '0' && r <= '9':
			dig = true
		}
	}
	return up && low && dig && entropy(tok) >= EntropyThreshold
}

func entropy(s string) float64 {
	counts := map[rune]float64{}
	n := 0.0
	for _, r := range s {
		counts[r]++
		n++
	}
	var h float64
	for _, c := range counts {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}
