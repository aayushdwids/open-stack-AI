"""Clients the ML plane uses to reach the daemon: OpenAI ``/v1`` and the control API."""

from __future__ import annotations

from typing import Any, Dict, List, Optional

from ._http import Transport


class OpenAIClient:
    """Minimal OpenAI-compatible chat client pointed at the daemon's ``/v1`` surface."""

    def __init__(self, socket_path: Optional[str] = None, base_url: Optional[str] = None) -> None:
        self._t = Transport(socket_path=socket_path, base_url=base_url)

    def chat(self, model: str, messages: List[Dict[str, str]], temperature: float = 0.2,
             max_tokens: int = 0) -> Dict[str, Any]:
        body: Dict[str, Any] = {"model": model, "messages": messages, "temperature": temperature}
        if max_tokens:
            body["max_tokens"] = max_tokens
        return self._t.request("POST", "/v1/chat/completions", body)

    def chat_text(self, model: str, messages: List[Dict[str, str]], **kw: Any) -> str:
        resp = self.chat(model, messages, **kw)
        choices = resp.get("choices") or []
        if not choices:
            return ""
        return choices[0].get("message", {}).get("content", "")


class DaemonClient:
    """Control-API client (health, span ingest). Tool calls are brokered by the daemon's
    MCP broker; the ML plane never touches a sandbox directly."""

    def __init__(self, socket_path: Optional[str] = None, base_url: Optional[str] = None) -> None:
        self._t = Transport(socket_path=socket_path, base_url=base_url)

    def health(self) -> Dict[str, Any]:
        return self._t.request("GET", "/api/health")

    def ingest_span(self, span: Dict[str, Any]) -> None:
        self._t.request("POST", "/api/spans", span)
