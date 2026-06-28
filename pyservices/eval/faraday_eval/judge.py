"""Eval adapters for the ML plane: an offline LLM-as-judge that scores against a LOCAL model
via the daemon's ``/v1`` (the single most important air-gap eval requirement), plus the
pass@k estimator. Heavier external harnesses (EvalPlus, inspect_ai) attach here later; they
all support an OpenAI-compatible base_url, so this is the integration seam."""

from __future__ import annotations

import re
from math import comb
from typing import List, Optional

from faraday_common import OpenAIClient

_SCORE_RE = re.compile(r"[1-5]")


def pass_at_k(n: int, c: int, k: int) -> float:
    """Unbiased pass@k estimator: 1 - C(n-c, k) / C(n, k)."""
    if n - c < k:
        return 1.0
    return 1.0 - comb(n - c, k) / comb(n, k)


class LocalJudge:
    """LLM-as-judge using a local model. No external service is contacted."""

    def __init__(self, oai: OpenAIClient, judge_model: str) -> None:
        self._oai = oai
        self._model = judge_model

    def score(self, prompt: str, answer: str, rubric: str = "") -> int:
        msg = (
            "Score the following answer from 1 to 5 (integer only).\n"
            f"Rubric: {rubric}\nPrompt: {prompt}\nAnswer: {answer}\nScore:"
        )
        text = self._oai.chat_text(self._model, [{"role": "user", "content": msg}])
        m = _SCORE_RE.search(text)
        return int(m.group(0)) if m else 0

    def mean_score(self, cases: List[dict]) -> float:
        if not cases:
            return 0.0
        total = 0
        for c in cases:
            ans = self._oai.chat_text(self._model, [{"role": "user", "content": c["prompt"]}])
            total += self.score(c["prompt"], ans, c.get("rubric", ""))
        return total / len(cases)
