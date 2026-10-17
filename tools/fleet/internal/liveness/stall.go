package liveness

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// errorLine ports resume_tick.py ERROR_LINE. The Claude glyph prefix (⏺ ⎿) became any short run of
// symbols, with an optional "error:" label, so Codex, Kimi and opencode renderings match too. The
// message alternatives are unchanged, plus Codex's "stream disconnected" line.
var errorLine = regexp.MustCompile(`(?i)^\s*[^\pL\pN\s]{0,3}\s*(?:error:\s*)?(?:API Error|Connection error|Request timed out|Network error|fetch failed|` +
	`socket hang up|.*\bECONNRESET\b|.*\boverloaded_error\b|Stream idle timeout|stream disconnected)`)

// chrome ports resume_tick.py CHROME: input boxes, status bars and blank lines that are not output.
var chrome = regexp.MustCompile(`^\s*(─|❯|⏵⏵|※|✻|\? for shortcuts|$)`)

// tailLines is how many non-chrome lines at the end of the snapshot are inspected.
const tailLines = 6

// StallSignature returns a short hash of the tail of text when one of its last non-chrome lines is a
// network or API error, and "" otherwise. text is `herdr agent read --source recent-unwrapped`.
// Equal signatures mean the same stall, so a caller sends at most one continue per signature.
func StallSignature(text string) string {
	var tail []string
	for _, line := range strings.Split(text, "\n") {
		if !chrome.MatchString(line) {
			tail = append(tail, line)
		}
	}
	if len(tail) > tailLines {
		tail = tail[len(tail)-tailLines:]
	}
	for _, line := range tail {
		if errorLine.MatchString(line) {
			sum := sha256.Sum256([]byte(strings.Join(tail, "\n")))
			return hex.EncodeToString(sum[:])[:16]
		}
	}
	return ""
}
