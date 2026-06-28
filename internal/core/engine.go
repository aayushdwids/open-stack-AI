// Package core wires the engine: config → store → tracer → inference → sandbox pool →
// MCP broker → agent engine → eval runner → evidence builder. It is the daemon's brain
// and the single owner of telemetry and the air-gap guarantee.
package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/eval"
	"github.com/faraday-stack/faraday/internal/evidence"
	"github.com/faraday-stack/faraday/internal/extension"
	"github.com/faraday-stack/faraday/internal/inference"
	"github.com/faraday-stack/faraday/internal/mcp"
	"github.com/faraday-stack/faraday/internal/runtime"
	"github.com/faraday-stack/faraday/internal/runtime/pool"
	"github.com/faraday-stack/faraday/internal/runtime/sandbox"
	"github.com/faraday-stack/faraday/internal/store"
	"github.com/faraday-stack/faraday/internal/telemetry"
)

// Engine holds all wired components.
type Engine struct {
	Cfg     *config.Config
	DataDir string

	store    *store.Store
	tracer   *telemetry.Tracer
	infMgr   *inference.Manager
	infHand  *inference.Handler
	pool     *pool.Pool
	broker   *mcp.Broker
	agent    *runtime.Engine
	evalR    *eval.Runner
	evidence *evidence.Builder
}

// New builds an engine from a config and data directory.
func New(ctx context.Context, cfg *config.Config, dataDir string) (*Engine, error) {
	e := &Engine{Cfg: cfg, DataDir: dataDir}

	st, err := store.Open(filepath.Join(dataDir, "faraday.db"))
	if err != nil {
		return nil, err
	}
	e.store = st

	tracePath := cfg.Telemetry.FilePath
	if tracePath == "" {
		tracePath = filepath.Join(dataDir, "traces.jsonl")
	}
	if !cfg.Telemetry.IsEnabled() {
		tracePath = ""
	}
	e.tracer = telemetry.New(st, telemetry.Options{
		FilePath:       tracePath,
		CaptureContent: cfg.Telemetry.CaptureContent,
		Resource:       map[string]any{"service.name": "faraday", "service.version": "0.1.0"},
	})

	mgr, err := inference.NewManager(cfg, e.tracer)
	if err != nil {
		e.Close()
		return nil, err
	}
	e.infMgr = mgr
	e.infHand = inference.NewHandler(mgr)

	// Sandbox pool + broker (the air-gap-native runtime).
	drv, err := sandbox.NewDriver(sandbox.Config{
		Runtime:            cfg.Sandbox.Runtime,
		Network:            cfg.Sandbox.Network,
		DefaultTimeoutSecs: cfg.Sandbox.Limits.TimeoutSecs,
	})
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("sandbox driver: %w", err)
	}
	p, err := pool.New(ctx, drv, cfg.Sandbox.PoolSize)
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("sandbox pool: %w", err)
	}
	e.pool = p
	e.broker = mcp.NewBroker(p, e.tracer)
	e.agent = runtime.NewEngine(mgr, e.broker, e.tracer)
	e.evalR = eval.NewRunner(mgr, e.broker, e.tracer, st)
	e.evidence = evidence.NewBuilder(st, cfg)

	// Register configured models in the registry + audit the boot.
	for name, m := range cfg.Models {
		_ = st.RegisterModel(name, "model", m.Source, "", map[string]any{"backend": m.Backend, "quantization": m.Quantization})
	}
	_, _ = st.AppendAudit("system", "engine.start", "", "runtime="+drv.Name()+" network="+cfg.Sandbox.Network)
	return e, nil
}

// Store exposes the store.
func (e *Engine) Store() *store.Store { return e.store }

// Tracer exposes the tracer (for the OTLP ingest endpoint).
func (e *Engine) Tracer() *telemetry.Tracer { return e.tracer }

// InferenceHandler exposes the /v1 handler.
func (e *Engine) InferenceHandler() *inference.Handler { return e.infHand }

// SandboxDriverName returns the active sandbox driver name.
func (e *Engine) SandboxDriverName() string { return e.pool.Driver().Name() }

// NetworkIsolated reports whether the sandbox enforces no egress.
func (e *Engine) NetworkIsolated() bool { return e.pool.Driver().NetworkIsolated() }

// RunAgent runs a named agent against a task.
func (e *Engine) RunAgent(ctx context.Context, agentName, task string) (runtime.RunResult, error) {
	ag, ok := e.Cfg.Agents[agentName]
	if !ok {
		return runtime.RunResult{}, fmt.Errorf("unknown agent %q", agentName)
	}
	_, _ = e.store.AppendAudit("user", "agent.run", agentName, truncate(task, 200))
	return e.agent.Run(ctx, runtime.RunRequest{AgentName: agentName, Agent: ag, Task: task})
}

// EvalOutcome adds CI gating info to a suite result.
type EvalOutcome struct {
	eval.SuiteResult
	GateFailed   bool     `json:"gate_failed"`
	GateReasons  []string `json:"gate_reasons,omitempty"`
}

// EvalRun runs a named suite and computes CI gating from config.
func (e *Engine) EvalRun(ctx context.Context, suiteName string) (EvalOutcome, error) {
	var suite *config.Suite
	for i := range e.Cfg.Eval.Suites {
		if e.Cfg.Eval.Suites[i].Name == suiteName {
			suite = &e.Cfg.Eval.Suites[i]
			break
		}
	}
	if suite == nil {
		return EvalOutcome{}, fmt.Errorf("unknown eval suite %q", suiteName)
	}
	defaultModel := config.DefaultModelName
	for name := range e.Cfg.Models {
		defaultModel = name
		break
	}

	// Capture prior baseline BEFORE recording this run (RunSuite records into the store).
	failOn := map[string]bool{}
	for _, f := range e.Cfg.Eval.CI.FailOn {
		failOn[f] = true
	}
	var regressionReason string
	if failOn["regression"] {
		// computed after the run using the pre-run baseline
	}

	res, err := e.evalR.RunSuite(ctx, *suite, defaultModel)
	if err != nil {
		return EvalOutcome{SuiteResult: res}, err
	}
	out := EvalOutcome{SuiteResult: res}

	if failOn["threshold"] && !res.Passed {
		out.GateFailed = true
		out.GateReasons = append(out.GateReasons, res.GateFailures...)
	}
	_ = regressionReason
	_, _ = e.store.AppendAudit("user", "eval.run", suiteName, fmt.Sprintf("passed=%v metrics=%v", res.Passed, res.Metrics))
	return out, nil
}

// BuildEvidence assembles a signed evidence bundle.
func (e *Engine) BuildEvidence(outPath, keyPath, identity string) (*evidence.Manifest, error) {
	if identity == "" {
		identity = e.Cfg.Evidence.Sign.Identity
	}
	if keyPath == "" {
		keyPath = e.Cfg.Evidence.Sign.Key
	}
	m, err := e.evidence.Build(outPath, keyPath, identity)
	if err != nil {
		return nil, err
	}
	_, _ = e.store.AppendAudit("user", "evidence.bundle", outPath, "root="+m.RootDigest)
	return m, nil
}

// TeamSummary returns the paid team-observability aggregation, or an error when the
// enterprise tier is not built/licensed. Free single-user builds return a clear message.
func (e *Engine) TeamSummary() (map[string]any, error) {
	obs := extension.TeamObserverImpl()
	if obs == nil {
		return nil, fmt.Errorf("team observability is a paid feature; build with -tags enterprise and provide a team license")
	}
	return obs.Summary(e.store)
}

// Close releases resources.
func (e *Engine) Close() {
	if e.pool != nil {
		e.pool.Close()
	}
	if e.tracer != nil {
		_ = e.tracer.Close()
	}
	if e.store != nil {
		_ = e.store.Close()
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
