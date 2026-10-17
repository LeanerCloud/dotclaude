package herdr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func fake(out string, err error) Runner {
	return func(context.Context, string, ...string) ([]byte, error) { return []byte(out), err }
}

func client(r Runner) *Client { return &Client{Binary: "herdr", Timeout: time.Second, Run: r} }

func TestEnvelopeErrors(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, out, want string }{
		{"missing result", `{"id":"cli:agent:list"}`, "no result"},
		{"null result", `{"id":"cli:agent:list","result":null}`, "no result"},
		{"error envelope", `{"error":{"code":"boom","message":"bad"},"id":"cli:agent:list"}`, "boom"},
		{"not json", `herdr: server not running`, "not a JSON envelope"},
		{"empty", ``, "not a JSON envelope"},
		{"no id", `{"result":{"agents":[]}}`, "no id"},
		{"no agents field", `{"id":"x","result":{"type":"agent_list"}}`, "no agents field"},
		{"agents wrong type", `{"id":"x","result":{"agents":{}}}`, "decode agent list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(fake(tt.out, nil)).ListAgents(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}

func TestEmptyAgentsIsValid(t *testing.T) {
	t.Parallel()
	a, err := client(fake(`{"id":"x","result":{"agents":[]}}`, nil)).ListAgents(context.Background())
	if err != nil || len(a) != 0 {
		t.Fatalf("got %v %v", a, err)
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	r := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := &Client{Binary: "herdr", Timeout: 20 * time.Millisecond, Run: r}
	if _, err := c.ListAgents(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

func TestExecRunnerTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := ExecRunner(ctx, "sleep", "5"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}
