package herdr

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func fake(out string, err error) Runner {
	return func(context.Context, string, ...string) ([]byte, error) { return []byte(out), err }
}

func client(r Runner) *Client { return &Client{Binary: "herdr", Timeout: time.Second, Run: r} }

func TestListAgentsFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/agent_list.json")
	if err != nil {
		t.Fatal(err)
	}
	agents, err := client(fake(string(data), nil)).ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 6 {
		t.Fatalf("want 6 agents, got %d", len(agents))
	}
	a := agents[1]
	want := Agent{Name: "claude-cc-631", PaneID: "w1:p49", TabID: "w1:t49", WorkspaceID: "w1", Agent: "claude",
		AgentStatus: "blocked", StateChangeSeq: 1904, Revision: 3, InteractiveReady: true}
	a.Cwd, a.AgentSession = "", AgentSession{}
	if a != want {
		t.Fatalf("got %+v want %+v", a, want)
	}
	if agents[1].AgentSession.Value == "" || agents[1].AgentSession.Kind != "id" || !strings.HasPrefix(agents[1].Cwd, "~/") {
		t.Fatalf("session/cwd not decoded: %+v", agents[1])
	}
}

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

func TestRunnerErrorWithEnvelope(t *testing.T) {
	t.Parallel()
	env := `{"error":{"code":"agent_not_found","message":"nope"},"id":"cli:agent:read"}`
	_, err := client(fake(env, errors.New("exit status 1"))).ReadAgent(context.Background(), "x", "detection", 5)
	var api *APIError
	if !errors.As(err, &api) || api.Code != "agent_not_found" {
		t.Fatalf("want APIError, got %v", err)
	}
	_, err = client(fake("", errors.New("exit status 1"))).ListAgents(context.Background())
	if err == nil || errors.As(err, &api) {
		t.Fatalf("want plain runner error, got %v", err)
	}
}

func TestReadAgent(t *testing.T) {
	t.Parallel()
	var got []string
	r := func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte("line1\nline2\n"), nil
	}
	text, err := client(r).ReadAgent(context.Background(), "a", "detection", 7)
	if err != nil || text != "line1\nline2\n" {
		t.Fatalf("got %q %v", text, err)
	}
	if strings.Join(got, " ") != "herdr agent read a --source detection --lines 7" {
		t.Fatalf("args: %v", got)
	}
	// A screen that merely mentions an error is text, not an envelope.
	text, err = client(fake(`{"error": "just screen text"}`, nil)).ReadAgent(context.Background(), "a", "", 0)
	if err != nil || text == "" {
		t.Fatalf("screen text misread as error: %q %v", text, err)
	}
	// Exit 0 means text, even when the screen holds a JSON error envelope.
	screen := `{"error":{"code":"x"},"note":"screen json"}`
	text, err = client(fake(screen, nil)).ReadAgent(context.Background(), "a", "", 0)
	if err != nil || text != screen {
		t.Fatalf("screen json on exit 0 must be text: %q %v", text, err)
	}
}

func TestNewRejectsNonPositiveTimeout(t *testing.T) {
	t.Parallel()
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := New("herdr", d); err == nil {
			t.Fatalf("timeout %v accepted", d)
		}
	}
	if c, err := New("herdr", time.Second); err != nil || c.Run == nil {
		t.Fatalf("valid timeout rejected: %v", err)
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

// A grandchild holding stdout open must not keep the runner blocked past the deadline.
func TestExecRunnerGrandchildHoldingStdout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ExecRunner(ctx, "sh", "-c", "sleep 4 & wait")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("runner blocked %v past the deadline", d)
	}
}
