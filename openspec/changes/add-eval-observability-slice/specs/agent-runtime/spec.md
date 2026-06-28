## ADDED Requirements

### Requirement: Air-gap-native sandboxed code execution
The system SHALL execute agent-generated code inside an isolated sandbox (gVisor/`runsc`
by default) drawn from a managed pool, and the sandbox MUST have no network access. The
network-deny guarantee MUST be enforced at the sandbox boundary, not merely requested by
policy.

#### Scenario: Generated code runs in a sandbox
- **WHEN** the agent generates code and invokes the `code_exec` tool
- **THEN** the code runs inside a sandbox isolated from the host filesystem and process
  space, and its stdout/stderr/exit-status are returned

#### Scenario: Sandbox cannot reach the network
- **WHEN** code executed in a sandbox attempts an outbound network connection
- **THEN** the connection fails because the sandbox has no network egress

#### Scenario: Resource limits enforced
- **WHEN** sandboxed code exceeds the configured CPU time or memory limit
- **THEN** the execution is terminated and reported as a resource-limit failure rather than
  hanging

### Requirement: Warm sandbox pool with lifecycle management
The system SHALL maintain a pool of pre-baked sandboxes to reduce startup latency,
assigning a sandbox per execution, resetting or destroying it after use, and asynchronously
replenishing the pool. The pool size MUST be configurable.

#### Scenario: Execution acquires from the pool
- **WHEN** a sandbox execution is requested and the pool has a ready sandbox
- **THEN** the execution is assigned that sandbox without waiting for a cold start

#### Scenario: Pool replenishes after use
- **WHEN** a sandbox is consumed by an execution
- **THEN** the pool asynchronously creates a replacement to restore the configured pool size

### Requirement: MCP-brokered tools
Agents SHALL access tools only through the daemon's MCP broker, never directly. The broker
MUST host built-in tools (including `code_exec`) as MCP servers and MUST emit a telemetry
span for every tool call.

#### Scenario: Tool call is brokered and traced
- **WHEN** the agent calls the `code_exec` tool
- **THEN** the call is routed through the MCP broker and produces an `execute_tool` span
  with the tool name and outcome

#### Scenario: Agent cannot bypass the broker
- **WHEN** the agent attempts to run code without going through a registered tool
- **THEN** no sandbox is allocated and the action is rejected

### Requirement: Code-gen reasoning loop
The system SHALL run a plan→generate→execute→observe→repair loop for the code-gen
reference agent: it plans the change, generates code and tests, executes them in the
sandbox, observes results, and repairs on failure up to a configured iteration limit. The
loop MUST run entirely against a local model with no internet.

#### Scenario: Successful generate-and-test
- **WHEN** the agent is given a task it can solve and runs with a working local model
- **THEN** it returns code whose generated tests pass, within the step and repair limits

#### Scenario: Repair on test failure
- **WHEN** the agent's first attempt produces failing tests and repair iterations remain
- **THEN** the agent observes the failures and produces a revised attempt, and the repair
  iteration is recorded on the trace

#### Scenario: Loop bounded by limits
- **WHEN** the agent cannot pass tests within `max_repair_iterations`
- **THEN** the run terminates with a clear unresolved status rather than looping
  indefinitely
