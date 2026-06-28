# Faraday ML-plane services (Python, behind the API)

The ML plane lives **behind the Go daemon's APIs** and is shipped as signed OCI images in
the air-gap bundle. It never owns the air-gap guarantee — it talks to the daemon over three
local channels (OpenAI `/v1`, the control API for span ingest, and the MCP broker for
tools), all on a Unix socket or `127.0.0.1`. Nothing here reaches the internet.

| Package | Role |
|---|---|
| `common/` | `faraday_common` — stdlib-only clients: `OpenAIClient` (`/v1`), `DaemonClient`, `SpanEmitter` (gen_ai.* spans → daemon ingest). No third-party deps, so it installs air-gapped. |
| `inference/` | `faraday_inference` — launch a real OpenAI-compatible server (vLLM on GPU, llama.cpp on CPU) from **local** weights with offline env flags. The daemon proxies `/v1` to it. |
| `agent/` | `faraday_agent` — the plan→generate→execute→observe→repair loop for real models (the alternate to the Go-native loop). Tools are brokered by the daemon. |
| `eval/` | `faraday_eval` — offline LLM-as-judge against a local model + the pass@k estimator; the seam where EvalPlus/inspect_ai attach. |

## Why a Go-native path exists too

The Go daemon implements a self-contained agent loop and eval runner (used by the default
CPU-only / mock path and CI) so the platform is runnable with **zero** Python dependencies.
These Python services are the path for real model serving and reasoning on GPU hardware —
faithful to the architecture's "Python behind the API" split — and emit spans into the same
telemetry stream.

## Local dev

```bash
# stdlib-only pieces import and run with no install:
PYTHONPATH="common:agent:eval:inference" python3 -c "from faraday_eval import pass_at_k; print(pass_at_k(10,5,1))"

# print the inference launch command without running it:
python3 -m faraday_inference.server --backend vllm --model-path /models/qwen --print-only
```
