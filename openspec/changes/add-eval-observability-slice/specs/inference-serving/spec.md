## ADDED Requirements

### Requirement: OpenAI-compatible inference surface
The daemon SHALL expose an OpenAI-compatible HTTP API including `/v1/chat/completions`,
`/v1/completions`, and `/v1/models`, accepting requests by logical model name. Responses
MUST conform to the OpenAI schema so existing OpenAI SDKs and tools work unchanged.

#### Scenario: Chat completion via the OpenAI API
- **WHEN** a client posts a valid `/v1/chat/completions` request naming a configured
  logical model
- **THEN** the daemon returns an OpenAI-shaped chat completion response

#### Scenario: Models listing
- **WHEN** a client requests `/v1/models`
- **THEN** the daemon returns the configured logical models in OpenAI list format

### Requirement: Native routing with fallback
The daemon SHALL resolve a logical model name to one or more backends using a configured
routing strategy (weighted with ordered failover), applying cooldown, timeout, and retry
policy. On backend failure it MUST fall back to the next eligible backend before erroring.

#### Scenario: Primary backend serves the request
- **WHEN** a request targets a logical model whose primary backend is healthy
- **THEN** the request is served by the primary backend

#### Scenario: Fallback on primary failure
- **WHEN** the primary backend for a logical model is unavailable and a fallback backend is
  configured
- **THEN** the request is served by the fallback backend and the route decision is recorded
  with a non-zero fallback depth

### Requirement: Pluggable inference backends including an offline mock
The system SHALL support pluggable inference backends — vLLM and llama.cpp for real
serving — and SHALL provide a deterministic mock backend so the full stack runs GPU-free
and offline (e.g. in CI). The daemon MUST select a default backend based on available
hardware when one is not specified.

#### Scenario: Mock backend runs without a GPU
- **WHEN** a model is configured with the mock backend and a chat completion is requested
- **THEN** the daemon returns a deterministic response with no GPU and no network access

#### Scenario: Backend auto-selection
- **WHEN** a model omits an explicit backend on a host with no GPU
- **THEN** the daemon selects a CPU-capable backend (llama.cpp or mock) rather than a
  GPU-only one

### Requirement: Route and inference spans
For every inference request the daemon SHALL emit a route-decision span wrapping a child
inference span, recording the requested logical model, chosen backend, fallback depth, and
token usage.

#### Scenario: Spans emitted per inference
- **WHEN** an inference request is served
- **THEN** the store contains a `route {model}` span with a child `chat {model}` span
  carrying the chosen backend and token usage
