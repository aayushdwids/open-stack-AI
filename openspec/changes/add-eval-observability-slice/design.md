# Design — Eval / Observability Vertical Slice

This is the *how* for the first slice. It refines [docs/ARCHITECTURE.md](../../../docs/ARCHITECTURE.md)
to exactly what slice 1 builds. The guiding constraint: **the whole slice must run on a
CPU-only laptop with the network off**, so every subsystem ships a real but minimal form,
plus a deterministic mock where hardware would otherwise be required.

## Technical decisions

- **Language split:** Go control plane (`internal/`), Python ML plane (`pyservices/`).
  They meet at three APIs: OpenAI `/v1` (inference), MCP/stdio (tools), OTLP/localhost
  (telemetry). The Go plane never imports Python.
- **API protocol:** Connect RPC over a Unix socket for control; OpenAI REST for inference.
  One `http.Server` mux on the daemon serves Connect handlers, `/v1/*`, the OTLP receiver,
  and (later) the embedded UI.
- **Storage:** `modernc.org/sqlite` (pure Go). One DB file under `~/.faraday/faraday.db`.
  Tables: `spans`, `traces`, `eval_runs`, `eval_results`, `registry`, `audit_log`,
  `kv`. Migrations embedded and applied on startup.
- **Telemetry:** OpenTelemetry Go SDK (`v1.38.x`). A thin `telemetry` package wraps span
  creation with `gen_ai.*` helpers behind our own adapter (so experimental-convention
  churn is contained). Export path: SDK → in-process span processor → SQLite sink + file
  exporter (OTLP JSON-lines). A localhost OTLP receiver (gRPC `:4317`) accepts spans from
  Python services. No exporter ever targets a non-loopback address — enforced by config
  and an egress test.
- **Inference backends:** interface `Backend{ ChatCompletion(ctx, req) (resp, error) }`.
  Implementations: `mock` (deterministic, hashes the prompt → canned-but-structured code
  for the eval fixtures; GPU-free, the CI default), `llamacpp` (spawn/attach
  `llama-server`, proxy `/v1`), `vllm` (spawn/attach vLLM, proxy `/v1`). Router picks by
  logical model → weighted failover.
- **Sandbox:** `Sandbox` interface with a `gvisor` driver (runsc via OCI/containerd) and a
  `local` fallback driver (subprocess in a locked-down temp dir with seccomp/rlimits +
  network namespace deny) used when `runsc` is absent — so the slice still runs on a dev
  laptop, while the gVisor driver is the real air-gap path. **Network-deny is enforced**
  in both: gVisor `--network=none`; local driver via a new network namespace with no
  interfaces + rlimits + cgroup. An egress test asserts a socket connect fails inside.
- **Agent loop:** Python `faraday_agent` implementing plan→generate→execute→observe→repair
  with an explicit state machine (LangGraph-style but dependency-light for the first cut).
  It calls the daemon `/v1` for generation and the MCP broker for `code_exec`/`run_tests`.
  The Go `runtime/agentctl` launches it, passes config, and correlates its OTLP spans.
- **MCP broker:** `mark3labs/mcp-go`; built-in tools `code_exec`, `fs_read`, `fs_write`,
  `run_tests` exposed as stdio MCP servers in-process. Per-call hook emits the
  `execute_tool` span.
- **Eval:** Go `eval` orchestrator drives suites; `code_passk` executes candidate
  solutions + unit tests in the sandbox and computes `pass@k = 1 − C(n−c,k)/C(n,k)`;
  `deterministic` runs rule checks; `judge` calls the local judge model via `/v1`.
  Datasets: bundled `humanevalplus-mini` (a small, license-clean fixture set under
  `test/fixtures/`). Thresholds + regression compare to the last run; non-zero exit on
  gated failure.
- **Evidence:** Go `evidence` package assembles a `tar.zst` with `eval_report.json`,
  `model_card.md`, `data_card.md`, `sbom.cdx.json`, `aibom.cdx.json`, `audit_log.jsonl`
  (hash-chained), `control_mappings.json` (NIST AI RMF / 800-53 subset), and a
  `manifest.json` with per-file digests. Signing: Ed25519 over the manifest digest;
  `verify --offline` recomputes digests and checks the signature with the bundled public
  key. (cosign/SLSA integration is layered in Slice 4; Slice 1 uses the self-contained
  Ed25519 path so it works with zero external tools.)

## Risks / trade-offs

- **gVisor may be absent on dev machines.** Mitigation: the `local` sandbox driver with
  enforced network-namespace deny keeps the slice runnable; gVisor is auto-selected when
  present and is the documented air-gap path. The air-gap network guarantee is tested in
  both drivers.
- **Experimental `gen_ai.*` conventions.** Mitigation: the adapter layer isolates names so
  upstream renames are a one-file change.
- **Mock backend ≠ real model quality.** Acceptable: the mock exists to prove the
  end-to-end wiring and CI determinism, not model quality; llama.cpp/vLLM provide real
  inference when hardware is present.
- **Pure-Go SQLite write throughput.** Acceptable at single-node free-tier span volumes;
  the cgo analytics build is an enterprise concern (Slice 7), not this slice.

## Migration / rollout

Greenfield — no migration. The slice lands as the initial implementation; later slices
widen capabilities behind the same APIs without breaking the `/v1`, MCP, OTLP, or config
contracts established here.

## Open questions (deferred, non-blocking)

- Exact bundled-dataset license sourcing for `humanevalplus-mini` — start with a tiny
  hand-authored fixture set to avoid any redistribution question; swap to EvalPlus-derived
  data once licensing is confirmed.
- Whether to embed Jaeger-Badger for a richer trace UI later vs. the SQLite-backed CLI/UI
  views — defer to Slice 6.
