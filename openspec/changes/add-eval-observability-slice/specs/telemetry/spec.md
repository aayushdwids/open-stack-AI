## ADDED Requirements

### Requirement: Every meaningful action emits a span
The system SHALL emit an OpenTelemetry span for every route decision, inference call, tool
/ MCP call, retrieval, agent step, and eval execution, using the `gen_ai.*` semantic
conventions (opted in via `gen_ai_latest_experimental`). Spans MUST be correlated under a
single trace per top-level operation (e.g. one agent run).

#### Scenario: An agent run produces a correlated trace
- **WHEN** a code-gen agent runs a task that performs one inference and one tool call
- **THEN** the store contains a trace whose root is the agent-invoke span with child spans
  for the route decision, the inference (`chat {model}`), and the tool call
  (`execute_tool {tool}`), all sharing one trace id

#### Scenario: Inference span carries token usage
- **WHEN** an inference completes
- **THEN** its span records `gen_ai.request.model`, `gen_ai.provider.name`,
  `gen_ai.usage.input_tokens`, and `gen_ai.usage.output_tokens`

#### Scenario: Tool call span records outcome
- **WHEN** the `code_exec` tool runs code in a sandbox
- **THEN** an `execute_tool code_exec` span records the tool call id, the sandbox id, and
  the exit status

### Requirement: Zero-egress telemetry pipeline
The telemetry pipeline SHALL export spans only to local sinks — an embedded collector
writing to the SQLite store and to a local file (OTLP JSON-lines) — and MUST NOT make any
outbound network connection. ML-plane (Python) services SHALL emit spans via OTLP to a
`localhost` endpoint owned by the daemon.

#### Scenario: No outbound connections during telemetry export
- **WHEN** spans are produced and exported while outbound network is monitored
- **THEN** no connection is made to any non-loopback address

#### Scenario: Python service spans reach the daemon
- **WHEN** a Python inference or agent service emits OTLP spans to the daemon's localhost
  collector endpoint
- **THEN** those spans appear in the SQLite store correlated with the originating trace

### Requirement: Content capture is off by default
The system SHALL NOT record prompt or completion content on spans by default. When
`telemetry.capture_content` is explicitly enabled, content SHALL be recorded as structured
message records (`gen_ai.input.messages` / `gen_ai.output.messages`).

#### Scenario: Prompts are not stored by default
- **WHEN** an inference runs with default telemetry settings
- **THEN** the stored span contains token counts and metadata but no prompt or completion
  text

#### Scenario: Content captured only when enabled
- **WHEN** `telemetry.capture_content: true` is set and an inference runs
- **THEN** the span (or linked log records) contains the input and output messages

### Requirement: Trace query and retrieval
The system SHALL allow querying recorded traces through the CLI: listing recent traces and
showing the full span tree for a given trace, including the most recent one via
`faraday trace last`.

#### Scenario: Show the last trace
- **WHEN** at least one trace has been recorded and `faraday trace last` is invoked
- **THEN** the CLI prints the span tree for the most recent trace with names, durations,
  and key `gen_ai.*` attributes

#### Scenario: List recent traces
- **WHEN** `faraday trace list` is invoked
- **THEN** the CLI prints recent traces with their id, root operation, start time, and
  duration
