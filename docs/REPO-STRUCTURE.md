# Faraday Repo / Module Structure

Two artifacts: **one static Go binary** (control plane) and **a set of Python ML services**
(behind the API, shipped as signed OCI images). The repo is a Go module at the root with a
`pyservices/` subtree for the Python plane and an `enterprise/` submodule for the paid tier.

```
faraday/
├── go.mod                      # module github.com/faraday-stack/faraday  (Apache-2.0)
├── go.sum
├── LICENSE                     # Apache-2.0 (engine)
├── README.md
├── Makefile                    # build (static), test, bundle, gen-schema, proto
│
├── cmd/
│   └── faraday/
│       └── main.go             # single entrypoint: dispatches CLI vs `daemon`
│
├── internal/                   # the engine (Apache-2.0); not importable externally
│   ├── cli/                    # cobra commands: up, run, eval, trace, evidence, bundle,
│   │                           #   config, daemon, host, model, agent, version
│   ├── daemon/                 # faradayd: lifecycle, mux (Connect + /v1 + OTLP + UI)
│   ├── api/                    # Connect RPC service impl (control plane)
│   │   └── gen/                # generated protobuf/Connect code
│   ├── config/                # faraday.yaml load, defaults, JSON-Schema (gen from structs)
│   ├── store/                  # modernc sqlite: state, spans, eval runs, audit log, registry
│   │   └── migrations/
│   ├── telemetry/             # OTel SDK setup, embedded collector, gen_ai.* span helpers,
│   │                           #   span sink → sqlite + file exporter, semconv adapter
│   ├── inference/             # OpenAI /v1 proxy + native router (weighted failover)
│   │   ├── proxy/             # reverse-proxy /v1/* to backends, emit route+inference spans
│   │   ├── router/            # logical model → backend, cooldown, retries
│   │   └── backend/           # vllm, llamacpp, mock (deterministic, GPU-free for CI)
│   ├── registry/             # model registry: OCI artifacts, cosign verify, digest pin
│   ├── runtime/              # THE CENTERPIECE: air-gap-native agent runtime (control side)
│   │   ├── sandbox/           # gvisor(runsc) | firecracker | libkrun drivers
│   │   ├── pool/              # warm pool, lifecycle FSM, cgroups v2 limits
│   │   ├── netpolicy/         # network-deny enforcement (--network=none + nftables DROP)
│   │   └── agentctl/          # drives the Python reasoning loop, correlates spans
│   ├── mcp/                   # MCP broker (mark3labs/mcp-go): host stdio servers, per-call
│   │                           #   telemetry hooks, built-in tools (code_exec, fs_*, run_tests)
│   ├── eval/                 # eval orchestration: code_passk, deterministic, judge,
│   │                           #   regression, thresholds, CI exit codes
│   ├── evidence/             # evidence bundle: eval report, model/data card, SBOM/AIBOM,
│   │                           #   hash-chained audit log, control mappings, sign + verify
│   ├── provision/            # target providers: local, cloud (skypilot/dstack-style), airgap
│   ├── bundle/               # air-gap delivery: create/install, OCI layout, lockfile,
│   │                           #   chunked weights, cosign offline verify, registry seed
│   ├── license/              # ed25519 offline license verify (free vs enterprise gate iface)
│   └── version/
│
├── api/
│   └── proto/                 # .proto definitions for the Connect control API
│
├── ui/                        # web UI (later); built to ui/dist, //go:embed into binary
│   └── dist/                  # embedded; empty placeholder until Slice 6
│
├── pyservices/                # the ML plane (Python, behind the API; Apache-2.0)
│   ├── inference/             # vLLM / llama.cpp launchers exposing OpenAI /v1
│   │   ├── pyproject.toml
│   │   └── faraday_inference/
│   ├── agent/                 # plan→generate→execute→observe→repair loop (LangGraph-style)
│   │   ├── pyproject.toml
│   │   └── faraday_agent/     # talks to /v1, calls tools only via MCP broker
│   ├── eval/                  # judges + harness adapters (EvalPlus/inspect_ai/ragas-style)
│   │   └── faraday_eval/
│   ├── train/                 # fine-tune / RL jobs (TRL/NeMo/verl class) — Slice 5
│   └── common/                # OTLP→localhost telemetry, /v1 client, MCP client
│
├── sandbox-images/            # Dockerfiles for pre-baked sandbox rootfs (python/go/js)
│   └── python/Dockerfile
│
├── enterprise/                # PAID tier — separate module, //go:build enterprise
│   ├── go.mod                 # github.com/faraday-stack/faraday-enterprise (commercial)
│   ├── LICENSE                # proprietary commercial license
│   └── teamobs/               # cross-user aggregation, RBAC, audit, alerting, dashboards
│
├── docs/
│   ├── ARCHITECTURE.md
│   ├── CONFIG.md
│   ├── ROADMAP.md
│   ├── REPO-STRUCTURE.md
│   └── LICENSING.md
│
├── openspec/                  # specs of record (changes + capability specs)
│   ├── config.yaml
│   ├── changes/
│   └── specs/
│
├── examples/                  # faraday.yaml examples: minimal, code-gen, cloud, airgap
├── test/
│   ├── e2e/                   # the Slice-1 six-command flow, GPU-free (mock backend)
│   └── fixtures/              # bundled mini eval datasets (humanevalplus-mini, etc.)
└── .github/
    └── workflows/             # static build matrix, go test, py tests, e2e, DCO check
```

## Module / build conventions

- **One Go module** at the root (`github.com/faraday-stack/faraday`), engine code under
  `internal/` so it isn't importable as a library surface we must support.
- **`enterprise/` is a separate Go module** with its own commercial `LICENSE`, wired into
  the engine through interfaces the engine defines, compiled only with `-tags enterprise`.
  Default `make build` links **no** enterprise code.
- **Static build:** `CGO_ENABLED=0 go build -tags "netgo osusergo"`. An optional
  `-tags "analytics"` (cgo, DuckDB) enterprise build is the only non-static path.
- **Python services** are independent `pyproject.toml` packages, each turned into a signed
  OCI image; the Go plane never imports Python — it speaks `/v1`, MCP, and OTLP to them.
- **Proto → Go:** `buf generate` into `internal/api/gen/`. **Structs → JSON Schema:**
  `make gen-schema` writes the embedded config schema.
- **Generated/embedded:** `ui/dist` and the JSON Schema are `//go:embed`-ed so the binary
  is self-contained.
