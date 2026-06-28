package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Backend serves chat completions.
type Backend interface {
	Name() string
	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error)
	Health(ctx context.Context) error
}

// ---- Mock backend: deterministic, GPU-free, network-free (the CI default) ----

// MockBackend returns deterministic responses. It recognizes the bundled fixture tasks
// by the requested function name and returns a correct implementation, so the whole
// stack runs and the eval pipeline is exercised without a GPU or any model weights.
// It is for wiring/CI determinism only — NOT a measure of model quality.
type MockBackend struct{ name string }

// NewMockBackend constructs a mock backend.
func NewMockBackend(name string) *MockBackend { return &MockBackend{name: name} }

// Name returns the backend name.
func (m *MockBackend) Name() string { return m.name }

// Health always succeeds.
func (m *MockBackend) Health(context.Context) error { return nil }

// mockSolutions maps a recognizable function name to a correct Python implementation.
var mockSolutions = map[string]string{
	"add":           "def add(a, b):\n    return a + b\n",
	"subtract":      "def subtract(a, b):\n    return a - b\n",
	"reverse_string": "def reverse_string(s):\n    return s[::-1]\n",
	"is_palindrome": "def is_palindrome(s):\n    s = [c.lower() for c in s if c.isalnum()]\n    return s == s[::-1]\n",
	"factorial": "def factorial(n):\n    r = 1\n    for i in range(2, n + 1):\n        r *= i\n    return r\n",
	"fib": "def fib(n):\n    a, b = 0, 1\n    for _ in range(n):\n        a, b = b, a + b\n    return a\n",
	"max_of_list": "def max_of_list(xs):\n    m = xs[0]\n    for x in xs[1:]:\n        if x > m:\n            m = x\n    return m\n",
}

// ChatCompletion returns a deterministic completion.
func (m *MockBackend) ChatCompletion(_ context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	prompt := strings.ToLower(req.LastUser())
	var code string
	var chosen string
	for fn, impl := range mockSolutions {
		if strings.Contains(prompt, fn) {
			// pick the longest matching name to disambiguate (e.g. "add" vs "max_of_list")
			if len(fn) > len(chosen) {
				chosen = fn
				code = impl
			}
		}
	}
	var content string
	if code != "" {
		content = "Here is the implementation:\n\n```python\n" + code + "```\n"
	} else {
		// Unknown task: return a harmless stub (will fail tests — realistic).
		content = "```python\ndef solution(*args, **kwargs):\n    raise NotImplementedError\n```\n"
	}
	pt := tokenize(req.LastUser())
	ct := tokenize(content)
	return ChatCompletionResponse{
		ID:      "chatcmpl-mock-" + shortHash(req.LastUser()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: content}, FinishReason: "stop"}},
		Usage:   Usage{PromptTokens: pt, CompletionTokens: ct, TotalTokens: pt + ct},
	}, nil
}

func tokenize(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Fields(s))
}

func shortHash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}

// ---- HTTP proxy backend: forwards to a real OpenAI-compatible server (vLLM/llama.cpp) ----

// ProxyBackend forwards /v1/chat/completions to an existing OpenAI-compatible endpoint.
// The pyservices launchers stand up vLLM or llama-server and expose such an endpoint;
// this backend keeps the daemon as the single OpenAI surface and telemetry owner.
type ProxyBackend struct {
	name     string
	endpoint string // e.g. http://127.0.0.1:8001
	model    string // model name the upstream server expects
	client   *http.Client
}

// NewProxyBackend constructs a proxy backend. kind is "vllm" or "llama_cpp" (informational).
func NewProxyBackend(name, kind, endpoint, model string) *ProxyBackend {
	return &ProxyBackend{
		name:     name,
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

// Name returns the backend name.
func (p *ProxyBackend) Name() string { return p.name }

// Health probes the upstream /v1/models endpoint.
func (p *ProxyBackend) Health(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/v1/models", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upstream unhealthy: %d", resp.StatusCode)
	}
	return nil
}

// ChatCompletion forwards the request to the upstream server.
func (p *ProxyBackend) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	if p.model != "" {
		req.Model = p.model
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatCompletionResponse{}, fmt.Errorf("proxy %s: %w", p.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return ChatCompletionResponse{}, fmt.Errorf("proxy %s upstream %d: %s", p.name, resp.StatusCode, string(data))
	}
	var out ChatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return ChatCompletionResponse{}, fmt.Errorf("proxy %s decode: %w", p.name, err)
	}
	return out, nil
}
