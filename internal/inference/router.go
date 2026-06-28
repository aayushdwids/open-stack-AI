package inference

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/telemetry"
)

// Manager builds backends from config and routes logical model names to them with
// weighted failover, emitting one route span wrapping a child inference span.
type Manager struct {
	tracer   *telemetry.Tracer
	backends map[string]Backend       // by config model name
	routes   map[string]config.Route  // logical name -> route
	models   map[string]config.Model
}

// NewManager constructs backends for every configured model.
func NewManager(cfg *config.Config, tracer *telemetry.Tracer) (*Manager, error) {
	m := &Manager{
		tracer:   tracer,
		backends: map[string]Backend{},
		routes:   cfg.Routing,
		models:   cfg.Models,
	}
	for name, mc := range cfg.Models {
		b, err := buildBackend(name, mc)
		if err != nil {
			return nil, err
		}
		m.backends[name] = b
	}
	return m, nil
}

func buildBackend(name string, mc config.Model) (Backend, error) {
	backend := mc.Backend
	endpoint := mc.Serve.Endpoint
	if endpoint == "" && mc.Serve.Port != 0 {
		endpoint = fmt.Sprintf("http://127.0.0.1:%d", mc.Serve.Port)
	}
	switch backend {
	case "mock":
		return NewMockBackend(name), nil
	case "vllm", "llama_cpp", "sglang":
		if endpoint == "" {
			return nil, fmt.Errorf("model %q backend %q requires serve.endpoint or serve.port", name, backend)
		}
		return NewProxyBackend(name, backend, endpoint, mc.Source), nil
	case "", "auto":
		// Auto-select: if a real endpoint is configured, proxy to it; otherwise fall back
		// to the GPU-free mock so the stack stays runnable on a CPU-only/offline host.
		if endpoint != "" {
			return NewProxyBackend(name, "auto", endpoint, mc.Source), nil
		}
		return NewMockBackend(name), nil
	default:
		return nil, fmt.Errorf("model %q: unknown backend %q", name, backend)
	}
}

// resolved is one candidate backend in failover order.
type resolved struct {
	model   string
	backend Backend
}

// resolve returns the ordered failover list for a logical name.
func (m *Manager) resolve(logical string) ([]resolved, error) {
	if r, ok := m.routes[logical]; ok && len(r.Backends) > 0 {
		bs := append([]config.RouteBackend(nil), r.Backends...)
		// Higher weight first; weight 0 is fallback-only (kept, but last).
		sort.SliceStable(bs, func(i, j int) bool { return bs[i].Weight > bs[j].Weight })
		var out []resolved
		for _, b := range bs {
			if be, ok := m.backends[b.Model]; ok {
				out = append(out, resolved{model: b.Model, backend: be})
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("route %q has no usable backends", logical)
		}
		return out, nil
	}
	if be, ok := m.backends[logical]; ok {
		return []resolved{{model: logical, backend: be}}, nil
	}
	return nil, fmt.Errorf("unknown model or route %q", logical)
}

// Models lists configured logical models for /v1/models.
func (m *Manager) Models() []ModelCard {
	seen := map[string]bool{}
	var out []ModelCard
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, ModelCard{ID: id, Object: "model", OwnedBy: "faraday"})
		}
	}
	for name := range m.routes {
		add(name)
	}
	for name := range m.models {
		add(name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ChatCompletion routes a request with failover, emitting a route span (parent) wrapping
// a child inference span recording backend and token usage.
func (m *Manager) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	candidates, err := m.resolve(req.Model)
	if err != nil {
		return ChatCompletionResponse{}, err
	}

	ctx, routeSpan := m.tracer.Start(ctx, "route "+req.Model, telemetry.KindInternal)
	routeSpan.SetAttr(telemetry.AttrRequestModel, req.Model)
	defer routeSpan.End()

	var lastErr error
	for depth, c := range candidates {
		_, chatSpan := m.tracer.Start(ctx, "chat "+c.model, telemetry.KindClient)
		chatSpan.SetAttrs(map[string]any{
			telemetry.AttrOperationName: "chat",
			telemetry.AttrRequestModel:  req.Model,
			telemetry.AttrProviderName:  "faraday",
			telemetry.AttrChosenBackend: c.backend.Name(),
		})
		if m.tracer.CaptureContent() {
			chatSpan.SetContent(telemetry.AttrInputMessages, req.Messages)
		}
		start := time.Now()
		resp, cerr := c.backend.ChatCompletion(ctx, req)
		if cerr != nil {
			chatSpan.SetStatus("error: " + cerr.Error())
			chatSpan.End()
			lastErr = cerr
			continue
		}
		chatSpan.SetAttrs(map[string]any{
			telemetry.AttrResponseModel: resp.Model,
			telemetry.AttrInputTokens:   resp.Usage.PromptTokens,
			telemetry.AttrOutputTokens:  resp.Usage.CompletionTokens,
			"faraday.latency_ms":        time.Since(start).Milliseconds(),
		})
		if len(resp.Choices) > 0 {
			chatSpan.SetAttr(telemetry.AttrFinishReasons, []string{resp.Choices[0].FinishReason})
			if m.tracer.CaptureContent() {
				chatSpan.SetContent(telemetry.AttrOutputMessages, resp.Choices[0].Message)
			}
		}
		chatSpan.SetStatus("ok")
		chatSpan.End()

		routeSpan.SetAttrs(map[string]any{
			telemetry.AttrChosenBackend: c.backend.Name(),
			telemetry.AttrFallbackDepth: depth,
		})
		routeSpan.SetStatus("ok")
		return resp, nil
	}
	routeSpan.SetStatus("error: all backends failed")
	return ChatCompletionResponse{}, fmt.Errorf("all backends failed for %q: %w", req.Model, lastErr)
}
