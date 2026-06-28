package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/faraday-stack/faraday/internal/bundle"
	"github.com/faraday-stack/faraday/internal/provision"
)

// newBundleCmd implements the air-gap delivery spine (create online, install offline).
func newBundleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bundle", Short: "Air-gap delivery: create, verify, install signed bundles"}

	var out, key, identity string
	var srcArgs []string
	create := &cobra.Command{
		Use:   "create",
		Short: "Pack artifacts into a signed, digest-pinned bundle (online)",
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := parseSources(srcArgs)
			if err != nil {
				return err
			}
			// Always include the active config when present.
			if cp := resolveConfigPath(); cp != "" {
				sources = append(sources, bundle.Source{Name: "faraday.yaml", Path: cp, Kind: "config"})
			}
			if len(sources) == 0 {
				return fail("no artifacts; pass --add name=path (and/or have a ./faraday.yaml)")
			}
			lock, err := bundle.Create(out, key, identity, sources)
			if err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", out)
			fmt.Printf("artifacts: %d (digest-pinned in faraday.lock.json)\n", len(lock.Artifacts))
			fmt.Printf("signed:    ed25519 — install offline with: faraday bundle install --bundle %s --dest <dir>\n", out)
			return nil
		},
	}
	create.Flags().StringVar(&out, "out", "faraday-bundle.tar.zst", "output bundle path")
	create.Flags().StringVar(&key, "key", "", "ed25519 signing key path (generated if absent)")
	create.Flags().StringVar(&identity, "identity", "", "identity recorded in the lockfile")
	create.Flags().StringArrayVar(&srcArgs, "add", nil, "artifact to include as name=path[:kind] (repeatable)")
	cmd.AddCommand(create)

	verify := &cobra.Command{
		Use:   "verify <bundle>",
		Short: "Verify a bundle entirely offline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := bundle.Verify(args[0])
			if err != nil {
				return err
			}
			printJSON(res)
			if !res.OK {
				return fail("verification FAILED")
			}
			return nil
		},
	}
	cmd.AddCommand(verify)

	var bundlePath, dest string
	install := &cobra.Command{
		Use:   "install",
		Short: "Verify offline and install a bundle (offline)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if bundlePath == "" || dest == "" {
				return fail("--bundle and --dest are required")
			}
			rep, err := bundle.Install(bundlePath, dest)
			if err != nil {
				return err
			}
			fmt.Printf("verified: %v\n", rep.Verified)
			fmt.Printf("installed %d artifacts into %s\n", len(rep.Installed), rep.DestDir)
			return nil
		},
	}
	install.Flags().StringVar(&bundlePath, "bundle", "", "bundle path")
	install.Flags().StringVar(&dest, "dest", "", "install destination directory")
	cmd.AddCommand(install)
	return cmd
}

// newUpCmd resolves the target and brings it up (the spine: same config, any target).
func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Resolve the target (local|cloud|airgap) and bring the stack up",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigOrDefault()
			if err != nil {
				return err
			}
			p, err := provision.Select(cfg)
			if err != nil {
				return err
			}
			st, err := p.Up(context.Background(), cfg)
			if err != nil {
				return err
			}
			fmt.Printf("target:  %s\n", st.Target)
			fmt.Printf("ready:   %v\n", st.Ready)
			fmt.Printf("message: %s\n", st.Message)
			if len(st.Details) > 0 {
				fmt.Println("details:")
				printJSON(st.Details)
			}
			return nil
		},
	}
}

func parseSources(args []string) ([]bundle.Source, error) {
	var out []bundle.Source
	for _, a := range args {
		name, rest, ok := cut(a, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --add %q (want name=path[:kind])", a)
		}
		path, kind, hasKind := cut(rest, ":")
		if !hasKind {
			kind = "other"
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("artifact %q: %w", path, err)
		}
		out = append(out, bundle.Source{Name: name, Path: path, Kind: kind})
	}
	return out, nil
}

func cut(s, sep string) (string, string, bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}
