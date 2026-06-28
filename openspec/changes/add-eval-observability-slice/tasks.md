# Tasks — Eval / Observability Vertical Slice

Ordered by dependency. The slice must stay end-to-end runnable; the GPU-free `mock`
backend and the `local` sandbox driver keep every task verifiable on a CPU-only offline
laptop. Telemetry emit is non-optional in any task that adds a route/tool/inference/eval
path.

## 1. Repo + build scaffolding

- [ ] 1.1 Init Go module `github.com/faraday-stack/faraday`; create `cmd/faraday/main.go`
  dispatching CLI vs `daemon`; add cobra command tree (stubs): `daemon`, `version`,
  `config`, `run`, `eval`, `trace`, `evidence`.
- [ ] 1.2 Add `Makefile` targets: `build` (`CGO_ENABLED=0 -tags "netgo osusergo"`), `test`,
  `gen-schema`, `proto`, `e2e`; verify the binary is statically linked.
- [ ] 1.3 Add CI workflow: static build matrix (amd64/arm64), `go vet`, `go test`, DCO
  check, and the e2e flow on CPU with network disabled.
- [ ] 1.4 Scaffold `pyservices/{inference,agent,eval,common}` packages with
  `pyproject.toml` and a shared OTLP/`/v1`/MCP client in `common`.

## 2. Config + store (capability: engine-daemon)

- [ ] 2.1 Define config Go structs for all top-level keys (`version`, `target`, `models`,
  `routing`, `agents`, `tools`, `sandbox`, `eval`, `telemetry`, `evidence`, `bundle`,
  `secrets`, `env`) with documented defaults.
- [ ] 2.2 Implement YAML load + default application + `gen-schema` (structs → embedded JSON
  Schema); wire `faraday config validate` (located errors) and `faraday config schema`.
- [ ] 2.3 Add test: minimal 2-line config validates; same config resolves identically for
  `target.kind: local` vs `airgap` (only `target` differs).
- [ ] 2.4 Implement `internal/store` over `modernc.org/sqlite` with embedded migrations for
  `spans`, `traces`, `eval_runs`, `eval_results`, `registry`, `audit_log`, `kv`; auto-migrate
  on startup; test state survives restart.

## 3. Daemon + control API (capability: engine-daemon)

- [ ] 3.1 Implement `faraday daemon`: create Unix socket `/run/faraday.sock` (mode `0660`),
  start the shared `http.Server` mux; graceful shutdown.
- [ ] 3.2 Define the Connect control API proto (`api/proto`), generate into
  `internal/api/gen`, implement `Version`, `Health`, `ConfigValidate`, and stubs the CLI
  will call (`RunAgent`, `EvalRun`, `TraceList`, `TraceShow`, `EvidenceBundle`).
- [ ] 3.3 Wire the CLI to talk to the daemon over the socket; `faraday version` round-trips;
  clear non-zero error when the daemon is down.

## 4. Telemetry pipeline (capability: telemetry)

- [ ] 4.1 Set up the OTel Go SDK + tracer provider; implement the `telemetry` package with
  `gen_ai.*` span helpers behind an adapter (route, chat, execute_tool, retrieval,
  invoke_agent/plan, eval).
- [ ] 4.2 Implement the SQLite span sink (span processor → `spans`/`traces` tables) and the
  file exporter (OTLP JSON-lines); honor `telemetry.capture_content` (off by default).
- [ ] 4.3 Stand up the localhost OTLP receiver (`:4317`) so Python services' spans land in
  the store, correlated by trace id.
- [ ] 4.4 Add an egress test asserting no exporter connects to a non-loopback address.
- [ ] 4.5 Implement `faraday trace list` and `faraday trace last`/`show <id>` rendering the
  span tree with `gen_ai.*` attributes.

## 5. Inference serving + routing (capability: inference-serving)

- [ ] 5.1 Define the `Backend` interface and the `mock` backend (deterministic, GPU-free,
  network-free; structured code outputs for the eval fixtures).
- [ ] 5.2 Implement the OpenAI-compatible handlers (`/v1/chat/completions`,
  `/v1/completions`, `/v1/models`) on the daemon mux.
- [ ] 5.3 Implement the native router (logical model → weighted failover, cooldown, timeout,
  retries); emit the `route {model}` span wrapping the child `chat {model}` span with
  backend + token usage; test fallback increments fallback depth.
- [ ] 5.4 Implement the `llamacpp` backend (spawn/attach `llama-server`, proxy `/v1`) and
  the `vllm` backend (spawn/attach vLLM, proxy `/v1`); auto-select backend by hardware.
- [ ] 5.5 Implement `pyservices/inference` launchers for vLLM and llama.cpp exposing `/v1`;
  document explicit local-weights paths (no `HF_HUB_OFFLINE` reliance).

## 6. Agent runtime — the centerpiece (capability: agent-runtime)

- [ ] 6.1 Define the `Sandbox` interface; implement the `local` driver (new network
  namespace with no egress + seccomp + rlimits + temp-dir FS isolation) so the slice runs
  without gVisor.
- [ ] 6.2 Implement the `gvisor` driver (runsc via OCI/containerd, `--network=none`); add
  `internal/runtime/netpolicy` to enforce nftables default-DROP for the sandbox netns.
- [ ] 6.3 Add the egress test: a socket connect from inside a sandbox fails in BOTH drivers.
- [ ] 6.4 Implement the warm pool + lifecycle FSM (acquire→assign→reset/destroy→async
  replenish) with cgroups v2 / rlimit resource limits; test pool acquire + replenish +
  resource-limit termination.
- [ ] 6.5 Implement the MCP broker (`mark3labs/mcp-go`) hosting built-in tools `code_exec`,
  `fs_read`, `fs_write`, `run_tests` as stdio servers; per-call hook emits the
  `execute_tool` span; assert the agent cannot allocate a sandbox except via a tool.
- [ ] 6.6 Implement `pyservices/agent` plan→generate→execute→observe→repair loop calling
  daemon `/v1` for generation and the MCP broker for tools; bound by `max_steps` /
  `max_repair_iterations`.
- [ ] 6.7 Implement `internal/runtime/agentctl` to launch the Python loop, pass config, and
  correlate its OTLP spans under the `invoke_agent` root; wire `faraday run agent <name>
  "<task>"`.
- [ ] 6.8 Test: successful generate-and-test passes; an induced failure triggers a recorded
  repair iteration; the loop terminates at the limit with an unresolved status.

## 7. Eval runner (capability: eval)

- [ ] 7.1 Add bundled fixture dataset `test/fixtures/humanevalplus-mini` (small, license-clean,
  hand-authored: prompt + reference tests per case).
- [ ] 7.2 Implement the `code_passk` suite: generate candidate(s), execute candidate + unit
  tests in the sandbox, compute `pass@k`; emit `eval {suite}` spans.
- [ ] 7.3 Implement the `deterministic` suite (exit_zero / regex / json-schema / exact-match)
  and the `judge` suite (LLM-as-judge via local model over `/v1`).
- [ ] 7.4 Record reproducible runs (dataset digest, seed, model identity, metrics) into
  `eval_runs`/`eval_results`; implement run-over-run regression vs baseline.
- [ ] 7.5 Implement thresholds + `ci.fail_on`; `faraday eval run <suite>` exits non-zero on
  gated threshold/regression failure, zero on pass; test both exit paths.

## 8. Evidence bundle (capability: evidence)

- [ ] 8.1 Implement the hash-chained audit log writer (append access/inference/change
  events; chain digest) and a verifier that detects tampering.
- [ ] 8.2 Implement evidence assembly → `tar.zst`: `eval_report.json` (pinned datasets/
  seeds/model), `model_card.md`, `data_card.md`, `sbom.cdx.json`, `aibom.cdx.json`,
  `audit_log.jsonl`, `control_mappings.json` (NIST AI RMF / 800-53 subset), `manifest.json`
  (per-file digests).
- [ ] 8.3 Implement Ed25519 signing over the manifest digest and `faraday evidence verify
  --offline` (recompute digests + check signature with the bundled public key, no network).
- [ ] 8.4 Test: valid bundle verifies offline; any post-sign modification fails verification.

## 9. End-to-end + docs

- [ ] 9.1 Add `examples/` configs (minimal, code-gen, cloud, airgap) and `test/e2e` running
  the six-command flow (`daemon → run agent → eval run → trace last → evidence bundle →
  evidence verify`) on CPU with network disabled.
- [ ] 9.2 Verify acceptance: the e2e flow runs offline, emits a complete correlated trace,
  gates on a threshold, and produces a bundle that `evidence verify --offline` accepts.
- [ ] 9.3 Update `README` quick-start and `docs/` to match the shipped commands; record any
  deviations from this plan.
