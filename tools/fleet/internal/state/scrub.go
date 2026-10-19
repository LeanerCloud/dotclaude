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
	{"basic-auth", regexp.MustCompile(`(?i)(\bAuthorization:[ \t]*Basic[ \t]+)[A-Za-z0-9+/=]{8,}`), "${1}[REDACTED:basic-auth]"},
	{"url-password", regexp.MustCompile(`(://[^/\s:@]+:)[^@\s/]+(@)`), "${1}[REDACTED:url-password]${2}"},
	{"slack-token", regexp.MustCompile(`\b(?:xox[abprs]-[A-Za-z0-9-]+|xapp-[A-Za-z0-9-]+)`), ""},
	{"npm-token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{30,}`), ""},
	{"stripe-key", regexp.MustCompile(`\bsk_(?:live|test)_[A-Za-z0-9]+`), ""},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`), ""},
}

// assignmentRule runs after the entropy rule, so values it already redacted (they start with "[") and
// the AWS secret key forms keep their more specific labels. It redacts the value of NAME=value or
// NAME: value when NAME ends in password, passwd, passphrase, secret, token or api key; prose such as
// "password reset" or "token bucket" has no "=" or ":" after the keyword and is left alone. The value
// cannot start with "=", so Go operators (":=", "==") and "!=", "<=", ">=" are not assignments.
// Prose such as "secret: see vault" is over-redacted on purpose: dropping a word costs less than a leak.
var assignmentRule = regexp.MustCompile(`(?i)([A-Za-z0-9_]*(?:password|passwd|passphrase|secret|token|api[_-]?key)["']?[ \t]*[=:][ \t]*["']?)[^\s\["'=][^\s\["']*`)

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
//
// Beyond the prefix rules, Scrub redacts Slack, npm, Stripe (sk_) and Google AIza API keys,
// "Authorization: Basic" credentials, the password in "scheme://user:password@host", and the value of
// NAME=value or NAME: value when NAME ends in password, passwd, passphrase, secret, token or api key.
//
// Remaining gaps: Slack webhook URLs, Stripe rk_live_ and whsec_ keys, Google ya29. OAuth tokens,
// Cookie: headers, "://token@host" (Sentry-style) and "://:password@host" URLs, "curl -u user:password",
// "--password value" flags, "mysql -pPASSWORD", assignments under other names (private_key=, pwd=,
// DB_PASS, client_secret_value, a bare "key="), values starting with "=", only the first word of a
// quoted value with spaces, Basic credentials without an "Authorization:" prefix, and URL passwords
// containing "/" or "@" (the part after the first such character stays visible).
func Scrub(s string) string {
	for _, r := range scrubRules {
		repl := r.repl
		if repl == "" {
			repl = "[REDACTED:" + r.name + "]"
		}
		s = r.re.ReplaceAllString(s, repl)
	}
	s = redactSlashKeys(s)
	s = tokenRE.ReplaceAllStringFunc(s, func(tok string) string {
		for _, seg := range strings.FieldsFunc(tok, func(r rune) bool { return r == '/' || r == '-' || r == '_' || r == '.' }) {
			if len(seg) >= MinEntropyLen && looksSecret(seg) {
				return "[REDACTED:high-entropy]"
			}
		}
		return tok
	})
	return assignmentRule.ReplaceAllString(s, "${1}[REDACTED:assignment]")
}

// redactSlashKeys redacts standalone 40-character base64 runs containing "+" or "/" that look random.
// The per-segment entropy rule cannot see them because "/" splits them.
func redactSlashKeys(s string) string {
	const tokenChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=_.-"
	var b strings.Builder
	last := 0
	for _, m := range b64Run40.FindAllStringIndex(s, -1) {
		start, end := m[0], m[1]
		// "=" is allowed on the left (KEY=value); quotes and ":" are not token characters anyway.
		if start > 0 && s[start-1] != '=' && strings.IndexByte(tokenChars, s[start-1]) >= 0 {
			continue
		}
		// A "." ending a sentence (followed by whitespace or the end) is not a continuation.
		sentenceEnd := end < len(s) && s[end] == '.' && (end+1 == len(s) || strings.IndexByte(" \t\r\n", s[end+1]) >= 0)
		if end < len(s) && !sentenceEnd && strings.IndexByte(tokenChars, s[end]) >= 0 {
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
