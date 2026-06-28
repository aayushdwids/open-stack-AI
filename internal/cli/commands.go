package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/core"
	"github.com/faraday-stack/faraday/internal/daemon"
	"github.com/faraday-stack/faraday/internal/evidence"
	"github.com/faraday-stack/faraday/internal/store"
	"github.com/faraday-stack/faraday/internal/version"
)

// ---- daemon ----

func newDaemonCmd() *cobra.Command {
	var tcpAddr string
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the faraday engine daemon (no internet required)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigOrDefault()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			eng, err := core.New(ctx, cfg, flagDataDir)
			if err != nil {
				return err
			}
			defer eng.Close()

			d := daemon.New(eng, daemon.Options{SocketPath: flagSocket, TCPAddr: tcpAddr})
			fmt.Fprintf(os.Stderr, "faraday daemon listening on %s (sandbox=%s, network_isolated=%v)\n",
				flagSocket, eng.SandboxDriverName(), eng.NetworkIsolated())
			if tcpAddr != "" {
				fmt.Fprintf(os.Stderr, "OpenAI-compatible /v1 on http://%s/v1\n", tcpAddr)
			}
			return d.Serve(ctx)
		},
	}
	cmd.Flags().StringVar(&tcpAddr, "listen", "", "optional localhost addr to serve /v1 (e.g. 127.0.0.1:8080)")
	return cmd
}

// ---- version ----

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version (queries the daemon if running)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var info version.Info
			if err := client().Get(ctx, "/api/version", &info); err == nil {
				fmt.Printf("faraday %s (daemon) commit=%s %s/%s\n", info.Version, info.Commit, info.OS, info.Arch)
				return nil
			}
			local := version.Get()
			fmt.Printf("faraday %s (cli) commit=%s %s/%s\n", local.Version, local.Commit, local.OS, local.Arch)
			return nil
		},
	}
}

// ---- config ----

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Work with faraday.yaml"}
	cmd.AddCommand(&cobra.Command{
		Use:   "validate [file]",
		Short: "Validate a config file (located errors)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath()
			if len(args) > 0 {
				path = args[0]
			}
			if path == "" {
				return fail("no config file; pass a path or create ./faraday.yaml")
			}
			if _, err := config.Load(path); err != nil {
				return err
			}
			fmt.Printf("%s is valid\n", path)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for faraday.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := config.Schema()
			if err != nil {
				return err
			}
			var pretty map[string]any
			_ = json.Unmarshal(b, &pretty)
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	})
	return cmd
}

// ---- run agent ----

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run", Short: "Run agents and workloads"}
	agentCmd := &cobra.Command{
		Use:   "agent <name> <task...>",
		Short: "Run a code-gen agent in the air-gap-native sandbox",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			task := strings.Join(args[1:], " ")
			var res struct {
				TraceID    string `json:"trace_id"`
				Status     string `json:"status"`
				Code       string `json:"code"`
				Output     string `json:"output"`
				Iterations int    `json:"iterations"`
				Passed     bool   `json:"passed"`
			}
			if err := client().Post(context.Background(), "/api/run/agent", map[string]any{"agent": name, "task": task}, &res); err != nil {
				return err
			}
			fmt.Printf("status:     %s\n", res.Status)
			fmt.Printf("iterations: %d\n", res.Iterations)
			fmt.Printf("trace:      %s\n", res.TraceID)
			fmt.Printf("\n--- code ---\n%s\n", res.Code)
			if strings.TrimSpace(res.Output) != "" {
				fmt.Printf("--- output ---\n%s\n", res.Output)
			}
			if res.Status == "error" {
				return fail("agent run errored")
			}
			return nil
		},
	}
	cmd.AddCommand(agentCmd)
	return cmd
}

// ---- eval ----

func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "eval", Short: "Run offline eval suites"}
	cmd.AddCommand(&cobra.Command{
		Use:   "run <suite>",
		Short: "Run an eval suite (non-zero exit on a gated failure)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out core.EvalOutcome
			if err := client().Post(context.Background(), "/api/eval/run", map[string]any{"suite": args[0]}, &out); err != nil {
				return err
			}
			fmt.Printf("suite:   %s (%s)\n", out.Suite, out.Kind)
			fmt.Printf("cases:   %d\n", out.Cases)
			fmt.Printf("dataset: %s\n", short(out.DatasetDigest))
			keys := make([]string, 0, len(out.Metrics))
			for k := range out.Metrics {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("metric:  %s = %.3f\n", k, out.Metrics[k])
			}
			fmt.Printf("passed:  %v\n", out.Passed)
			if out.GateFailed {
				for _, r := range out.GateReasons {
					fmt.Printf("gate:    FAIL %s\n", r)
				}
				return fail("eval gate failed")
			}
			return nil
		},
	})
	return cmd
}

// ---- trace ----

func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "trace", Short: "Inspect recorded telemetry traces"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List recent traces",
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Traces []store.TraceSummary `json:"traces"`
			}
			if err := client().Get(context.Background(), "/api/trace/list", &resp); err != nil {
				return err
			}
			if len(resp.Traces) == 0 {
				fmt.Println("no traces recorded")
				return nil
			}
			for _, t := range resp.Traces {
				fmt.Printf("%s  %-28s  %d spans  %.1fms\n", short(t.TraceID), t.RootName, t.SpanCount, float64(t.DurationNs)/1e6)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "last",
		Short: "Show the most recent trace as a span tree",
		RunE:  func(cmd *cobra.Command, args []string) error { return showTrace("/api/trace/last") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <id>",
		Short: "Show a trace as a span tree",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return showTrace("/api/trace/show?id=" + args[0]) },
	})
	return cmd
}

func showTrace(path string) error {
	var resp struct {
		TraceID string        `json:"trace_id"`
		Spans   []store.Span  `json:"spans"`
	}
	if err := client().Get(context.Background(), path, &resp); err != nil {
		return err
	}
	fmt.Printf("trace %s — %d spans\n", resp.TraceID, len(resp.Spans))
	renderSpanTree(resp.Spans)
	return nil
}

func renderSpanTree(spans []store.Span) {
	children := map[string][]store.Span{}
	var roots []store.Span
	for _, s := range spans {
		if s.ParentID == "" {
			roots = append(roots, s)
		} else {
			children[s.ParentID] = append(children[s.ParentID], s)
		}
	}
	var walk func(s store.Span, depth int)
	walk = func(s store.Span, depth int) {
		indent := strings.Repeat("  ", depth)
		attrs := ""
		for _, k := range []string{"gen_ai.request.model", "gen_ai.tool.name", "faraday.route.backend", "faraday.exec.exit_status", "faraday.eval.suite", "gen_ai.usage.output_tokens"} {
			if v, ok := s.Attrs[k]; ok {
				attrs += fmt.Sprintf(" %s=%v", shortKey(k), v)
			}
		}
		fmt.Printf("%s• %-24s [%s] %.1fms%s\n", indent, s.Name, s.Kind, float64(s.DurationNs)/1e6, attrs)
		kids := children[s.SpanID]
		sort.Slice(kids, func(i, j int) bool { return kids[i].StartUnixNano < kids[j].StartUnixNano })
		for _, c := range kids {
			walk(c, depth+1)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].StartUnixNano < roots[j].StartUnixNano })
	for _, r := range roots {
		walk(r, 0)
	}
}

// ---- evidence ----

func newEvidenceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "evidence", Short: "Assemble and verify the air-gapped evidence bundle"}

	var out, identity, key string
	bundle := &cobra.Command{
		Use:   "bundle",
		Short: "Assemble a signed evidence bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Out      string             `json:"out"`
				Manifest *evidence.Manifest `json:"manifest"`
			}
			if err := client().Post(context.Background(), "/api/evidence/bundle", map[string]any{"out": out, "identity": identity, "key": key}, &resp); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", resp.Out)
			if resp.Manifest != nil {
				fmt.Printf("identity:    %s\n", resp.Manifest.Identity)
				fmt.Printf("files:       %d\n", len(resp.Manifest.Files))
				fmt.Printf("root digest: %s\n", short(resp.Manifest.RootDigest))
				fmt.Printf("signed:      ed25519 (verify with: faraday evidence verify %s)\n", resp.Out)
			}
			return nil
		},
	}
	bundle.Flags().StringVar(&out, "out", "evidence.tar.zst", "output bundle path")
	bundle.Flags().StringVar(&identity, "identity", "", "signing identity recorded in the manifest")
	bundle.Flags().StringVar(&key, "key", "", "ed25519 private key path (generated if absent)")
	cmd.AddCommand(bundle)

	var offline bool
	verify := &cobra.Command{
		Use:   "verify <bundle>",
		Short: "Verify a bundle entirely offline (no daemon, no network)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := evidence.Verify(args[0])
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(b))
			if !res.OK {
				return fail("verification FAILED")
			}
			fmt.Println("OK — signature valid, all digests match (offline)")
			return nil
		},
	}
	verify.Flags().BoolVar(&offline, "offline", true, "verify without any network access (always on)")
	cmd.AddCommand(verify)
	return cmd
}

// ---- helpers ----

func loadConfigOrDefault() (*config.Config, error) {
	path := resolveConfigPath()
	if path == "" {
		return config.Parse([]byte("version: " + config.CurrentVersion + "\n"))
	}
	return config.Load(path)
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func shortKey(k string) string {
	parts := strings.Split(k, ".")
	return parts[len(parts)-1]
}
