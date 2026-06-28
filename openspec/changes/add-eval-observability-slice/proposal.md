## Why

Cloud eval/observability tools assume internet egress; air-gapped and regulated teams
cannot use them. The eval/observability/assurance layer is Faraday's sharpest wedge, sits
upstream of everything else, and produces the artifact accreditation buyers pay for. We
build it first as one thin, complete vertical slice that exercises every core subsystem
end-to-end: local model → compose a code-gen agent → run it sandboxed (air-gap-capable) →
eval it → see the trace → produce an offline-verifiable evidence bundle.

## What Changes

- Introduce the single static Go binary `faraday` (CLI + `faraday daemon`) talking over a
  Unix socket via Connect RPC, with a pure-Go SQLite store and `faraday.yaml` loading +
  JSON-Schema validation.
- **Telemetry from day one**: every route decision, inference, tool call, agent step, and
  eval emits an OpenTelemetry `gen_ai.*` span into an embedded collector → SQLite + file
  sink, with **zero network egress**. `faraday trace` queries this one stream.
- An OpenAI-compatible `/v1` proxy + native router forwarding to a Python inference worker
  (vLLM/llama.cpp), with a deterministic **mock backend** so the whole slice runs GPU-free
  and offline in CI.
- The **air-gap-native agent runtime** (thin, first form): a gVisor sandbox pool with the
  network killed (`--network=none`), a `code_exec` MCP tool, and the
  plan→generate→execute→observe→repair loop for the code-gen reference agent.
- An offline **eval runner**: `code_passk` (sandboxed unit-test execution), `deterministic`
  checks, and `judge` (LLM-as-judge against a local model), with thresholds and CI exit
  codes.
- An offline-verifiable **evidence bundle**: signed, reproducible eval report + model/data
  card + SBOM/AIBOM + hash-chained audit log + NIST control mappings.

## Capabilities

### New Capabilities
- `engine-daemon`: the single static binary, CLI/daemon split, Connect control API, config
  loading + JSON-Schema validation, and the pure-Go SQLite store.
- `telemetry`: `gen_ai.*` span emission, embedded zero-egress collector, SQLite + file
  sinks, and trace query/retrieval.
- `inference-serving`: OpenAI-compatible `/v1` surface, native router with fallback, and
  pluggable backends (vLLM, llama.cpp, deterministic mock) with per-route/per-inference
  spans.
- `agent-runtime`: the air-gap-native sandboxed code-execution runtime — gVisor pool,
  enforced network-deny, MCP `code_exec` tool, and the code-gen reasoning loop.
- `eval`: offline eval suites (`code_passk`, `deterministic`, `judge`), regression
  comparison, thresholds, and CI gating.
- `evidence`: assembly, signing, and offline verification of the air-gapped evidence
  bundle (eval report, cards, SBOM/AIBOM, hash-chained audit log, control mappings).

### Modified Capabilities
- (none — greenfield)

## Impact

- New Go module `github.com/faraday-stack/faraday` (engine under `internal/`, entrypoint
  `cmd/faraday`), and Python ML services under `pyservices/` (inference, agent, eval).
- New dependencies: OpenTelemetry Go SDK + collector components, `modernc.org/sqlite`,
  Connect RPC, `mark3labs/mcp-go`, gVisor (`runsc`) on the host, cosign/syft for evidence.
- Establishes the telemetry span contract, the `faraday.yaml` schema, and the air-gap
  guarantee enforced at the sandbox boundary — all of which later slices build on.
- Flagged scoping (not blockers): the "zero-dep single binary" promise covers the control
  plane; the ML plane ships as signed OCI images, and the sandbox layer requires a
  host container runtime + gVisor bootstrapped from the bundle.
