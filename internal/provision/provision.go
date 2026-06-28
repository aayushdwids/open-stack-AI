// Package provision resolves a faraday.yaml target into a deployment. The SAME config
// deploys to a rented cloud GPU (TRY) or an offline owned machine (OWN); only target.kind
// changes. local/airgap providers are static (provisioning on owned hardware is a no-op);
// the cloud provider produces a SkyPilot/dstack-style plan (execution deferred).
package provision

import (
	"context"
	"fmt"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/runtime/sandbox"
)

// Status reports the outcome of bringing a target up.
type Status struct {
	Target  string         `json:"target"`
	Ready   bool           `json:"ready"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Provider brings a target up and down.
type Provider interface {
	Name() string
	Up(ctx context.Context, cfg *config.Config) (Status, error)
	Down(ctx context.Context) error
}

// Select returns the provider for the config's target.
func Select(cfg *config.Config) (Provider, error) {
	switch cfg.Target.Kind {
	case "local":
		return &staticProvider{kind: "local"}, nil
	case "airgap":
		return &staticProvider{kind: "airgap"}, nil
	case "cloud":
		return &cloudProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown target.kind %q", cfg.Target.Kind)
	}
}

// staticProvider handles local and airgap: nothing to provision on owned hardware; it
// confirms the air-gap-native runtime is available and reports readiness.
type staticProvider struct{ kind string }

func (p *staticProvider) Name() string { return p.kind }

func (p *staticProvider) Up(_ context.Context, cfg *config.Config) (Status, error) {
	st := Status{Target: p.kind, Details: map[string]any{}}
	drv, err := sandbox.NewDriver(sandbox.Config{Runtime: cfg.Sandbox.Runtime, Network: cfg.Sandbox.Network})
	if err != nil {
		st.Message = "sandbox runtime unavailable: " + err.Error()
		return st, nil
	}
	models := make([]string, 0, len(cfg.Models))
	for name := range cfg.Models {
		models = append(models, name)
	}
	st.Ready = true
	st.Details["sandbox_driver"] = drv.Name()
	st.Details["network_isolated"] = drv.NetworkIsolated()
	st.Details["models"] = models
	if p.kind == "airgap" {
		st.Message = "ready (offline) — provisioning is a no-op on owned hardware; start with: faraday daemon"
		st.Details["note"] = "expects a pre-installed signed bundle (faraday bundle install)"
	} else {
		st.Message = "ready — start with: faraday daemon"
	}
	return st, nil
}

func (p *staticProvider) Down(context.Context) error { return nil }

// cloudProvider produces a provisioning plan for rented GPUs. Actual cloud calls are
// deferred (implement now, validate later); the plan proves the spine: the same config's
// models/agents/eval blocks are unchanged vs the airgap target.
type cloudProvider struct{}

func (p *cloudProvider) Name() string { return "cloud" }

func (p *cloudProvider) Up(_ context.Context, cfg *config.Config) (Status, error) {
	res := cfg.Target.Resources
	plan := map[string]any{
		"provider":        "skypilot/dstack-style (deferred)",
		"infra":           cfg.Target.Infra,
		"accelerators":    res.Accelerators,
		"cpus":            res.CPUs,
		"memory":          res.Memory,
		"disk":            res.Disk,
		"spot":            cfg.Target.Spot,
		"idle_teardown_m": cfg.Target.IdleTeardownMinutes,
		"steps": []string{
			"resolve cheapest capacity across configured clouds",
			"pull the signed model bundle to the rented box",
			"launch vLLM and the agent runtime",
			"tear down on idle timeout or `faraday down`",
		},
	}
	return Status{
		Target:  "cloud",
		Ready:   false,
		Message: "cloud provisioning planned (execution deferred — provide cloud credentials to enable)",
		Details: plan,
	}, nil
}

func (p *cloudProvider) Down(context.Context) error { return nil }
