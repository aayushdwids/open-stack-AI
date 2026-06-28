## ADDED Requirements

### Requirement: Multi-model routing with fallback
The system SHALL route requests across multiple configured models with weighted selection
and ordered failover, exposing all models through the one OpenAI-compatible surface, and
SHALL record each route decision as a span.

#### Scenario: Route across two models
- **WHEN** a logical model is backed by two models and the primary is unavailable
- **THEN** the request is served by the secondary and the fallback is recorded on the trace

### Requirement: External MCP tool servers
The system SHALL allow registering external MCP servers (stdio or streamable-HTTP) as tools
available to agents, scoping tools per session and emitting a span per tool call.

#### Scenario: Attach an external MCP tool
- **WHEN** an external MCP server is configured and an agent invokes one of its tools
- **THEN** the daemon brokers the call through the MCP broker and records an `execute_tool`
  span

### Requirement: Multi-step and multi-agent orchestration
The system SHALL support orchestration beyond the single code-gen loop, including
multi-step plans and handoff between agents, with all steps correlated under one trace.

#### Scenario: Multi-agent handoff is traced
- **WHEN** one agent hands off to another within a run
- **THEN** both agents' steps appear under a single correlated trace
