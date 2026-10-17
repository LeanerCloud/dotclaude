// Package herdr wraps the herdr CLI. Every JSON response must be an {"id","result"} envelope;
// an error envelope or a missing/null result is an error, never a zero value.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Runner executes a command and returns its stdout. It is injected so tests never spawn herdr.
type Runner func(ctx context.Context, name string, args ...string) (stdout []byte, err error)

// ExecRunner runs the command with exec.CommandContext. A non-zero exit still returns stdout, because
// herdr reports errors as JSON envelopes on stdout.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	var out, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), ctx.Err())
		}
		return out.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

type Client struct {
	Binary  string
	Timeout time.Duration
	Run     Runner
}

// New returns a client for the herdr binary with a per-call timeout.
func New(binary string, timeout time.Duration) *Client {
	return &Client{Binary: binary, Timeout: timeout, Run: ExecRunner}
}

type AgentSession struct {
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Value  string `json:"value"`
}

// Agent mirrors one entry of `herdr agent list`.
type Agent struct {
	Name             string       `json:"name"`
	PaneID           string       `json:"pane_id"`
	TabID            string       `json:"tab_id"`
	WorkspaceID      string       `json:"workspace_id"`
	Cwd              string       `json:"cwd"`
	Agent            string       `json:"agent"`
	AgentStatus      string       `json:"agent_status"`
	AgentSession     AgentSession `json:"agent_session"`
	StateChangeSeq   int64        `json:"state_change_seq"`
	Revision         int64        `json:"revision"`
	Focused          bool         `json:"focused"`
	InteractiveReady bool         `json:"interactive_ready"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// APIError is the error envelope herdr returns, e.g. agent_not_found.
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("herdr error %s: %s", e.Code, e.Message) }

type envelope struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *errorBody      `json:"error"`
}

// decodeResult validates the envelope and returns the raw result.
func decodeResult(out []byte) (json.RawMessage, error) {
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("herdr response is not a JSON envelope: %w", err)
	}
	if env.Error != nil {
		return nil, &APIError{Code: env.Error.Code, Message: env.Error.Message}
	}
	if env.ID == "" {
		return nil, errors.New("herdr response has no id")
	}
	if len(env.Result) == 0 || string(env.Result) == "null" {
		return nil, fmt.Errorf("herdr response %q has no result", env.ID)
	}
	return env.Result, nil
}

func (c *Client) call(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	out, err := c.Run(ctx, c.Binary, args...)
	if err != nil {
		// An error envelope on stdout is more precise than the exit status.
		if _, envErr := decodeResult(out); envErr != nil {
			var apiErr *APIError
			if errors.As(envErr, &apiErr) {
				return nil, apiErr
			}
		}
		return nil, err
	}
	return out, nil
}

// ListAgents runs `herdr agent list`.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	out, err := c.call(ctx, "agent", "list")
	if err != nil {
		return nil, err
	}
	raw, err := decodeResult(out)
	if err != nil {
		return nil, err
	}
	var res struct {
		Agents *[]Agent `json:"agents"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode agent list: %w", err)
	}
	if res.Agents == nil {
		return nil, errors.New("agent list result has no agents field")
	}
	return *res.Agents, nil
}

// ReadAgent returns terminal text. source is one of visible, recent, recent-unwrapped, detection.
// On success herdr prints raw text, not an envelope; on failure it prints an error envelope, which
// is returned as *APIError.
func (c *Client) ReadAgent(ctx context.Context, name, source string, lines int) (string, error) {
	args := []string{"agent", "read", name}
	if source != "" {
		args = append(args, "--source", source)
	}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	out, err := c.call(ctx, args...)
	if err != nil {
		return "", err
	}
	var env envelope
	if t := bytes.TrimSpace(out); bytes.HasPrefix(t, []byte(`{"error"`)) && json.Unmarshal(t, &env) == nil && env.Error != nil {
		return "", &APIError{Code: env.Error.Code, Message: env.Error.Message}
	}
	return string(out), nil
}
