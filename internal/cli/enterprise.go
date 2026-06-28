package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/faraday-stack/faraday/internal/license"
)

// newLicenseCmd inspects offline license files (works in any build, no daemon needed).
func newLicenseCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "license", Short: "Inspect offline license files"}
	cmd.AddCommand(&cobra.Command{
		Use:   "show <file>",
		Short: "Show and verify a license file offline (no network)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := license.LoadFile(args[0])
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(l.Claims, "", "  ")
			fmt.Println(string(b))
			if verr := l.Verify(license.ProductionPublicKey); verr != nil {
				fmt.Printf("signature: INVALID (%v)\n", verr)
				return fail("license not valid against the embedded production key")
			}
			fmt.Println("signature: VALID (verified offline against the embedded key)")
			return nil
		},
	})
	return cmd
}

// newTeamCmd exposes the paid team-observability tier (enterprise build + license).
func newTeamCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "team", Short: "Team-scale observability (paid tier)"}
	cmd.AddCommand(&cobra.Command{
		Use:   "summary",
		Short: "Cross-user/run eval aggregation (requires the enterprise build + a team license)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var out map[string]any
			if err := client().Get(context.Background(), "/api/team/summary", &out); err != nil {
				return err
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	})
	return cmd
}
