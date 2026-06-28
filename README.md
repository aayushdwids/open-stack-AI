# Faraday

**The open stack for AI that can't touch the internet.**

Faraday takes a team from *"try a model on a rented GPU"* all the way to *"run agentic
AI air-gapped on hardware we own"* — with the **same workflow the whole way**. The same
declarative config that spins up a rented box deploys the identical stack to an offline
machine with zero changes. That continuity across the air-gap boundary is the product.

Everything serves **sovereignty**: full AI capability without surrendering control of
your data, your models, or your network.

> A Faraday cage blocks the signals. So does this.

---

## The lifecycle

| Stage | What it does | Faraday command |
|---|---|---|
| **TRY** | Provision/rent a GPU, sandbox, experiment | `faraday up --target cloud` |
| **COMPOSE** | Pick models, route between them, pair MCP tools, wire agents | `faraday run agent ...` |
| **TRAIN** | *(optional)* RL / fine-tune on a private corpus | `faraday train ...` |
| **OWN + AIR-GAP** | Run the **identical** stack fully offline on owned hardware | `faraday up --target airgap` |
| **OBSERVABILITY / EVAL** *(cross-cutting, paid tier)* | Team evals, monitoring, audit, accreditation evidence | `faraday eval ...`, `faraday trace ...` |

**The spine:** one `faraday.yaml`. Cloud-rented or air-gapped-owned — same file, same
schema. Most tools break at the air-gap boundary. This one must not.

---

## Architecture in one breath

- **One static Go binary** (`faraday`) is both the CLI and the daemon. `CGO_ENABLED=0`,
  pure-Go, embedded CA roots, embedded web UI. Installs from a USB stick with zero
  host dependencies on the control plane.
- **Python lives behind the API** for everything ML-touching (inference, training, the
  agent reasoning loop, eval harnesses), shipped as **signed OCI images** in the bundle.
- **OpenAI-compatible** wherever inference is touched — free integration with the
  ecosystem.
- **Declarative config is the contract.** Almost nothing required; everything optional
  with good defaults. A newcomer writes 3 lines; a power user writes 300; same schema.
- **Telemetry from day one.** Every route decision, tool call, inference, retrieval,
  agent step, and eval emits an OpenTelemetry span (`gen_ai.*` conventions). All
  observability, eval, and monitoring read this single stream. Zero egress.
- **The centerpiece is the air-gap-native agent runtime:** a gVisor sandbox pool with a
  hard network kill, an MCP tool broker, and an orchestration loop — sandboxed
  code-execution that runs with no internet at all.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/CONFIG.md](docs/CONFIG.md),
[docs/ROADMAP.md](docs/ROADMAP.md), and [docs/REPO-STRUCTURE.md](docs/REPO-STRUCTURE.md).

---

## 30-second tour (the first vertical slice)

```bash
# 1. Start the daemon (no internet required)
faraday daemon &

# 2. Three-line config: a local code-gen model + agent
cat > faraday.yaml <<'EOF'
version: faraday/v1
models:
  coder: { source: qwen2.5-coder-32b }     # served via vLLM or llama.cpp
agents:
  fixer: { model: coder, tools: [code_exec] }
EOF

# 3. Run the code-gen agent — it writes code and executes it in a sealed sandbox
faraday run agent fixer "Write a function that parses an ISO-8601 duration, with tests"

# 4. Evaluate it offline against a golden set (pass@k via sandboxed unit-test execution)
faraday eval run evalplus-mini --agent fixer

# 5. See exactly what happened — every span, every tool call, every token
faraday trace last

# 6. Produce the air-gapped evidence bundle accreditation buyers pay for
faraday evidence bundle --out ./evidence.tar.zst
```

The same `faraday.yaml`, pointed at `--target cloud`, would have provisioned a rented
GPU and done the identical thing.

---

## Status

Built in the open as a deep-learning exercise — **built through, not in parallel**: one
thin complete vertical slice end-to-end first (the eval/observability slice), then each
stage widened. See the [roadmap](docs/ROADMAP.md).

**Working today** (verified offline, CPU-only):

- The single static Go binary (`make build`; static Linux build confirmed). `faraday daemon`
  + CLI over a Unix socket.
- Config load + JSON-Schema validation; the same config resolves identically across
  `local`/`cloud`/`airgap` targets.
- Zero-egress `gen_ai.*` telemetry → SQLite + OTLP JSON-lines; `faraday trace`.
- OpenAI `/v1` proxy + weighted-failover router; deterministic mock backend (GPU-free) +
  vLLM/llama.cpp proxy backends.
- The air-gap-native agent runtime: network-isolated sandbox pool (gVisor when present,
  else an OS-enforced local driver), MCP tool broker, plan→generate→execute→observe→repair
  loop. **A sandboxed connect to a host-reachable listener is proven to fail.**
- Offline eval (`code_passk` pass@k, `deterministic`, `judge`) with thresholds + CI exit codes.
- Signed, offline-verifiable evidence bundle (eval report, cards, SBOM/AIBOM, hash-chained
  audit log, NIST mappings).
- Air-gap delivery: `faraday bundle create|verify|install` (digest-pinned lockfile, Ed25519,
  tamper-refused); `faraday up` (local/airgap/cloud-plan).
- Open-core: Ed25519 offline license + build-tag-gated enterprise `team summary`.
- Python ML-plane services (inference launcher, agent loop, eval, common) behind the API.

The full six-command quick-start below runs end-to-end on a network-disconnected laptop
(`go test -tags e2e ./test/e2e/...`). See the slice-1 task record + deviations in
[openspec/changes/add-eval-observability-slice/tasks.md](openspec/changes/add-eval-observability-slice/tasks.md).

## License

Open-core. **Apache-2.0** on the engine (this repo's `engine/` and `cmd/`). The
team-scale observability/eval tier (`enterprise/`, build-tag gated) is under a separate
**commercial license** — sold as a self-hosted license + support that runs *inside* the
air-gap and **cannot phone home**. Contributions are accepted under the **DCO**
(`Signed-off-by`). See [LICENSE](LICENSE) and [docs/LICENSING.md](docs/LICENSING.md).
