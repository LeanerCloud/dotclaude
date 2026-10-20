package limits

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	zoneRE  = regexp.MustCompile(`\(([A-Za-z]+(?:/[A-Za-z_+\-]+)*)\)`)
	resetRE = regexp.MustCompile(`(?i)\bresets?(?:\s+at)?\s+(.+)$`)
	clockRE = regexp.MustCompile(`(?i)(?:\b(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s*(?:at\s+)?)?\b(\d{1,2})(?::(\d{2}))?\s*([ap])\.?m\b`)
	months  = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
)

// parseReset extracts the reset time a limit line shows, e.g. "resets 3pm (Europe/Bucharest)" or
// "resets Oct 20, 9:30am (UTC)". A time of day without a date means the next such time after now.
// It returns the matched text and the zero time when nothing parses; callers must not invent one.
func parseReset(line string, now time.Time) (string, time.Time) {
	m := resetRE.FindStringSubmatch(line)
	if m == nil {
		return "", time.Time{}
	}
	text := strings.TrimSpace(m[1])
	loc := now.Location()
	if z := zoneRE.FindStringSubmatch(text); z != nil {
		l, err := time.LoadLocation(z[1])
		if err != nil {
			return text, time.Time{}
		}
		loc = l
	}
	c := clockRE.FindStringSubmatch(text)
	if c == nil {
		return text, time.Time{}
	}
	hour, _ := strconv.Atoi(c[3])
	minute := 0
	if c[4] != "" {
		minute, _ = strconv.Atoi(c[4])
	}
	if hour < 1 || hour > 12 || minute > 59 {
		return text, time.Time{}
	}
	hour %= 12
	if strings.EqualFold(c[5], "p") {
		hour += 12
	}
	local := now.In(loc)
	if c[1] == "" {
		t := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return text, t
	}
	day, _ := strconv.Atoi(c[2])
	t := time.Date(local.Year(), months[strings.ToLower(c[1])], day, hour, minute, 0, 0, loc)
	if t.Before(now.AddDate(0, 0, -1)) {
		t = t.AddDate(1, 0, 0)
	}
	return text, t
}
