## ADDED Requirements

### Requirement: Self-hosted paid tier that cannot phone home
The paid team-observability tier SHALL be compiled behind the `enterprise` build tag, run
entirely inside the air-gap, and validate its license from an offline Ed25519-signed file
against a public key embedded in the binary — making no network call. The default build
MUST link no proprietary code.

#### Scenario: Enterprise features gated by offline license
- **WHEN** the enterprise build runs with a valid offline license file and no network
- **THEN** team features are enabled with no outbound connection

#### Scenario: Default build excludes proprietary code
- **WHEN** the default (non-enterprise) build is produced
- **THEN** it links no proprietary tier code and exposes only free single-user features

### Requirement: Cross-user aggregation and governance
The tier SHALL provide team-scale capabilities not available in the free single-user tier:
cross-user trace/eval aggregation, retention/history, RBAC, audit trails, alerting, and
collaborative eval dashboards.

#### Scenario: Aggregate across users
- **WHEN** multiple users' traces are recorded and a team dashboard is opened
- **THEN** the tier aggregates them into a team-wide view subject to RBAC

#### Scenario: RBAC restricts access
- **WHEN** a user without permission requests a restricted team view
- **THEN** access is denied per the configured role
