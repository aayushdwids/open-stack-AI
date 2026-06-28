package config

// Default constants for the air-gap-first, secure-by-default posture.
const (
	DefaultTargetKind     = "local"
	DefaultModelName      = "coder"
	DefaultModelSource    = "qwen2.5-coder-32b"
	DefaultSandboxRuntime = "auto" // resolves to gvisor if present else local
	DefaultSandboxNetwork = "none" // the air-gap default: no egress
	DefaultSandboxPool    = 2
	DefaultMaxSteps       = 12
	DefaultMaxRepair      = 4
	DefaultAgentTimeout   = 600
	DefaultSandboxTimeout = 120
	DefaultTelemetrySink  = "both"
	DefaultSemconv        = "gen_ai_latest_experimental"
	DefaultRetentionDays  = 90
)

// ApplyDefaults fills every omitted field with its documented default, in place.
// After this, a 2-line config behaves identically to a fully specified one.
func (c *Config) ApplyDefaults() {
	if c.Version == "" {
		c.Version = CurrentVersion
	}

	// Target: local, network egress off at the sandbox by default.
	if c.Target.Kind == "" {
		c.Target.Kind = DefaultTargetKind
	}

	// Models: if none declared, provide the bundled default code model.
	if c.Models == nil {
		c.Models = map[string]Model{}
	}
	if len(c.Models) == 0 {
		c.Models[DefaultModelName] = Model{Source: DefaultModelSource}
	}
	for name, m := range c.Models {
		if m.Backend == "" {
			m.Backend = "" // empty => auto-select at runtime by hardware
		}
		c.Models[name] = m
	}

	// Tools: default to the built-in sandboxed executor.
	if len(c.Tools.MCP) == 0 {
		c.Tools.MCP = []ToolDef{{Name: "code_exec"}}
	}

	// Agents: fill per-agent defaults; an empty agent inherits the default model+tool.
	if c.Agents == nil {
		c.Agents = map[string]Agent{}
	}
	for name, a := range c.Agents {
		if a.Model == "" {
			a.Model = DefaultModelName
		}
		if a.Orchestration == "" {
			a.Orchestration = "plan_execute_repair"
		}
		if len(a.Tools) == 0 {
			a.Tools = []string{"code_exec"}
		}
		if a.MaxSteps == 0 {
			a.MaxSteps = DefaultMaxSteps
		}
		if a.MaxRepairIterations == 0 {
			a.MaxRepairIterations = DefaultMaxRepair
		}
		if a.TimeoutSecs == 0 {
			a.TimeoutSecs = DefaultAgentTimeout
		}
		c.Agents[name] = a
	}

	// Sandbox: secure defaults even when the block is omitted.
	if c.Sandbox.Runtime == "" {
		c.Sandbox.Runtime = DefaultSandboxRuntime
	}
	if c.Sandbox.Network == "" {
		c.Sandbox.Network = DefaultSandboxNetwork
	}
	if c.Sandbox.PoolSize == 0 {
		c.Sandbox.PoolSize = DefaultSandboxPool
	}
	if c.Sandbox.Limits.TimeoutSecs == 0 {
		c.Sandbox.Limits.TimeoutSecs = DefaultSandboxTimeout
	}
	if c.Sandbox.Limits.CPUs == 0 {
		c.Sandbox.Limits.CPUs = 2
	}
	if c.Sandbox.Limits.Memory == "" {
		c.Sandbox.Limits.Memory = "4GB"
	}
	if c.Sandbox.Limits.PIDs == 0 {
		c.Sandbox.Limits.PIDs = 256
	}

	// Telemetry: on by default, content capture off, both sinks.
	if c.Telemetry.Sink == "" {
		c.Telemetry.Sink = DefaultTelemetrySink
	}
	if c.Telemetry.Sampling == "" {
		c.Telemetry.Sampling = "always_on"
	}
	if c.Telemetry.Semconv == "" {
		c.Telemetry.Semconv = DefaultSemconv
	}
	if c.Telemetry.RetentionDays == 0 {
		c.Telemetry.RetentionDays = DefaultRetentionDays
	}

	// Eval suite defaults.
	for i := range c.Eval.Suites {
		if c.Eval.Suites[i].K == 0 {
			c.Eval.Suites[i].K = 1
		}
		if c.Eval.Suites[i].Kind == "code_passk" && c.Eval.Suites[i].NSamples == 0 {
			c.Eval.Suites[i].NSamples = c.Eval.Suites[i].K
		}
	}
}
