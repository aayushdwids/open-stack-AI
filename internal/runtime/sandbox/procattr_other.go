//go:build !linux && !darwin

package sandbox

import "os/exec"

// setProcAttrs is a no-op on unsupported platforms (the local driver errors at
// construction for OSes without a network-isolation mechanism).
func setProcAttrs(cmd *exec.Cmd) {}
