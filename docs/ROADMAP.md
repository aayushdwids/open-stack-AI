# Faraday Roadmap — Vertical Slices

**Built through, not in parallel.** One thin, complete vertical slice runs end-to-end
before the next is widened. One working path beats ten half-built subsystems. Each slice
below is independently runnable.

The full skeleton (all capability specs) exists from the start; **Slice 1 is detailed and
built deep first** because the eval/observability layer is the sharpest wedge (cloud eval
tools assume egress; ours doesn't), is upstream of everything, and produces the artifact
regulated buyers pay for.

---

## Slice 1 — Eval / Observability / Assurance (THE FIRST, deep)

**Proves the whole spine in miniature:** local model → compose one code-gen agent → run
it sandboxed (air-gap-capable) → eval it → see the trace → produce the evidence bundle.

End-to-end deliverable:
```
faraday daemon  →  faraday run agent fixer "<task>"  →  faraday eval run evalplus-mini
                →  faraday trace last  →  faraday evidence bundle
```

Contains the thin-but-complete version of every core subsystem:
- Static Go binary: `faraday` CLI + `faraday daemon` over a Unix socket (Connect RPC).
- Config loader + JSON-Schema validation (`faraday.yaml`, defaults, `config validate`).
- Pure-Go SQLite store (state, spans, eval runs, audit log).
- **Telemetry from day one:** OTel SDK + embedded collector → file + SQLite; `gen_ai.*`
  spans for route/inference/tool/agent/eval.
- OpenAI-compatible `/v1` proxy + native router, forwarding to a Python inference worker
  (vLLM if GPU, llama.cpp otherwise; a deterministic mock backend for CI with no GPU).
- **Air-gap-native agent runtime (thin):** gVisor sandbox pool with `--network=none`,
  `code_exec` MCP tool, the plan→generate→execute→observe→repair loop (Python) for the
  code-gen agent.
- **Eval runner:** `code_passk` (sandboxed unit-test execution), `deterministic` checks,
  `judge` (LLM-as-judge against a local model); thresholds + CI exit codes.
- `faraday trace` — query/show spans from the store.
- **Evidence bundle:** signed, reproducible eval report + model card + SBOM/AIBOM +
  hash-chained audit log + control mappings; offline-verifiable.

**Acceptance:** the six-command flow above runs on a CPU-only laptop with the network
physically off (mock or llama.cpp backend), emits a complete trace, gates on a threshold,
and produces a bundle that `faraday evidence verify --offline` accepts.

---

## Slice 2 — Compose: routing + MCP tools + orchestration (widen)

Widen COMPOSE into the real thing:
- Full router: weighted failover, cooldowns, multi-model, embeddings endpoint.
- MCP broker hardening: external stdio/HTTP MCP servers, per-session tool scoping,
  per-call telemetry via hooks.
- More orchestration patterns beyond plan-execute-repair; multi-agent handoff.
- Native tools: `fs_read/fs_write/run_tests`, plus a registry for custom MCP servers.

**Acceptance:** route across two models with fallback, attach an external MCP tool, run a
multi-step agent, all spans correlated under one trace.

---

## Slice 3 — TRY: cloud provisioning (widen)

The cloud half of the spine:
- `target.kind: cloud` provider: SkyPilot/dstack-style GPU provisioning with
  auto-failover, signed-bundle pull, vLLM launch, idle teardown.
- Prove the spine: the **same** `faraday.yaml` runs `--target cloud` and `--target local`
  with identical `models`/`agents`/`eval` blocks.

**Acceptance:** one config, two targets, identical agent + eval behavior; cloud run tears
down cleanly.

---

## Slice 4 — OWN + AIR-GAP: the offline spine + bundle delivery (widen; bundle held loosely)

The differentiating spine and the supply chain:
- `target.kind: airgap` static local-node provider (verify signed bundle → launch
  identically).
- `faraday bundle create` (online) → USB → `faraday bundle install` (offline): OCI-layout
  bundle, `faraday.lock.json` digest pinning, chunked weights, cosign offline verify,
  grype CVE gate, local registry seeding, `faraday host preflight`/`install-runtime`.
- gVisor/Firecracker high-isolation runtime class; full network-deny hardening (nftables).

**Acceptance:** build a bundle online, carry it to a network-disconnected machine, install,
and run the identical Slice-1 flow with zero internet. Held loosely — don't over-invest;
it's plumbing, not identity.

---

## Slice 5 — TRAIN: fine-tune / RL on a private corpus (widen, optional)

- `faraday train` over a Python training service (TRL/NeMo/verl class) behind the API.
- Produce a new signed model bundle consumable by the same registry/routing.
- Training telemetry into the same span store; training data card into the evidence
  bundle.

**Acceptance:** fine-tune the default code model on a small private corpus offline,
register the result, route an agent to it, eval the delta.

---

## Slice 6 — Web UI (widen)

- The local web UI as another API client, served from `embed.FS` on the daemon mux.
- Trace explorer, eval dashboards (single-user views over the span store), config editor.

**Acceptance:** open the UI from the daemon with no internet; view a trace and an eval run.

---

## Slice 7 — Paid tier: team-scale observability (widen, commercial)

Everything `//go:build enterprise`, sold as a self-hosted license that cannot phone home:
- Cross-user aggregation, retention/history, RBAC, audit trails, alerting, collaborative
  eval dashboards.
- Optional cgo `analytics` build (DuckDB/chdb) for team-scale span volumes.
- Ed25519 offline license validation.

**Acceptance:** with an enterprise license file (no network), aggregate traces across
users, enforce RBAC, and gate on team-wide eval dashboards — all inside the air-gap.

---

## Build order summary

```
1. Eval/Observability  ← deep first, end-to-end
2. Compose (routing/MCP/orchestration)
3. TRY (cloud provisioning)        ┐ prove the spine: one config, both targets
4. OWN+AIR-GAP (+ bundle)          ┘
5. TRAIN (optional)
6. Web UI
7. Paid tier (enterprise)
```

Sequencing rationale: Slice 1 is the wedge and is upstream of all monitoring. Slices 3+4
together prove the spine (the product). Slice 4's bundle pipeline is held loosely. The
centerpiece (air-gap-native agent runtime) is seeded in Slice 1 and hardened in Slices 2
and 4 — always led with "air-gap-native," never "agent framework."
