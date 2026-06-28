"""Tiny stdlib-only HTTP helper that speaks to the daemon over either a Unix socket or a
local TCP address. No third-party dependencies, so it installs and runs air-gapped."""

from __future__ import annotations

import http.client
import json
import socket
from typing import Any, Optional


class _UnixHTTPConnection(http.client.HTTPConnection):
    """HTTPConnection over a Unix domain socket."""

    def __init__(self, socket_path: str, timeout: float = 600.0) -> None:
        super().__init__("localhost", timeout=timeout)
        self._socket_path = socket_path

    def connect(self) -> None:  # noqa: D401
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.settimeout(self.timeout)
        s.connect(self._socket_path)
        self.sock = s


class Transport:
    """Local transport to the daemon.

    Provide exactly one of ``socket_path`` (Unix socket) or ``base_url``
    (e.g. ``http://127.0.0.1:8080``).
    """

    def __init__(self, socket_path: Optional[str] = None, base_url: Optional[str] = None,
                 timeout: float = 600.0) -> None:
        if not socket_path and not base_url:
            raise ValueError("provide socket_path or base_url")
        self.socket_path = socket_path
        self.base_url = base_url.rstrip("/") if base_url else None
        self.timeout = timeout

    def _conn(self) -> http.client.HTTPConnection:
        if self.socket_path:
            return _UnixHTTPConnection(self.socket_path, self.timeout)
        host = self.base_url.split("://", 1)[1]
        return http.client.HTTPConnection(host, timeout=self.timeout)

    def request(self, method: str, path: str, body: Optional[Any] = None) -> Any:
        conn = self._conn()
        data = json.dumps(body).encode() if body is not None else None
        headers = {"Content-Type": "application/json"} if data else {}
        conn.request(method, path, body=data, headers=headers)
        resp = conn.getresponse()
        raw = resp.read()
        conn.close()
        if resp.status >= 400:
            raise RuntimeError(f"daemon {method} {path} -> {resp.status}: {raw.decode(errors='replace')}")
        if not raw:
            return None
        return json.loads(raw)
