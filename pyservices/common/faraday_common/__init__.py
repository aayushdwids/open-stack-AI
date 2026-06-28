"""Shared client surface for Faraday ML-plane services.

The ML plane (inference, agent reasoning, eval, training) lives BEHIND the daemon's APIs.
It never owns the air-gap guarantee; it talks to the Go daemon over three local channels:

- OpenAI-compatible ``/v1`` for inference (:class:`OpenAIClient`)
- the daemon control API for tool calls and span ingest (:class:`DaemonClient`,
  :class:`SpanEmitter`)

All transport is local (a Unix socket or ``127.0.0.1``); nothing here reaches the
internet.
"""

from .client import DaemonClient, OpenAIClient
from .telemetry import SpanEmitter

__all__ = ["DaemonClient", "OpenAIClient", "SpanEmitter"]
