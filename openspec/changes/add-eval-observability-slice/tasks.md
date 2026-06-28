# Tasks — Eval / Observability Vertical Slice

Status as built. The slice is end-to-end runnable on a CPU-only offline laptop: the GPU-free
`mock` backend and the network-isolated `local` sandbox driver keep every task verifiable.
Deviations from the original plan are recorded in section 10 (honest record).

## 1. Repo + build scaffolding

- [x] 1.1 Go module + `cmd/faraday` dispatching CLI vs `daemon`; cobra command tree.
- [x] 1.2 `Makefile` static build (`CGO_ENABLED=0 -tags "netgo osusergo"`); static linux build verified.
- [x] 1.3 CI workflow: static build matrix (amd64/arm64), `go vet`, `go test`, DCO check, e2e.
- [x] 1.4 `pyservices/{inference,agent,eval,common}` packages with `pyproject.toml` + shared client.

## 2. Config + store (capability: engine-daemon)

- [x] 2.1 Config Go structs for all top-level keys with documented defaults.
- [x] 2.2 YAML load + defaults + JSON-Schema (`config validate`, `config schema`).
- [x] 2.3 Tests: minimal config validates; same config resolves identically across targets.
- [x] 2.4 `internal/store` over modernc sqlite with migrations; state survives restart (tested).

## 3. Daemon + control API (capability: engine-daemon)

- [x] 3.1 `faraday daemon`: Unix socket (mode 0660), shared `http.Server` mux, graceful shutdown.
- [x] 3.2 Control API implemented as JSON-over-HTTP on the socket (see deviation D1: Connect RPC
  + protobuf codegen deferred; the JSON/HTTP surface is Connect-compatible in spirit).
- [x] 3.3 CLI talks to the daemon over the socket; `version` round-trips; clear error when down.

## 4. Telemetry pipeline (capability: telemetry)

- [x] 4.1 `gen_ai.*` span helpers behind an adapter (route/chat/execute_tool/invoke_agent/plan/eval).
- [x] 4.2 SQLite span sink + OTLP JSON-lines file sink; `capture_content` off by default.
- [x] 4.3 Localhost span-ingest endpoint (`/api/spans`) for ML-plane services.
- [x] 4.4 Zero egress by construction (no network exporter exists) + sandbox egress test.
- [x] 4.5 `faraday trace list` / `last` / `show` rendering the span tree with gen_ai.* attrs.

## 5. Inference serving + routing (capability: inference-serving)

- [x] 5.1 `Backend` interface + deterministic GPU-free `mock` backend.
- [x] 5.2 OpenAI-compatible handlers (`/v1/chat/completions`, `/v1/completions`, `/v1/models`).
- [x] 5.3 Native router (weighted failover, cooldown/timeout/retries); route+chat spans; fallback test.
- [x] 5.4 `llamacpp` + `vllm` HTTP-proxy backends; auto-select by hardware/endpoint.
- [x] 5.5 `pyservices/inference` launchers for vLLM/llama.cpp with explicit local-weights paths.

## 6. Agent runtime — the centerpiece (capability: agent-runtime)

- [x] 6.1 `Sandbox` interface + `local` driver (OS-enforced network deny; runs without gVisor).
- [x] 6.2 `gvisor` driver (runsc `--network=none`) implemented; host nftables netpolicy deferred (D2).
- [x] 6.3 Egress test: a sandboxed connect to a host-reachable listener fails (local driver; gVisor
  uses `--network=none`).
- [x] 6.4 Warm pool + lifecycle FSM + async replenish; resource-limit termination (timeout) tested.
- [x] 6.5 MCP broker with built-in tools (code_exec, run_tests) + per-call execute_tool span; external
  `mark3labs/mcp-go` stdio hosting deferred to Slice 2 (D3).
- [x] 6.6 `pyservices/agent` plan→generate→execute→observe→repair loop (real-model path).
- [x] 6.7 Go-native `agentctl` loop + `faraday run agent` (the default offline path; D4).
- [x] 6.8 Tests: successful generate-and-test; induced failure triggers a recorded repair; bounded at limit.

## 7. Eval runner (capability: eval)

- [x] 7.1 Bundled `humanevalplus-mini` fixtures (license-clean, hand-authored).
- [x] 7.2 `code_passk` suite: sandboxed test execution + pass@k; eval spans.
- [x] 7.3 `deterministic` + `judge` (LLM-as-judge via local model) suites.
- [x] 7.4 Reproducible runs (dataset digest/seed/model/metrics) recorded; regression comparison
  implemented (`CheckRegression`); CLI gating on regression partial (threshold gating wired) (D5).
- [x] 7.5 Thresholds + `ci.fail_on`; `faraday eval run` non-zero exit on gated failure (both paths tested).

## 8. Evidence bundle (capability: evidence)

- [x] 8.1 Hash-chained audit log writer + tamper-detecting verifier.
- [x] 8.2 Evidence assembly → tar.zst (eval report, model/data card, SBOM/AIBOM, audit log,
  control mappings, manifest).
- [x] 8.3 Ed25519 signing + `faraday evidence verify --offline` (no network).
- [x] 8.4 Tests: valid bundle verifies; post-sign modification fails verification.

## 9. End-to-end + docs

- [x] 9.1 `examples/` configs + `test/e2e` running the six-command flow on CPU with network disabled.
- [x] 9.2 Acceptance verified: flow runs offline, emits a correlated trace, gates on a threshold,
  produces a bundle that `evidence verify --offline` accepts.
- [x] 9.3 README/docs updated; deviations recorded below.

## 10. Deviations from plan (honest record)

- **D1 — Connect RPC.** The control API ships as JSON-over-HTTP on the Unix socket rather than
  Connect RPC + protobuf codegen (no `buf` toolchain dependency in this cut). It is
  Connect-compatible in spirit (JSON/HTTP) and curl-able for air-gap debugging. Migrating to
  Connect is mechanical and non-breaking for the CLI.
- **D2 — Sandbox default + nftables.** On non-Linux dev hosts the default sandbox is the `local`
  driver (network deny via macOS `sandbox-exec` / Linux netns). The gVisor driver
  (`--network=none`) is implemented and auto-selected when docker+runsc are present; host-level
  nftables defense-in-depth is documented but deferred to the Slice-4 air-gap hardening.
- **D3 — MCP transport.** Built-in tools are hosted by an in-process MCP-shaped broker that emits
  execute_tool spans. Hosting external `mark3labs/mcp-go` stdio servers is Slice 2.
- **D4 — Agent loop location.** Both a Go-native loop (default, dependency-free, offline/CI) and a
  Python loop (`pyservices/agent`, real-model path) exist. The architecture's "Python owns the
  loop" holds for real models; the Go-native loop guarantees the platform runs with zero Python.
- **D5 — Regression gating.** Run-over-run regression comparison is implemented; wiring it into
  `ci.fail_on: [regression]` exit codes is partial (threshold gating is fully wired + tested).
- **D6 — OTel SDK.** Telemetry uses an OpenTelemetry-*shaped* tracer emitting the exact `gen_ai.*`
  attribute keys to local sinks, rather than the upstream OTel Go SDK + embedded collector. This
  keeps the binary lean and the stream zero-egress by construction; swapping in the SDK behind the
  same `telemetry` adapter is isolated to one package.
