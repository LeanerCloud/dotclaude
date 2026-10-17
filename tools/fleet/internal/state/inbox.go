package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
)

type Cmd string

const (
	CmdNudge     Cmd = "nudge"
	CmdInterrupt Cmd = "interrupt"
	CmdMigrate   Cmd = "migrate"
	CmdRetire    Cmd = "retire"
	CmdPin       Cmd = "pin"
	CmdUnpin     Cmd = "unpin"
	CmdEscalate  Cmd = "escalate"
	CmdReport    Cmd = "report"
	CmdNote      Cmd = "note"
)

type Template string

const (
	TplContinue       Template = "continue"
	TplRereadBrief    Template = "reread-brief"
	TplRefreshHandoff Template = "refresh-handoff"
	TplStopNewClaims  Template = "stop-new-claims"
)

const (
	MaxReportBytes  = 8 * 1024
	maxCommandBytes = 64 * 1024
)

// Command is one inbox/<ts>.json request. There is deliberately no field for an escalation default:
// fleetd always writes "hold", and a "default" key is rejected as unknown.
type Command struct {
	Cmd      Cmd            `json:"cmd"`
	Slot     string         `json:"slot,omitempty"`
	Template Template       `json:"template,omitempty"`
	To       config.Harness `json:"to,omitempty"`
	Title    string         `json:"title,omitempty"`
	Question string         `json:"question,omitempty"`
	Options  []string       `json:"options,omitempty"`
	Markdown string         `json:"markdown,omitempty"`
	Text     string         `json:"text,omitempty"`
	Reason   string         `json:"reason"`
	Evidence []string       `json:"evidence"`
}

// fieldSpec lists which command-specific fields a command requires; every other command-specific
// field must be absent, so a nudge cannot smuggle free text.
var fieldSpec = map[Cmd][]string{
	CmdNudge:     {"slot", "template"},
	CmdInterrupt: {"slot", "template"},
	CmdMigrate:   {"slot", "to"},
	CmdRetire:    {"slot"},
	CmdPin:       {"slot"},
	CmdUnpin:     {"slot"},
	CmdEscalate:  {"title", "question", "options"},
	CmdReport:    {"markdown"},
	CmdNote:      {"slot", "text"},
}

func (c *Command) present() map[string]bool {
	return map[string]bool{
		"slot": c.Slot != "", "template": c.Template != "", "to": c.To != "", "title": c.Title != "",
		"question": c.Question != "", "options": len(c.Options) > 0, "markdown": c.Markdown != "", "text": c.Text != "",
	}
}

func templateValid(t Template) bool {
	switch t {
	case TplContinue, TplRereadBrief, TplRefreshHandoff, TplStopNewClaims:
		return true
	}
	return false
}

func harnessValid(h config.Harness) bool {
	switch h {
	case config.Claude, config.Codex, config.Kimi, config.OpenCode:
		return true
	}
	return false
}

// Validate checks the command shape only. Preconditions (slot state, caps) belong to fleetd.
func (c *Command) Validate() error {
	required, ok := fieldSpec[c.Cmd]
	if !ok {
		return fmt.Errorf("unknown cmd %q", c.Cmd)
	}
	have := c.present()
	allowed := map[string]bool{}
	for _, f := range required {
		allowed[f] = true
		if !have[f] {
			return fmt.Errorf("%s: missing required field %q", c.Cmd, f)
		}
	}
	for f, set := range have {
		if set && !allowed[f] {
			return fmt.Errorf("%s: field %q is not allowed", c.Cmd, f)
		}
	}
	if c.Slot != "" && !slotNameRE.MatchString(c.Slot) {
		return fmt.Errorf("%s: invalid slot name %q", c.Cmd, c.Slot)
	}
	if c.Template != "" && !templateValid(c.Template) {
		return fmt.Errorf("%s: unknown template %q", c.Cmd, c.Template)
	}
	if c.To != "" && !harnessValid(c.To) {
		return fmt.Errorf("%s: unknown harness %q", c.Cmd, c.To)
	}
	if c.Cmd == CmdReport && len(c.Markdown) > MaxReportBytes {
		return fmt.Errorf("report: markdown is %d bytes, max %d", len(c.Markdown), MaxReportBytes)
	}
	if strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf("%s: reason is required", c.Cmd)
	}
	if len(c.Evidence) == 0 {
		return fmt.Errorf("%s: evidence (paths or URLs) is required", c.Cmd)
	}
	for _, e := range c.Evidence {
		if strings.TrimSpace(e) == "" || strings.ContainsAny(e, "\n\r") {
			return fmt.Errorf("%s: invalid evidence entry %q", c.Cmd, e)
		}
	}
	return nil
}

// ParseCommand decodes strictly (unknown keys fail) and validates.
func ParseCommand(data []byte) (*Command, error) {
	if len(data) > maxCommandBytes {
		return nil, fmt.Errorf("command is %d bytes, max %d", len(data), maxCommandBytes)
	}
	var c Command
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("decode command: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode command: trailing data")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}
