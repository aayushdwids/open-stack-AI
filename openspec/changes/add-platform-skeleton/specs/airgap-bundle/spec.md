## ADDED Requirements

### Requirement: Offline static provider
The system SHALL deploy to owned hardware with `target.kind: airgap` by verifying a
locally available signed bundle and launching the identical stack with no network access;
provisioning against pre-owned hardware is a no-op.

#### Scenario: Bring up the stack offline
- **WHEN** a verified bundle is present and a config with `target.kind: airgap` is brought
  up on a disconnected host
- **THEN** the stack launches and serves the configured models with no network access

### Requirement: Signed bundle create → transfer → install
The system SHALL build a self-contained, signed bundle online (`faraday bundle create`),
pinned by a reproducible lockfile of immutable digests, and install it offline (`faraday
bundle install`) after offline signature verification, reproducing the identical
environment.

#### Scenario: Reproducible install from a carried bundle
- **WHEN** a bundle built online is carried to a disconnected host and installed
- **THEN** offline verification passes and the installed environment matches the lockfile
  digests

#### Scenario: Tampered bundle is rejected offline
- **WHEN** a carried bundle is modified and installation is attempted
- **THEN** offline verification fails and installation is refused

### Requirement: Host runtime bootstrap from the bundle
The system SHALL bootstrap required host components (container runtime and gVisor) from the
bundle via a host-preflight/install step, since these are host-level installs not contained
in the engine binary.

#### Scenario: Preflight provisions the sandbox runtime
- **WHEN** the agent runtime requires gVisor and it is absent, and host install is run from
  the bundle
- **THEN** the container runtime and gVisor are installed from bundle contents with no
  network access
