"""Inference launcher: stand up a real OpenAI-compatible server (vLLM on GPU, llama.cpp on
CPU) from local weights, with NO network access. The Go daemon proxies ``/v1`` to whatever
this launches and owns all telemetry; this process is a dumb worker.

Air-gap notes:
- Always pass an explicit local weights path; do not rely on HF cache/offline env semantics
  (some libs mishandle HF_HUB_OFFLINE). We still set the offline flags as belt-and-braces.
- vLLM and llama-server both expose ``/v1/chat/completions`` so the daemon speaks one
  protocol regardless of which is launched.

This module builds the command; it does not require torch/vLLM to be importable to be
imported itself (so it stays testable on a CPU-only box without the heavy stack).
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from typing import List


def build_command(backend: str, model_path: str, port: int, quantization: str = "",
                  max_model_len: int = 0, tensor_parallel: int = 0,
                  extra_args: List[str] | None = None) -> List[str]:
    """Build the launch command for the chosen backend."""
    extra = list(extra_args or [])
    if backend == "vllm":
        cmd = [sys.executable, "-m", "vllm.entrypoints.openai.api_server",
               "--model", model_path, "--port", str(port)]
        if quantization:
            cmd += ["--quantization", quantization]
        if max_model_len:
            cmd += ["--max-model-len", str(max_model_len)]
        if tensor_parallel:
            cmd += ["--tensor-parallel-size", str(tensor_parallel)]
        return cmd + extra
    if backend in ("llama_cpp", "llama-cpp", "llamacpp"):
        binary = shutil.which("llama-server") or "llama-server"
        # model_path is a .gguf file for llama.cpp.
        cmd = [binary, "-m", model_path, "--port", str(port), "--host", "127.0.0.1"]
        if max_model_len:
            cmd += ["-c", str(max_model_len)]
        return cmd + extra
    raise ValueError(f"unsupported backend {backend!r}")


def offline_env() -> dict:
    env = dict(os.environ)
    env["HF_HUB_OFFLINE"] = "1"
    env["TRANSFORMERS_OFFLINE"] = "1"
    env["HF_DATASETS_OFFLINE"] = "1"
    return env


def main(argv: List[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Faraday offline inference launcher")
    p.add_argument("--backend", required=True, choices=["vllm", "llama_cpp"])
    p.add_argument("--model-path", required=True, help="LOCAL path to weights (.safetensors dir or .gguf)")
    p.add_argument("--port", type=int, default=8001)
    p.add_argument("--quantization", default="")
    p.add_argument("--max-model-len", type=int, default=0)
    p.add_argument("--tensor-parallel", type=int, default=0)
    p.add_argument("--print-only", action="store_true", help="print the command and exit")
    args, extra = p.parse_known_args(argv)

    cmd = build_command(args.backend, args.model_path, args.port, args.quantization,
                        args.max_model_len, args.tensor_parallel, extra)
    if args.print_only:
        print(" ".join(cmd))
        return 0
    return subprocess.call(cmd, env=offline_env())


if __name__ == "__main__":
    raise SystemExit(main())
