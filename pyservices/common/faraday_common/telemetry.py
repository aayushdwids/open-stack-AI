"""Span emission for ML-plane services. Spans are POSTed to the daemon's localhost ingest
endpoint, which is the ONLY collector. Uses gen_ai.* semantic-convention attribute keys so
ML-plane spans correlate with the Go-plane stream. Zero egress: the daemon is local."""

from __future__ import annotations

import os
import secrets
import time
from contextlib import contextmanager
from typing import Any, Dict, Iterator, Optional

from .client import DaemonClient


class SpanEmitter:
    """Creates spans and ships them to the daemon. Use :meth:`span` as a context manager.

    Trace/span ids are propagated so a Python-side step nests under a Go-side ``invoke_agent``
    root when ``trace_id``/``parent_id`` are passed in.
    """

    def __init__(self, daemon: DaemonClient, resource: Optional[Dict[str, Any]] = None) -> None:
        self._daemon = daemon
        self._resource = resource or {"service.name": "faraday-mlplane"}

    @staticmethod
    def _id(n: int) -> str:
        return secrets.token_hex(n)

    @contextmanager
    def span(self, name: str, kind: str = "INTERNAL", trace_id: Optional[str] = None,
             parent_id: Optional[str] = None, attrs: Optional[Dict[str, Any]] = None) -> Iterator[Dict[str, Any]]:
        trace_id = trace_id or os.environ.get("FARADAY_TRACE_ID") or self._id(16)
        span_id = self._id(8)
        rec: Dict[str, Any] = {
            "trace_id": trace_id,
            "span_id": span_id,
            "parent_id": parent_id or os.environ.get("FARADAY_PARENT_ID", ""),
            "name": name,
            "kind": kind,
            "attrs": dict(attrs or {}),
            "resource": self._resource,
        }
        start = time.time_ns()
        try:
            yield rec
            rec.setdefault("status", "ok")
        except Exception as exc:  # noqa: BLE001
            rec["status"] = f"error: {exc}"
            raise
        finally:
            rec["start_unix_nano"] = start
            rec["duration_ns"] = time.time_ns() - start
            try:
                self._daemon.ingest_span(rec)
            except Exception:  # noqa: BLE001
                # Telemetry must never crash the workload; drop on failure.
                pass
