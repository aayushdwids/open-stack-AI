"""The plan->generate->execute->observe->repair reasoning loop, in Python (the alternate to
the Go-native loop, used with real models). It talks to the daemon's ``/v1`` for generation
and emits spans to the daemon. Tool execution is brokered by the Go daemon — this loop never
touches a sandbox or the network directly.

LangGraph-style structure kept dependency-light for the first cut: an explicit state machine
rather than a heavy framework, so it runs offline with only the stdlib + faraday_common.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Callable, List, Optional

from faraday_common import OpenAIClient, SpanEmitter

_CODE_RE = re.compile(r"```(?:python|py)?\s*\n(.*?)```", re.DOTALL)


def extract_code(text: str) -> str:
    m = _CODE_RE.search(text)
    return (m.group(1).strip() + "\n") if m else text


@dataclass
class AgentConfig:
    model: str
    system: str = "You are a meticulous engineer. Return one fenced python code block."
    max_repair_iterations: int = 4


@dataclass
class RunResult:
    status: str = "unresolved"  # solved | unresolved | error
    code: str = ""
    output: str = ""
    iterations: int = 0
    passed: bool = False


# Verifier runs the candidate and returns (passed, combined_output). In production this is a
# thin wrapper that asks the daemon's broker to run code_exec/run_tests; injected so the loop
# stays testable without a live daemon.
Verifier = Callable[[str], "tuple[bool, str]"]


class AgentLoop:
    def __init__(self, oai: OpenAIClient, emitter: Optional[SpanEmitter] = None) -> None:
        self._oai = oai
        self._emitter = emitter

    def run(self, cfg: AgentConfig, task: str, verify: Verifier,
            trace_id: Optional[str] = None) -> RunResult:
        result = RunResult()
        last_failure = ""
        for it in range(cfg.max_repair_iterations + 1):
            user = task
            if it > 0 and last_failure:
                user = (f"{task}\n\nThe previous attempt failed when executed:\n"
                        f"{last_failure[:2000]}\n\nFix it and return the corrected code.")
            messages = [{"role": "system", "content": cfg.system},
                        {"role": "user", "content": user}]
            content = self._generate(messages, cfg.model, trace_id)
            code = extract_code(content)
            result.code = code
            result.iterations = it + 1

            passed, output = verify(code)
            result.output = output
            if passed:
                result.status = "solved"
                result.passed = True
                return result
            last_failure = output
        return result

    def _generate(self, messages: List[dict], model: str, trace_id: Optional[str]) -> str:
        if self._emitter is None:
            return self._oai.chat_text(model, messages)
        with self._emitter.span("chat " + model, kind="CLIENT", trace_id=trace_id,
                                attrs={"gen_ai.operation.name": "chat",
                                       "gen_ai.request.model": model}):
            return self._oai.chat_text(model, messages)
