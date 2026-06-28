package runtime

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/inference"
	"github.com/faraday-stack/faraday/internal/mcp"
	"github.com/faraday-stack/faraday/internal/runtime/pool"
	"github.com/faraday-stack/faraday/internal/runtime/sandbox"
	"github.com/faraday-stack/faraday/internal/store"
	"github.com/faraday-stack/faraday/internal/telemetry"
)

// fakeGen returns a scripted sequence of completions to exercise the repair loop.
type fakeGen struct {
	replies []string
	n       int
}

func (f *fakeGen) ChatCompletion(_ context.Context, _ inference.ChatCompletionRequest) (inference.ChatCompletionResponse, error) {
	r := f.replies[min(f.n, len(f.replies)-1)]
	f.n++
	return inference.ChatCompletionResponse{
		Model:   "fake",
		Choices: []inference.Choice{{Message: inference.Message{Role: "assistant", Content: r}, FinishReason: "stop"}},
		Usage:   inference.Usage{CompletionTokens: 10},
	}, nil
}

func buildEngine(t *testing.T, gen Generator) *Engine {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tr := telemetry.New(st, telemetry.Options{})
	drv, err := sandbox.NewLocalDriver(sandbox.Config{Network: "none", DefaultTimeoutSecs: 20})
	if err != nil {
		t.Skipf("local driver unavailable: %v", err)
	}
	p, err := pool.New(context.Background(), drv, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	broker := mcp.NewBroker(p, tr)
	return NewEngine(gen, broker, tr)
}

func TestAgentRepairLoop(t *testing.T) {
	gen := &fakeGen{replies: []string{
		"```python\ndef add(a, b):\n    return a - b\n```", // wrong
		"```python\ndef add(a, b):\n    return a + b\n```", // fixed
	}}
	e := buildEngine(t, gen)
	tests := "from solution import add\nassert add(2, 3) == 5\nprint('ok')\n"
	res, err := e.Run(context.Background(), RunRequest{
		AgentName: "fixer",
		Agent:     config.Agent{Model: "fake", MaxRepairIterations: 3},
		Task:      "implement add",
		Tests:     tests,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "solved" || !res.Passed {
		t.Fatalf("expected solved, got %+v", res)
	}
	if res.Iterations != 2 {
		t.Errorf("expected 2 iterations (1 repair), got %d", res.Iterations)
	}
}

func TestAgentBoundedByLimit(t *testing.T) {
	gen := &fakeGen{replies: []string{"```python\ndef add(a, b):\n    return a - b\n```"}} // always wrong
	e := buildEngine(t, gen)
	tests := "from solution import add\nassert add(2, 3) == 5\n"
	res, err := e.Run(context.Background(), RunRequest{
		AgentName: "fixer",
		Agent:     config.Agent{Model: "fake", MaxRepairIterations: 2},
		Task:      "implement add",
		Tests:     tests,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "unresolved" {
		t.Fatalf("expected unresolved at limit, got %s", res.Status)
	}
	if res.Iterations != 3 { // initial + 2 repairs
		t.Errorf("expected 3 attempts, got %d", res.Iterations)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
