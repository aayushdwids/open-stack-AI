# Faraday Architecture

This document is the engineering source of truth for how Faraday is built. It is grounded
in current (2025–2026) tooling research; key sources are cited inline.

---

## 1. The one big idea

**One declarative config deploys identically across the air-gap boundary.** A
`faraday.yaml` that provisions a rented cloud GPU deploys the *identical* stack to an
offline owned machine with zero changes. Everything below exists to make that true.

The system is split into two planes:

```
┌──────────────────────────────────────────────────────────────────────┐
│  CONTROL PLANE — one static Go binary (faraday)                        │
│  • CLI + daemon (faradayd) in the same binary                          │
│  • Provisioning, routing, orchestration authority, telemetry,          │
│    container-pool lifecycle, MCP brokering, the air-gap guarantee,      │
│    eval orchestration, bundle create/install, license gating           │
│  • Pure Go, CGO_ENABLED=0, installs from USB with zero deps            │
└──────────────────────────────────────────────────────────────────────┘
                 │ OpenAI /v1 REST     │ MCP (stdio)      │ OTLP (localhost)
                 ▼                     ▼                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│  ML PLANE — Python behind the API, shipped as signed OCI images        │
│  • Inference workers (vLLM / llama.cpp)                                 │
│  • Agent reasoning loop (plan→generate→execute→observe→repair)         │
│  • Eval harnesses (pass@k, LLM-as-judge, deterministic checks)         │
│  • Training jobs (fine-tune / RL) — optional                           │
│  • Runs inside sandboxes / containers the control plane manages         │
└──────────────────────────────────────────────────────────────────────┘
```

The boundary between them is **always an API** — OpenAI-compatible REST for inference,
MCP for tools, OTLP for telemetry, Connect RPC for control. Nothing in the ML plane is
trusted with the air-gap guarantee; the Go control plane owns the security boundary.

---

## 2. The single static Go binary

`faraday` is both the CLI and the daemon (Tailscale's `tailscale`/`tailscaled` model,
k3s's "one binary many roles"). `faraday <cmd>` is a thin client; `faraday daemon`
(internally `faradayd`) is the long-lived server.

**Static build guarantee:**
```bash
CGO_ENABLED=0 go build -tags "netgo osusergo" -ldflags '-s -w' ./cmd/faraday
```
- `CGO_ENABLED=0` + `netgo`/`osusergo` → pure-Go net and user resolvers, no libc.
  Verify with `go tool nodeps` / `file` ("statically linked").
- **TLS roots:** an air-gapped `scratch` host has no `ca-certificates.crt`. Embed roots
  in the binary (`crypto/x509` fallback roots / `gwatts/rootcerts`). Never rely on
  `/etc/ssl`.
- **DNS:** the pure-Go resolver does not do mDNS/NSS. Air-gap uses static hosts/IPs;
  documented, not a bug.
- Cross-compile `linux/amd64` + `linux/arm64` from any host with no toolchain.

**Web UI** ships *inside* the binary via `//go:embed ui/dist` → `embed.FS`, served on the
same mux as the API (Flipt's approach). Zero extra files on the USB stick. The UI is just
another API client, served locally, so it works air-gapped.

---

## 3. The API surface

| Surface | Protocol | Transport | Consumers |
|---|---|---|---|
| **Control API** | Connect RPC (`connectrpc.com/connect`) | Unix socket `/run/faraday.sock` (local), HTTP/2 + mTLS (remote) | CLI, web UI, remote clients |
| **Inference API** | OpenAI-compatible REST | HTTP `/v1/chat/completions`, `/v1/completions`, `/v1/embeddings`, `/v1/models` | the ecosystem, agents, SDKs |
| **Telemetry ingest** | OTLP | gRPC `localhost:4317` / HTTP `:4318` | ML-plane services emitting spans |
| **Tools** | MCP (`2025-11-25`) | stdio (offline-native), streamable-HTTP | tool servers brokered by the daemon |

Connect RPC is chosen over raw gRPC because it serves the CLI (Unix socket, HTTP/1.1),
the future browser UI (plain fetch, no grpc-web proxy), and remote clients from **one**
protobuf service definition, and degrades to JSON-over-HTTP for `curl`-ability in
air-gap debugging.

Local socket auth = Unix socket file perms (`0660` + group). Remote = mTLS / signed
bearer tokens.

---

## 4. Storage — pure Go, single file

`modernc.org/sqlite` (pure Go, **no cgo** — preserves the static-binary guarantee) is the
**universal store**: daemon state, model/agent registry, license, telemetry spans
(denormalized table: `trace_id, span_id, parent_id, name, kind, start_unix_nano,
duration_ns, attrs JSON, resource JSON`), eval runs/results, audit log.

- SQLite handles millions of spans fine for single-node dashboards — the free,
  single-user tier needs nothing more.
- **Paid aggregation tier** (cross-user, billions of spans) may compile an optional
  **cgo analytics build** (`//go:build analytics` → DuckDB / `chdb` embedded ClickHouse)
  for columnar trace queries. The *default* offline binary stays 100% pure-Go.
- bbolt is avoided as primary (KV only, no SQL for dashboards).

---

## 5. Telemetry — the one stream everything reads

**From day one, every meaningful action emits an OpenTelemetry span.** This is cheap to
add now and a rewrite if retrofitted, so the emit exists before any UI.

- **SDK:** OpenTelemetry Go SDK `v1.38.x`. Opt into GenAI conventions with
  `OTEL_SEMCONV_STABILITY_OPT_IN=gen_ai_latest_experimental` (all `gen_ai.*` conventions
  are still *Development*/experimental as of mid-2026 — we pin our usage and own an
  adapter so upstream churn is contained).
- **In-process collector:** the daemon embeds a collector (OCB-built `otelcol`
  components) → **fileexporter** (OTLP JSON-lines on disk, the durable audit artifact) +
  a SQLite span sink (queryable for `faraday trace` and dashboards). **No internet
  egress, ever.**
- **ML-plane services** (Python) emit OTLP to `localhost` — the daemon is the only
  collector.

**Span model (`gen_ai.*` semantic conventions):**

| Action | Span name | Kind | Key attributes |
|---|---|---|---|
| Route decision | `route {requested_model}` | INTERNAL | requested model, chosen backend, fallback depth, reason |
| Inference | `chat {gen_ai.request.model}` | CLIENT | `gen_ai.provider.name`, `gen_ai.usage.input_tokens`/`output_tokens`, finish reasons, TTFT, backend, quant |
| Tool / MCP call | `execute_tool {gen_ai.tool.name}` | INTERNAL | `gen_ai.tool.call.id`, tool type, sandbox id, exit status |
| Retrieval (RAG) | `retrieval {data_source.id}` | CLIENT | data source, k, scores |
| Agent step | `invoke_agent {agent.name}` / `plan` | INTERNAL | `gen_ai.agent.id`, step index, repair iteration |
| Eval | `eval {suite}` | INTERNAL | suite, metric, score, pass/fail, dataset digest, seed |

Content capture (prompts/completions) is **off by default** (conventions say SHOULD NOT);
when enabled it goes to `gen_ai.input.messages` / `gen_ai.output.messages` as log records,
never silently. This matters for classified data.

---

## 6. Inference + routing

- **Engines:** default **vLLM** on GPU (PagedAttention, continuous batching, native
  OpenAI `/v1`); **llama.cpp / llama-server (GGUF)** as the CPU/no-GPU fallback. Both
  expose identical `/v1`, so the daemon speaks one protocol regardless. SGLang is an
  opt-in high-throughput backend for prefix-heavy agent workloads. (TGI is avoided —
  archived/maintenance-only as of 2026.)
- **The Go daemon is the OpenAI-compatible front + router** (a native, thin LiteLLM
  equivalent): logical-model → backend map, weighted routing, ordered fallback,
  cooldown, retries, timeouts. It reverse-proxies `/v1/chat/completions` to the Python
  worker over HTTP and emits a **route span** (parent) wrapping an **inference span**
  (child). Python/vLLM stays a dumb worker; the daemon owns the OpenAI surface and all
  telemetry.
- **Default code model:** `Qwen2.5-Coder-32B-Instruct` — AWQ-4bit at the 48 GB sweet
  spot, GGUF `Q4_K_M` for 24 GB / CPU. Tiers: `DeepSeek-Coder-V2-Lite` (small/consumer),
  `Codestral-22B` (fast FIM completion). Shipped default bundle = Qwen2.5-Coder-32B-AWQ.
- **Model registry & packaging:** weights are first-class **OCI artifacts** (ORAS) in a
  local registry, `safetensors`+AWQ for GPU and `GGUF` for CPU, **cosign-signed** and
  **digest-pinned**, verified on pull before serving. Always pass explicit local paths to
  the engine (don't rely on `HF_HUB_OFFLINE` cache semantics, which some libs mishandle).

---

## 7. The centerpiece — air-gap-native agent runtime

Always lead with *air-gap-native*, never "agent framework." The runtime is a **sandboxed
tool-use/code-execution engine with a hard network boundary.**

**Control plane (Go) owns the security boundary:**
- **Sandbox default: gVisor (`runsc`)** via OCI — user-space kernel, no KVM dependency,
  runs on laptops/VMs/bare-metal alike (portability is the air-gap requirement), strong
  track record for untrusted LLM code. **Opt-in high-isolation runtime class =
  Firecracker / libkrun** on KVM hosts, using snapshot/restore for 5–30 ms warm starts.
- **Container pool:** a warm pool of pre-baked `runsc` sandboxes (base toolchain already
  in the rootfs — there's no internet to `pip install` from anyway). Lifecycle FSM:
  acquire → assign → reset/destroy → async replenish.
- **The air-gap guarantee (defense in depth):** (1) gVisor `--network=none`; (2) host
  nftables default-DROP egress for the sandbox netns/cgroup; (3) no DNS, block the cloud
  metadata IP. The sandbox *cannot* reach the network even if the model asks it to.
- **Resource limits:** cgroups v2 per sandbox (CPU/mem/pids/IO).
- **MCP broker:** the daemon hosts MCP servers as **stdio subprocesses** (naturally
  offline) via `mark3labs/mcp-go`, registers tools, and emits a telemetry span per tool
  call (MCP hooks). The agent never touches a sandbox or tool except through this broker.

**Reasoning loop (Python) owns the thinking:**
- A LangGraph-style explicit graph: **plan → generate → execute → observe → repair**,
  with checkpointing and streaming, talking to the **local model** via the daemon's `/v1`
  surface and calling tools **only** through the Go MCP broker.
- We do **not** reimplement LangGraph in Go. Everything touching the sandbox/network is
  Go; the agent graph is Python. The MCP boundary is the contract between them.

For the first reference workload (**code-gen agent**), the loop is: plan the change →
generate code + tests → execute in the sandbox (no network) → observe test results →
repair on failure → return diff + trace.

---

## 8. Eval / observability / assurance (the paid layer, built first)

The first vertical slice. Cloud eval tools assume internet egress; ours doesn't. It is
upstream of everything and produces the artifact regulated buyers pay for.

- **Eval primitives:** golden datasets; deterministic checks (regex / JSON-schema /
  exact-match / assertions, no model needed); **LLM-as-judge against the local model**
  (every serious framework supports an OpenAI-compatible `base_url` — this is the single
  most important air-gap requirement and it's broadly met); **code-gen pass@k** via
  sandboxed unit-test execution. Regression tracking + CI gating (exit codes).
- **Code-gen eval** runs generated solutions against unit tests **inside the same sandbox
  pool** as the agent runtime: `pass@k = 1 − C(n−c,k)/C(n,k)`. Bundled mini-suites
  modeled on EvalPlus (HumanEval+/MBPP+) and Aider-polyglot, pre-cached so they run fully
  offline. Larger suites (SWE-bench Verified with `--namespace ''` local image builds)
  are opt-in.
- **Everything reads the telemetry stream** — evals, monitoring, and dashboards are all
  views over the one span store. Nothing is computed twice.
- **Evidence bundle** (`faraday evidence bundle`) — the accreditation artifact:
  - Signed, reproducible eval reports (pinned datasets, seeds, configs, container
    digests so results re-derive offline).
  - Model card + data card + **SLSA build provenance** + Sigstore/OMS model signatures.
  - **SBOM + AIBOM/ML-BOM** (CycloneDX) over software, weights, datasets, prompts.
  - A **hash-chained, tamper-evident audit log** of access/inference/changes.
  - NIST AI RMF (AI 100-1 / 600-1) + 800-53 control mappings; FedRAMP/eMASS-style
    package shape (SSP/SAR/POA&M equivalents) for IL4–IL6 enclaves.
  - The whole bundle is hash-chained and signed → integrity verifiable after offline
    transfer out of the enclave.

**Free vs paid line:** local single-user (run the engine, see *my own* traces, basic
metrics, run an eval, make a bundle) = **free, Apache-2.0**. Team-scale (cross-user
aggregation, retention/history, RBAC, audit trails, alerting, collaborative eval
dashboards) = **paid**, behind the `enterprise` build tag.

---

## 9. Provisioning — TRY (cloud) and OWN (air-gap), one config

Mirror SkyPilot/dstack: a declarative `target` block (GPU type/count, mem, disk) resolved
by a **pluggable provider backend**:
- **`target.kind: cloud`** → delegate to a SkyPilot/dstack-style provisioner: spin GPUs
  across clouds, pull the signed model bundle, launch vLLM, tear down after.
- **`target.kind: airgap`** (or `local`) → the same YAML resolves to a static local-node
  provider: verify the signed bundle from the internal registry, launch vLLM/llama.cpp
  **identically**. "Provision" is a no-op against pre-owned hardware.

The `models` / `agents` / `eval` / `telemetry` blocks are **byte-for-byte identical**
across cloud and air-gap. Only `target` changes. *That is the portability contract.*

---

## 10. Bundle delivery — CD for the air-gap (held loosely)

Real but plumbing, not identity; build it, don't let it consume time. The Zarf/Hauler
pattern generalized beyond Kubernetes:

```
ONLINE   faraday bundle create
         → resolve tags→digests into faraday.lock.json (reproducibility key)
         → oras/skopeo pull images + MCP servers → OCI layout
         → chunk + hash weights (BLAKE3, content-defined chunking) → OCI blobs
         → syft SBOM + in-toto SLSA provenance
         → cosign sign (bundle + attestations); export pubkey + trusted_root.json
         → pack → faraday-<ver>-<digest>.tar.zst

CARRY    USB stick into the enclave

OFFLINE  faraday bundle install
         → verify checksum + cosign verify --offline (bundled key + SET, no tlog)
         → verify-attestation; grype offline CVE gate
         → unpack OCI layout; seed local registry; place weights by digest
         → reconcile against faraday.lock.json → byte-identical environment
```

Signing is **offline-verifiable**: the cosign bundle carries the SET so tlog inclusion is
checkable without reaching Rekor; the public key rides on the stick.

---

## 11. Open-core mechanics

- **Engine = Apache-2.0** (`cmd/`, `engine/`, `pyservices/` reference images). Permissive,
  already relicensable by recipients.
- **Proprietary tier = separate Go module gated by `//go:build enterprise`**
  (`enterprise/`), wired into the OSS core through interfaces the core defines. Default
  builds link **no** proprietary code (Grafana's model). Clean, auditable Apache
  obligations.
- **Contributions = DCO** (`Signed-off-by`), not a CLA. The core is Apache (already
  relicensable), the paid tier is separately authored, so no CLA is needed to protect
  relicensing. *(Confirm dual-license mechanics with a lawyer before any relicense.)*
- **License gating, offline:** Ed25519-signed license files. A signed payload (tier,
  seats, expiry, features) is verified locally against a **public key hard-coded in the
  binary** — no network, tamper-evident, expiry embedded in signed claims. The only model
  that works air-gapped.

---

## 12. Where each language lives

| Concern | Language | Why |
|---|---|---|
| CLI, daemon, API, routing, container-pool lifecycle, network-deny, MCP broker, telemetry collector, storage, bundle, license | **Go** | Single static binary, owns the security boundary, zero-dep offline install |
| Inference serving | **Python** (vLLM / llama.cpp) | The ecosystem lives here; behind `/v1` |
| Agent reasoning loop | **Python** (LangGraph-style) | Don't reinvent the graph; behind MCP |
| Eval judges / harnesses | **Python** | Frameworks (Ragas/DeepEval/inspect_ai/EvalPlus) are Python; behind the eval API |
| Training (fine-tune / RL) | **Python** (TRL / NeMo / verl class) | Behind the training API; optional |

---

## 13. Flagged tensions (locked decisions that need honest scoping)

These are not blockers, but the messaging and bundle design must account for them:

1. **"Single static binary, zero deps" applies to the control plane, not the ML plane.**
   vLLM/llama.cpp/torch/CUDA and the eval/agent Python services are heavy and are **not**
   in the Go binary. The air-gap install is therefore **two-part**: (a) the trivial Go
   binary, (b) the ML runtime as **pre-built signed OCI images** in the bundle. This is
   by design and is exactly what the bundle slice delivers — but external messaging must
   say *"the engine installs with zero dependencies,"* not *"the whole platform is one
   file."*

2. **The agent runtime needs a host container runtime + gVisor.** The sandbox pool
   requires containerd/Docker + `runsc` present on the host. On an air-gapped box these
   are shipped *in the bundle* but are **host-level installs**, not carried inside the one
   binary. A `faraday host preflight` / `faraday host install-runtime` step provisions
   them from the bundle. Honest framing: *one binary for the engine; the sandbox layer is
   a one-time host bootstrap from the same signed bundle.*

3. **Paid cross-user aggregation may want the cgo analytics build.** Billions of spans
   across a team can outgrow pure-Go SQLite. The `analytics` build tag (DuckDB/chdb) is
   cgo and breaks the pure-static promise — acceptable because it's the **enterprise**
   bundle, still air-gap-installable, and the default free binary stays pure-Go.

4. **`gen_ai.*` conventions are experimental.** We adopt them now (correct long-term bet)
   but own a thin adapter layer so upstream renames don't ripple through the codebase.

None of these contradict a locked decision; they scope it. No locked decision is
technically unworkable as written.
