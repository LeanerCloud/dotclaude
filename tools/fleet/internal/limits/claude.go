package limits

import (
	"regexp"
	"strings"
)

// The strings below are the guard of ~/.claude/scripts/herdr-wait-for-limit-reset.py, ported
// exactly: the usage-limit prompt, its second option, and the cursor sitting on the first option.
const (
	claudePrompt        = "What do you want to do?"
	claudeWaitOption    = "2. Wait here, then continue automatically"
	claudeCursorOnFirst = "❯ 1. Stop and wait for limit to reset"
)

// ClaudeWaitKeys moves the cursor from option 1 to option 2 and confirms it. They are only valid
// after ClaudeWaitAnswerSafe accepted the same screen, and they never reach option 3 (usage credits).
// Each call returns a fresh slice, so a caller cannot change the sequence for the next one.
func ClaudeWaitKeys() []string { return []string{"down", "enter"} }

// ClaudeWaitAnswerSafe reports whether screen shows Claude's usage-limit prompt with the cursor on
// option 1, and returns the key sequence that selects "Wait here, then continue automatically".
// Any other screen (an approval dialog, the cursor elsewhere, a transcript) returns false and nil.
// Callers also require agent_status blocked and re-read the screen just before sending.
func ClaudeWaitAnswerSafe(screen string) (bool, []string) {
	if !strings.Contains(screen, claudePrompt) || !strings.Contains(screen, claudeWaitOption) ||
		!strings.Contains(screen, claudeCursorOnFirst) {
		return false, nil
	}
	return true, ClaudeWaitKeys()
}

// claudeLimitRE matches the limit notice line, anchored after an optional tool-result glyph, so a
// sentence that merely mentions limits does not match. The wordings are the notice variants Claude
// Code prints; the fixtures reconstruct them from the guard strings above.
var claudeLimitRE = regexp.MustCompile(`(?i)^[^\pL\pN]*(?:you['’]ve hit your (?:\w+ )?limit|claude (?:ai )?usage limit reached|(?:5-hour|weekly|session|opus|sonnet) limit reached)\b`)

func claudeFind(lines []string) (string, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if claudeLimitRE.MatchString(lines[i]) {
			return lines[i], true
		}
	}
	return "", false
}

// claudeChrome is the limit menu itself: the prompt plus its first option.
func claudeChrome(lines []string) bool {
	text := strings.Join(lines, "\n")
	return strings.Contains(text, claudePrompt) && strings.Contains(text, "Stop and wait for limit to reset")
}
