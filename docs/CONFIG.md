# Faraday Config Schema (`faraday.yaml`)

**The contract.** The same file deploys to a rented cloud GPU or an offline owned
machine. Only the `target` block changes between them; `models` / `agents` / `tools` /
`eval` / `telemetry` are byte-for-byte identical.

**The rule:** almost nothing is required; everything is optional with a good default. A
newcomer writes 3 lines; a power user writes 300. Same file, same schema (SkyPilot's
principle). The schema is generated from Go structs and validated against an embedded
JSON Schema; `version:` is the only field that should ever be required, and even it
defaults to the current version when omitted.

---

## Top-level keys

| Key | Purpose | Required |
|---|---|---|
| `version` | Schema version (`faraday/v1`). Config outlives binaries; lets us migrate readers before writers. | recommended (defaults to current) |
| `name` | Human label for the stack. | no |
| `target` | Where it runs: `local` \| `cloud` \| `airgap`, plus resources. **The only block that differs cloud↔air-gap.** | no (defaults to `local`) |
| `models` | Logical model name → source / quantization / serving backend / routing. | no (defaults to bundled code model) |
| `agents` | Agent definitions: model ref, system prompt, tools, orchestration, limits. | no |
| `tools` | MCP servers + native tools available to agents. | no (defaults to `code_exec`) |
| `sandbox` | Sandbox runtime class, pool size, resource + network policy. | no (secure defaults) |
| `eval` | Suites, datasets, metrics, judges, thresholds, CI gating. | no |
| `telemetry` | Span sink, sampling, content capture, retention. | no (on by default, content capture off) |
| `evidence` | Evidence-bundle contents + signing identity. | no |
| `bundle` | Air-gap bundle create/install settings (registry, signing key). | no |
| `secrets` | Secret references (kept separate from `env`). | no |
| `env` | Plain environment values. | no |

---

## Minimal example (the "3 lines")

This is a complete, valid config. Everything else is defaulted: `target: local`, the
bundled `qwen2.5-coder` model, a `code_exec` tool, a gVisor sandbox with the network
killed, telemetry on, content capture off.

```yaml
version: faraday/v1
agents:
  fixer: { model: coder, tools: [code_exec] }
```

Even shorter — run an ad-hoc agent with zero config:

```yaml
agents:
  fixer: {}        # inherits the default code model + code_exec tool
```

---

## Typical example (the everyday case)

```yaml
version: faraday/v1
name: code-assistant

target:
  kind: local                     # local | cloud | airgap

models:
  coder:
    source: qwen2.5-coder-32b     # resolves to a signed bundle in the registry
    quantization: awq             # awq | gptq | gguf:q4_k_m | fp16 (auto by VRAM if omitted)
  judge:
    source: qwen2.5-coder-7b      # a small local model for LLM-as-judge

agents:
  fixer:
    model: coder
    system: "You are a meticulous Go/Python engineer. Always write tests."
    tools: [code_exec, fs_read]
    max_steps: 12

tools:
  mcp:
    - name: code_exec             # built-in sandboxed executor
    - name: fs_read

eval:
  suites:
    - name: evalplus-mini
      kind: code_passk
      dataset: bundled:humanevalplus-mini
      k: 1
      judge: judge
      threshold: { pass_rate: 0.8 }   # CI gate: fail the run below this

telemetry:
  capture_content: false          # never log prompts/completions by default
```

---

## Full example (the power user's "300 lines", abridged but exhaustive in shape)

Every overridable knob, to show the schema's ceiling.

```yaml
version: faraday/v1
name: sovereign-code-platform

target:
  kind: cloud                     # provision a rented GPU for TRY; flip to airgap unchanged
  infra: aws/us-east-1            # SkyPilot-style; optional, auto-failover if omitted
  resources:
    accelerators: A100:2          # or L40S:1, H100:1, or "none" for CPU
    cpus: 16
    memory: 64GB
    disk: 200GB
  spot: true                      # cloud only; ignored for airgap
  idle_teardown_minutes: 30       # cloud only

models:
  coder:
    source: qwen2.5-coder-32b
    backend: vllm                 # vllm | llama_cpp | sglang (auto by hardware if omitted)
    quantization: awq
    max_model_len: 32768
    gpu_memory_utilization: 0.90
    tensor_parallel_size: 2
    serve:
      port: 8001
      extra_args: ["--enable-prefix-caching"]
  coder_cpu:
    source: qwen2.5-coder-7b
    backend: llama_cpp
    quantization: gguf:q4_k_m
  judge:
    source: qwen2.5-coder-7b

routing:                          # the LiteLLM-equivalent native router
  coder:                          # logical name agents ask for
    strategy: weighted_failover
    backends:
      - { model: coder, weight: 1 }
      - { model: coder_cpu, weight: 0 }   # fallback only
    cooldown_seconds: 30
    timeout_seconds: 120
    retries: 2

agents:
  fixer:
    model: coder
    system: "You are a meticulous engineer. Plan, write code with tests, run them, repair."
    orchestration: plan_execute_repair   # the built-in code-gen loop
    tools: [code_exec, fs_read, fs_write, run_tests]
    max_steps: 16
    max_repair_iterations: 4
    timeout_seconds: 600
    temperature: 0.2

tools:
  mcp:
    - name: code_exec             # built-in sandboxed executor (Python/Go/JS)
      runtime: gvisor             # inherits sandbox defaults below
    - name: fs_read
      root: ./workspace
    - name: fs_write
      root: ./workspace
    - name: run_tests
      command: ["pytest", "-q"]
    - name: custom-tool           # any external MCP server (stdio)
      transport: stdio
      command: ["/opt/tools/my-mcp-server"]

sandbox:
  runtime: gvisor                 # gvisor | firecracker | libkrun
  pool_size: 4                    # warm pre-baked sandboxes
  image: faraday/sandbox-python:latest   # signed OCI image from the bundle
  network: none                   # none | host-deny (enforced; "none" is the air-gap default)
  limits:
    cpus: 2
    memory: 4GB
    pids: 256
    timeout_seconds: 120
  high_isolation: false           # true → firecracker/libkrun microVM class

eval:
  suites:
    - name: evalplus-mini
      kind: code_passk            # code_passk | judge | deterministic | regression
      dataset: bundled:humanevalplus-mini
      k: 1
      n_samples: 5
      sandbox: { network: none }
      threshold: { pass_rate: 0.80 }
    - name: style-judge
      kind: judge
      dataset: ./evals/style_cases.yaml
      judge: judge                # LLM-as-judge against the LOCAL judge model
      rubric: ./evals/style_rubric.md
      threshold: { mean_score: 4.0 }
    - name: must-compile
      kind: deterministic
      dataset: ./evals/compile_cases.yaml
      checks: [exit_zero, no_stderr]
  regression:
    baseline: last_passing        # compare run-over-run, gate on regressions
  ci:
    fail_on: [threshold, regression]   # non-zero exit for CI gating

telemetry:
  enabled: true
  sink: sqlite                    # sqlite (default) | file (otlp jsonl) | both
  file_path: ./.faraday/traces.jsonl
  sampling: always_on             # always_on | ratio:0.1
  capture_content: false          # prompts/completions stay off unless explicitly enabled
  retention_days: 90              # free tier single-user; team retention is paid
  semconv: gen_ai_latest_experimental

evidence:
  include:
    - eval_reports
    - model_card
    - data_card
    - sbom
    - aibom
    - slsa_provenance
    - audit_log
    - control_mappings            # NIST AI RMF + 800-53
  control_frameworks: [nist-ai-rmf, nist-800-53, fedramp-il5]
  sign:
    key: ./keys/evidence.key      # ed25519 / cosign; verified offline
    identity: "ACME Corp Accreditation"

bundle:
  registry: localhost:5000        # internal OCI registry seeded offline
  sign_key: ./keys/bundle.key
  trusted_root: ./keys/trusted_root.json
  weights_chunking: blake3        # content-defined chunking for resumable USB transfer

secrets:
  hf_token: ${env:HF_TOKEN}       # only needed online during bundle create; never at run time

env:
  TZ: UTC
```

---

## Design notes

- **`target` is the only cloud↔air-gap delta.** Take this exact file, change
  `target.kind: cloud` → `airgap`, and the deployment is identical. That is the spine.
- **Defaults are real defaults.** Omitting `sandbox` still gives you gVisor with the
  network killed. Omitting `telemetry` still records spans. Security and observability are
  never opt-in.
- **Validation:** the Go structs are the source of truth; an embedded JSON Schema is
  generated from them and used to validate `faraday.yaml` with precise error locations.
  `faraday config validate` runs it; `faraday config schema` prints it.
- **Versioning:** `version: faraday/v1`. New schema versions add a reader migration so old
  configs keep working (Kubernetes `apiVersion` lesson).
