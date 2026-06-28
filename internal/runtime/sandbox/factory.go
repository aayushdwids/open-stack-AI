package sandbox

import "fmt"

// NewDriver selects a sandbox driver from config. "auto" prefers the real gVisor path and
// falls back to the local driver so the runtime stays usable everywhere. Every returned
// driver enforces network isolation at the OS level.
func NewDriver(cfg Config) (Driver, error) {
	switch cfg.Runtime {
	case "gvisor":
		return NewGvisorDriver(cfg)
	case "local":
		return NewLocalDriver(cfg)
	case "firecracker", "libkrun":
		return nil, fmt.Errorf("runtime %q (high-isolation class) is not implemented in this slice; use gvisor or local", cfg.Runtime)
	case "", "auto":
		if d, err := NewGvisorDriver(cfg); err == nil {
			return d, nil
		}
		return NewLocalDriver(cfg)
	default:
		return nil, fmt.Errorf("unknown sandbox runtime %q", cfg.Runtime)
	}
}
