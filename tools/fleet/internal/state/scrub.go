package state

import (
	"math"
	"regexp"
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
}

var scrubRules = []rule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)},
	{"github-token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`)},
	{"sk-key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"aws-key-id", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)},
}

var (
	tokenRE = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)
	uuidRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	hexRE   = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// Scrub replaces secrets in s with [REDACTED:<kind>]. Pure hex strings (git SHAs, sha256 digests) and
// UUIDs are never redacted; the entropy rule requires upper, lower and a digit in a 32+ character token
// with entropy at or above EntropyThreshold. Hex-encoded secrets therefore pass: an accepted trade for
// not mangling every commit hash in the journal.
func Scrub(s string) string {
	for _, r := range scrubRules {
		s = r.re.ReplaceAllString(s, "[REDACTED:"+r.name+"]")
	}
	return tokenRE.ReplaceAllStringFunc(s, func(tok string) string {
		if looksSecret(tok) {
			return "[REDACTED:high-entropy]"
		}
		return tok
	})
}

func looksSecret(tok string) bool {
	if uuidRE.MatchString(tok) || hexRE.MatchString(tok) {
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
