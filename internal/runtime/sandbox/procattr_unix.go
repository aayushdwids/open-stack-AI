//go:build linux || darwin

package sandbox

import (
	"os/exec"
	"syscall"
)

// setProcAttrs puts the child in its own process group so a timeout kills the whole
// group, preventing orphaned runaway processes.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
