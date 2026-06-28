package inference

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/store"
	"github.com/faraday-stack/faraday/internal/telemetry"
)

type failBackend struct{ name string }

func (f *failBackend) Name() string                            { return f.name }
func (f *failBackend) Health(context.Context) error            { return errors.New("down") }
func (f *failBackend) ChatCompletion(context.Context, ChatCompletionRequest) (ChatCompletionResponse, error) {
	return ChatCompletionResponse{}, errors.New("backend down")
}

func newTracer(t *testing.T) *telemetry.Tracer {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return telemetry.New(st, telemetry.Options{})
}

func TestRouterFailover(t *testing.T) {
	cfg, _ := config.Parse([]byte("version: faraday/v1\n" +
		"models:\n  primary: {backend: mock}\n  secondary: {backend: mock}\n" +
		"routing:\n  coder:\n    backends:\n      - {model: primary, weight: 10}\n      - {model: secondary, weight: 1}\n" +
		"agents:\n  x: {model: coder}\n"))
	mgr, err := NewManager(cfg, newTracer(t))
	if err != nil {
		t.Fatal(err)
	}
	// Force the primary to fail so failover engages.
	mgr.backends["primary"] = &failBackend{name: "primary"}

	resp, err := mgr.ChatCompletion(context.Background(), ChatCompletionRequest{
		Model:    "coder",
		Messages: []Message{{Role: "user", Content: "implement add"}},
	})
	if err != nil {
		t.Fatalf("expected failover to succeed: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices returned")
	}
}

func TestMockSolvesKnownTask(t *testing.T) {
	cfg, _ := config.Parse([]byte("version: faraday/v1\nmodels:\n  coder: {backend: mock}\nagents:\n  x: {model: coder}\n"))
	mgr, _ := NewManager(cfg, newTracer(t))
	resp, err := mgr.ChatCompletion(context.Background(), ChatCompletionRequest{
		Model:    "coder",
		Messages: []Message{{Role: "user", Content: "implement reverse_string"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.CompletionTokens == 0 {
		t.Error("expected non-zero completion tokens")
	}
}
