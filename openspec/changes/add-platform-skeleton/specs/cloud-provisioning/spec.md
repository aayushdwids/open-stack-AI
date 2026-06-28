## ADDED Requirements

### Requirement: Declarative cloud GPU provisioning
The system SHALL provision rented cloud GPU capacity from the `target` block of
`faraday.yaml` (`kind: cloud`), launch the inference/agent stack on it, and tear it down on
request or after idle timeout.

#### Scenario: Provision and serve on cloud
- **WHEN** a config with `target.kind: cloud` and GPU resources is brought up
- **THEN** the system provisions matching capacity, launches the stack, and serves the
  configured models

#### Scenario: Idle teardown
- **WHEN** a cloud target's idle timeout elapses with no activity
- **THEN** the provisioned resources are torn down

### Requirement: One config across cloud and air-gap (the spine)
The same `faraday.yaml` SHALL deploy to a cloud target and an air-gap target with only the
`target` block differing; the `models`, `agents`, `tools`, `eval`, and `telemetry` behavior
MUST be identical across both.

#### Scenario: Identical behavior across targets
- **WHEN** the same config is deployed with `target.kind: cloud` and later `target.kind:
  airgap`
- **THEN** the agent and eval behavior are identical and only `target` differs between the
  two deployments
