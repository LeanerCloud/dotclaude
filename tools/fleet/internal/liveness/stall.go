package liveness

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// errorLine ports resume_tick.py ERROR_LINE, which requires a leading output glyph. Besides the
// Claude glyphs (⏺ ⎿) it accepts the bullets Codex, Kimi and opencode print (● ■), or an explicit
// "error:" label at line start for renderings without a glyph. Plain prose that merely mentions an
// error ("Network error handling is now retried", "- socket hang up regression fixed") has neither,
// so it never matches. The message alternatives are unchanged, plus Codex's "stream disconnected".
var errorLine = regexp.MustCompile(`(?i)^\s*(?:[⏺⎿●■]\s+|error:\s*)(?:API Error|Connection error|Request timed out|Network error|fetch failed|` +
	`socket hang up|.*\bECONNRESET\b|.*\boverloaded_error\b|Stream idle timeout|stream disconnected)\b`)

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
