## ADDED Requirements

### Requirement: Single static binary with CLI and daemon
The system SHALL ship as one statically linked Go binary (`CGO_ENABLED=0`, `netgo`,
`osusergo`) that serves both as the `faraday` CLI and, via `faraday daemon`, as the
long-lived engine daemon. The binary MUST run on an offline host with no additional
runtime dependencies for the control plane, and MUST embed CA roots rather than relying
on host trust stores.

#### Scenario: Binary is statically linked
- **WHEN** the binary is built with the project build command and inspected
- **THEN** it reports as statically linked (no dynamic libc dependency) for `linux/amd64`
  and `linux/arm64`

#### Scenario: Daemon starts with no internet
- **WHEN** `faraday daemon` is started on a host with no network route to the internet
- **THEN** the daemon starts successfully and listens on its local socket without any
  outbound network call

### Requirement: Control API over a Unix socket
The daemon SHALL expose a Connect RPC control API over a Unix domain socket
(default `/run/faraday.sock`, overridable), and the CLI SHALL communicate with the daemon
exclusively through this API. The socket MUST be created with restrictive permissions
(owner/group only).

#### Scenario: CLI reaches the daemon
- **WHEN** the daemon is running and `faraday version` is invoked
- **THEN** the CLI connects over the Unix socket and prints the daemon's version

#### Scenario: Socket permissions restrict access
- **WHEN** the daemon creates its Unix socket
- **THEN** the socket file permissions deny access to other users (mode `0660` or stricter)

#### Scenario: CLI errors clearly when the daemon is down
- **WHEN** the daemon is not running and a CLI command requiring it is invoked
- **THEN** the CLI exits non-zero with a clear message instructing the user to start the
  daemon

### Requirement: Declarative config loading and validation
The system SHALL load a declarative `faraday.yaml` whose only effectively required field
is `version`, apply documented defaults for every omitted field, and validate the result
against a JSON Schema generated from the config structs. `faraday config validate` MUST
report precise, located errors; `faraday config schema` MUST print the schema.

#### Scenario: Minimal config is valid
- **WHEN** a config containing only `version: faraday/v1` and a single empty agent is
  validated
- **THEN** validation passes and all unspecified fields resolve to documented defaults
  (e.g. `target.kind: local`, gVisor sandbox with network disabled, telemetry enabled with
  content capture off)

#### Scenario: Invalid config is rejected with location
- **WHEN** a config sets an unknown field or an out-of-range value
- **THEN** `faraday config validate` exits non-zero and reports the offending key path and
  reason

#### Scenario: Same config resolves identically across targets
- **WHEN** the same config is loaded with `target.kind: local` and with `target.kind:
  airgap`
- **THEN** the resolved `models`, `agents`, `tools`, `eval`, and `telemetry` sections are
  identical and only the `target` section differs

### Requirement: Pure-Go embedded state store
The system SHALL persist all daemon state (registry, telemetry spans, eval runs, audit
log) in an embedded SQLite database via a pure-Go driver, preserving the static-binary
guarantee, and SHALL apply schema migrations automatically on startup.

#### Scenario: Store initializes on first run
- **WHEN** the daemon starts with no existing database
- **THEN** it creates the database, applies all migrations, and is ready to record spans
  and runs

#### Scenario: State survives restart
- **WHEN** spans and eval runs are recorded, the daemon is restarted, and the data is
  queried
- **THEN** the previously recorded spans and eval runs are returned intact
