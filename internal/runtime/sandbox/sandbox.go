// Package sandbox provides the air-gap-native sandboxed execution drivers. The default
// real driver is gVisor (runsc) on Linux; a cross-platform "local" driver enforces
// network-deny via OS primitives (macOS sandbox-exec, Linux network namespaces) so the
// runtime stays usable on a dev laptop while keeping the air-gap guarantee testable.
package sandbox

import "context"

// ExecRequest describes one execution inside a sandbox.
type ExecRequest struct {
	// Files are written into the sandbox working directory before the command runs.
	Files map[string]string
	// Cmd is the command + args to run (e.g. ["python3", "main.py"]).
	Cmd []string
	// TimeoutSecs bounds wall-clock execution; <=0 uses the driver default.
	TimeoutSecs int
}

// ExecResult is the outcome of an execution.
type ExecResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	TimedOut   bool   `json:"timed_out"`
	DurationMs int64  `json:"duration_ms"`
}

// Sandbox is one isolated execution environment with no network egress.
type Sandbox interface {
	// ID is a stable identifier for this sandbox instance.
	ID() string
	// Exec runs a command inside the sandbox.
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
	// Reset clears mutable state so the sandbox can be reused from the pool.
	Reset() error
	// Destroy releases the sandbox's resources.
	Destroy() error
}

// Driver creates sandboxes.
type Driver interface {
	// Name returns the driver name (gvisor, local, ...).
	Name() string
	// NetworkIsolated reports whether this driver enforces no-egress at the OS level.
	NetworkIsolated() bool
	// New creates a ready sandbox.
	New(ctx context.Context) (Sandbox, error)
}

// Config controls driver construction.
type Config struct {
	// Runtime is the requested runtime: auto | gvisor | firecracker | libkrun | local.
	Runtime string
	// Network is none | host-deny.
	Network string
	// MemoryLimitMB is a soft memory cap (best-effort on the local driver).
	MemoryLimitMB int
	// DefaultTimeoutSecs is the default per-exec wall-clock bound.
	DefaultTimeoutSecs int
}
