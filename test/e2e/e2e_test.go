//go:build e2e

// Package e2e runs the Slice-1 acceptance flow end-to-end, in-process, on CPU with no
// network: compose a code-gen agent -> run it sandboxed -> eval it -> see the trace ->
// produce an evidence bundle -> verify it offline.
package e2e

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/core"
	"github.com/faraday-stack/faraday/internal/evidence"
)

const cfgYAML = `
version: faraday/v1
name: e2e
models:
  coder: {source: qwen2.5-coder-32b, backend: mock}
agents:
  fixer: {model: coder, tools: [code_exec], max_repair_iterations: 3}
sandbox: {runtime: local, network: none, pool_size: 1}
eval:
  suites:
    - name: evalplus-mini
      kind: code_passk
      dataset: bundled:humanevalplus-mini
      k: 1
      n_samples: 1
      threshold: {pass_rate: 0.8}
  ci: {fail_on: [threshold]}
evidence:
  control_frameworks: [nist-ai-rmf, nist-800-53]
  sign: {identity: "E2E"}
`

func TestSliceOneEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required for the sandboxed flow")
	}
	cfg, err := config.Parse([]byte(cfgYAML))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	eng, err := core.New(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	defer eng.Close()

	// The air-gap guarantee must hold.
	if !eng.NetworkIsolated() {
		t.Fatal("sandbox is not network-isolated")
	}

	// 1) Run the code-gen agent in the sandbox.
	res, err := eng.RunAgent(context.Background(), "fixer", "Implement a function add(a, b) that returns their sum")
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	if res.Status != "solved" {
		t.Fatalf("agent status = %s, want solved", res.Status)
	}

	// 2) Eval it offline; the gate must pass.
	out, err := eng.EvalRun(context.Background(), "evalplus-mini")
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !out.Passed || out.GateFailed {
		t.Fatalf("eval failed: %+v", out)
	}
	if out.Metrics["pass_rate"] < 0.8 {
		t.Fatalf("pass_rate %.2f below threshold", out.Metrics["pass_rate"])
	}

	// 3) A correlated trace must exist.
	traces, err := eng.Store().ListTraces(10)
	if err != nil || len(traces) == 0 {
		t.Fatalf("expected traces, got %d (err=%v)", len(traces), err)
	}

	// 4) Produce the evidence bundle and 5) verify it offline.
	bundlePath := filepath.Join(dir, "evidence.tar.zst")
	m, err := eng.BuildEvidence(bundlePath, filepath.Join(dir, "k.key"), "E2E")
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if len(m.Files) == 0 {
		t.Fatal("evidence bundle has no files")
	}
	vr, err := evidence.Verify(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.OK {
		t.Fatalf("evidence verify failed: %+v", vr.Problems)
	}

	// 6) The audit chain underpinning the evidence must be intact.
	if broken, _ := eng.Store().VerifyAuditChain(); broken != 0 {
		t.Fatalf("audit chain broken at seq %d", broken)
	}
}
