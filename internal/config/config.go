// Package config defines the faraday.yaml schema, its defaults, loading, and
// validation. The contract: almost nothing is required, everything is optional with a
// good default. The same file deploys to cloud or air-gap; only the Target block differs.
package config

// CurrentVersion is the schema version this binary writes and expects.
const CurrentVersion = "faraday/v1"

// Config is the root of faraday.yaml.
type Config struct {
	Version   string             `yaml:"version" json:"version"`
	Name      string             `yaml:"name,omitempty" json:"name,omitempty"`
	Target    Target             `yaml:"target,omitempty" json:"target,omitempty"`
	Models    map[string]Model   `yaml:"models,omitempty" json:"models,omitempty"`
	Routing   map[string]Route   `yaml:"routing,omitempty" json:"routing,omitempty"`
	Agents    map[string]Agent   `yaml:"agents,omitempty" json:"agents,omitempty"`
	Tools     Tools              `yaml:"tools,omitempty" json:"tools,omitempty"`
	Sandbox   Sandbox            `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
	Eval      Eval               `yaml:"eval,omitempty" json:"eval,omitempty"`
	Telemetry Telemetry          `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`
	Evidence  Evidence           `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Bundle    Bundle             `yaml:"bundle,omitempty" json:"bundle,omitempty"`
	Secrets   map[string]string  `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Env       map[string]string  `yaml:"env,omitempty" json:"env,omitempty"`
}

// Target is the ONLY block that differs between cloud-rented and air-gapped-owned.
type Target struct {
	// Kind is one of: local, cloud, airgap.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	// Infra is a SkyPilot-style provider/region hint (cloud only), e.g. aws/us-east-1.
	Infra     string    `yaml:"infra,omitempty" json:"infra,omitempty"`
	Resources Resources `yaml:"resources,omitempty" json:"resources,omitempty"`
	// Spot and IdleTeardownMinutes are cloud-only and ignored for airgap/local.
	Spot                 bool `yaml:"spot,omitempty" json:"spot,omitempty"`
	IdleTeardownMinutes  int  `yaml:"idle_teardown_minutes,omitempty" json:"idle_teardown_minutes,omitempty"`
}

// Resources describes requested compute.
type Resources struct {
	Accelerators string `yaml:"accelerators,omitempty" json:"accelerators,omitempty"` // e.g. A100:2, or "none"
	CPUs         int    `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory       string `yaml:"memory,omitempty" json:"memory,omitempty"`
	Disk         string `yaml:"disk,omitempty" json:"disk,omitempty"`
}

// Model is a logical model definition.
type Model struct {
	Source             string   `yaml:"source,omitempty" json:"source,omitempty"`
	Backend            string   `yaml:"backend,omitempty" json:"backend,omitempty"` // vllm | llama_cpp | sglang | mock
	Quantization       string   `yaml:"quantization,omitempty" json:"quantization,omitempty"`
	MaxModelLen        int      `yaml:"max_model_len,omitempty" json:"max_model_len,omitempty"`
	GPUMemoryUtil      float64  `yaml:"gpu_memory_utilization,omitempty" json:"gpu_memory_utilization,omitempty"`
	TensorParallelSize int      `yaml:"tensor_parallel_size,omitempty" json:"tensor_parallel_size,omitempty"`
	Serve              Serve    `yaml:"serve,omitempty" json:"serve,omitempty"`
}

// Serve controls how a backend process is launched.
type Serve struct {
	Port      int      `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoint  string   `yaml:"endpoint,omitempty" json:"endpoint,omitempty"` // attach to an existing /v1 server
	ExtraArgs []string `yaml:"extra_args,omitempty" json:"extra_args,omitempty"`
}

// Route maps a logical name to backends with a failover strategy.
type Route struct {
	Strategy       string         `yaml:"strategy,omitempty" json:"strategy,omitempty"` // weighted_failover
	Backends       []RouteBackend `yaml:"backends,omitempty" json:"backends,omitempty"`
	CooldownSecs   int            `yaml:"cooldown_seconds,omitempty" json:"cooldown_seconds,omitempty"`
	TimeoutSecs    int            `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	Retries        int            `yaml:"retries,omitempty" json:"retries,omitempty"`
}

// RouteBackend is one backend within a route.
type RouteBackend struct {
	Model  string `yaml:"model" json:"model"`
	Weight int    `yaml:"weight,omitempty" json:"weight,omitempty"`
}

// Agent defines an agent.
type Agent struct {
	Model               string   `yaml:"model,omitempty" json:"model,omitempty"`
	System              string   `yaml:"system,omitempty" json:"system,omitempty"`
	Orchestration       string   `yaml:"orchestration,omitempty" json:"orchestration,omitempty"` // plan_execute_repair
	Tools               []string `yaml:"tools,omitempty" json:"tools,omitempty"`
	MaxSteps            int      `yaml:"max_steps,omitempty" json:"max_steps,omitempty"`
	MaxRepairIterations int      `yaml:"max_repair_iterations,omitempty" json:"max_repair_iterations,omitempty"`
	TimeoutSecs         int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	Temperature         float64  `yaml:"temperature,omitempty" json:"temperature,omitempty"`
}

// Tools declares MCP servers / built-in tools.
type Tools struct {
	MCP []ToolDef `yaml:"mcp,omitempty" json:"mcp,omitempty"`
}

// ToolDef is a single tool/MCP server definition.
type ToolDef struct {
	Name      string   `yaml:"name" json:"name"`
	Runtime   string   `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Transport string   `yaml:"transport,omitempty" json:"transport,omitempty"` // stdio | http
	Command   []string `yaml:"command,omitempty" json:"command,omitempty"`
	Root      string   `yaml:"root,omitempty" json:"root,omitempty"`
}

// Sandbox controls the agent runtime sandbox.
type Sandbox struct {
	Runtime       string        `yaml:"runtime,omitempty" json:"runtime,omitempty"` // gvisor | firecracker | libkrun | local
	PoolSize      int           `yaml:"pool_size,omitempty" json:"pool_size,omitempty"`
	Image         string        `yaml:"image,omitempty" json:"image,omitempty"`
	Network       string        `yaml:"network,omitempty" json:"network,omitempty"` // none | host-deny
	Limits        SandboxLimits `yaml:"limits,omitempty" json:"limits,omitempty"`
	HighIsolation bool          `yaml:"high_isolation,omitempty" json:"high_isolation,omitempty"`
}

// SandboxLimits caps per-sandbox resources.
type SandboxLimits struct {
	CPUs        int    `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory      string `yaml:"memory,omitempty" json:"memory,omitempty"`
	PIDs        int    `yaml:"pids,omitempty" json:"pids,omitempty"`
	TimeoutSecs int    `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
}

// Eval declares eval suites and gating.
type Eval struct {
	Suites     []Suite    `yaml:"suites,omitempty" json:"suites,omitempty"`
	Regression Regression `yaml:"regression,omitempty" json:"regression,omitempty"`
	CI         CI         `yaml:"ci,omitempty" json:"ci,omitempty"`
}

// Suite is one eval suite.
type Suite struct {
	Name      string            `yaml:"name" json:"name"`
	Kind      string            `yaml:"kind" json:"kind"` // code_passk | deterministic | judge | regression
	Dataset   string            `yaml:"dataset,omitempty" json:"dataset,omitempty"`
	K         int               `yaml:"k,omitempty" json:"k,omitempty"`
	NSamples  int               `yaml:"n_samples,omitempty" json:"n_samples,omitempty"`
	Judge     string            `yaml:"judge,omitempty" json:"judge,omitempty"`
	Rubric    string            `yaml:"rubric,omitempty" json:"rubric,omitempty"`
	Checks    []string          `yaml:"checks,omitempty" json:"checks,omitempty"`
	Sandbox   *Sandbox          `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
	Threshold map[string]float64 `yaml:"threshold,omitempty" json:"threshold,omitempty"`
}

// Regression configures run-over-run comparison.
type Regression struct {
	Baseline string `yaml:"baseline,omitempty" json:"baseline,omitempty"` // last_passing | last
}

// CI configures gating exit codes.
type CI struct {
	FailOn []string `yaml:"fail_on,omitempty" json:"fail_on,omitempty"` // threshold | regression
}

// Telemetry controls span emission and sinks.
type Telemetry struct {
	Enabled        *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Sink           string `yaml:"sink,omitempty" json:"sink,omitempty"` // sqlite | file | both
	FilePath       string `yaml:"file_path,omitempty" json:"file_path,omitempty"`
	Sampling       string `yaml:"sampling,omitempty" json:"sampling,omitempty"`
	CaptureContent bool   `yaml:"capture_content,omitempty" json:"capture_content,omitempty"`
	RetentionDays  int    `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
	Semconv        string `yaml:"semconv,omitempty" json:"semconv,omitempty"`
}

// Evidence controls the evidence bundle.
type Evidence struct {
	Include           []string  `yaml:"include,omitempty" json:"include,omitempty"`
	ControlFrameworks []string  `yaml:"control_frameworks,omitempty" json:"control_frameworks,omitempty"`
	Sign              EvideSign `yaml:"sign,omitempty" json:"sign,omitempty"`
}

// EvideSign is the signing identity for evidence bundles.
type EvideSign struct {
	Key      string `yaml:"key,omitempty" json:"key,omitempty"`
	Identity string `yaml:"identity,omitempty" json:"identity,omitempty"`
}

// Bundle controls air-gap delivery.
type Bundle struct {
	Registry        string `yaml:"registry,omitempty" json:"registry,omitempty"`
	SignKey         string `yaml:"sign_key,omitempty" json:"sign_key,omitempty"`
	TrustedRoot     string `yaml:"trusted_root,omitempty" json:"trusted_root,omitempty"`
	WeightsChunking string `yaml:"weights_chunking,omitempty" json:"weights_chunking,omitempty"`
}

// Enabled reports whether telemetry is on (default true).
func (t Telemetry) IsEnabled() bool {
	if t.Enabled == nil {
		return true
	}
	return *t.Enabled
}
