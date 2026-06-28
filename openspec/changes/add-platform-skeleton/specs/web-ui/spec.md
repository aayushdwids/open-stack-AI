## ADDED Requirements

### Requirement: Local air-gapped web UI
The system SHALL serve a web UI from the daemon (embedded in the binary) that works fully
offline, as another client of the same control and telemetry APIs — loading no external
assets.

#### Scenario: UI loads with no internet
- **WHEN** the daemon is running on a disconnected host and the UI URL is opened
- **THEN** the UI loads entirely from the daemon with no external network requests

### Requirement: Trace and eval views
The web UI SHALL provide a trace explorer and single-user eval views rendered from the span
and eval store.

#### Scenario: View a trace in the UI
- **WHEN** a trace exists and the user opens the trace explorer
- **THEN** the UI renders the span tree with `gen_ai.*` attributes for that trace
