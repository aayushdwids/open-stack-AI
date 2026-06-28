// Command faraday is the single static binary: both the CLI and (via `faraday daemon`)
// the engine daemon. It installs from a USB stick with zero control-plane dependencies.
package main

import (
	"fmt"
	"os"

	"github.com/faraday-stack/faraday/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
