// Package cli implements the faraday command tree. The CLI is a thin client to the
// daemon over a Unix socket; only `version`, `config`, and `evidence verify` work
// without a running daemon.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/faraday-stack/faraday/internal/api"
	"github.com/faraday-stack/faraday/internal/daemon"
)

var (
	flagSocket  string
	flagConfig  string
	flagDataDir string
)

// Execute runs the root command.
func Execute() error {
	root := &cobra.Command{
		Use:           "faraday",
		Short:         "Faraday — the open stack for AI that can't touch the internet",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flagSocket, "socket", envOr("FARADAY_SOCKET", daemon.DefaultSocket), "daemon control socket path")
	root.PersistentFlags().StringVar(&flagConfig, "config", envOr("FARADAY_CONFIG", ""), "path to faraday.yaml (default: ./faraday.yaml)")
	root.PersistentFlags().StringVar(&flagDataDir, "data-dir", envOr("FARADAY_DATA_DIR", defaultDataDir()), "data directory")

	root.AddCommand(
		newDaemonCmd(),
		newVersionCmd(),
		newConfigCmd(),
		newRunCmd(),
		newEvalCmd(),
		newTraceCmd(),
		newEvidenceCmd(),
	)
	return root.Execute()
}

func client() *api.Client { return api.NewClient(flagSocket) }

func resolveConfigPath() string {
	if flagConfig != "" {
		return flagConfig
	}
	if _, err := os.Stat("faraday.yaml"); err == nil {
		return "faraday.yaml"
	}
	return ""
}

func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".faraday")
	}
	return ".faraday"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fail(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}
