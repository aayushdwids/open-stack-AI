## ADDED Requirements

### Requirement: Air-gapped evidence bundle assembly
The system SHALL assemble an evidence bundle from recorded data via `faraday evidence
bundle`, containing at least: a reproducible eval report (with pinned datasets, seeds, and
model identity), a model card and data card, an SBOM and AIBOM/ML-BOM, a tamper-evident
hash-chained audit log, and control-framework mappings (NIST AI RMF / 800-53). Assembly
MUST require no network access.

#### Scenario: Bundle is produced offline
- **WHEN** eval runs and agent activity have been recorded and `faraday evidence bundle
  --out evidence.tar.zst` is invoked on an offline host
- **THEN** a bundle file is produced containing the eval report, cards, SBOM/AIBOM, audit
  log, and control mappings, with no network access

#### Scenario: Bundle contents are reproducible
- **WHEN** the eval report inside the bundle is inspected
- **THEN** it pins the dataset digest, seed, and model identity needed to re-derive the
  results offline

### Requirement: Tamper-evident audit log
The system SHALL maintain a hash-chained audit log of access, inference, and change events
such that any modification of a prior entry invalidates the chain, and the bundle SHALL
include this log.

#### Scenario: Chain detects tampering
- **WHEN** an audit log entry is altered after the fact and the chain is verified
- **THEN** verification fails at or after the altered entry

### Requirement: Offline-verifiable signing
The system SHALL sign the evidence bundle and SHALL verify it with `faraday evidence
verify --offline` using only material carried with the bundle (public key and signature),
making no network call.

#### Scenario: Valid bundle verifies offline
- **WHEN** a freshly signed bundle is verified with `faraday evidence verify --offline`
  using the bundled public key
- **THEN** verification succeeds without any network access

#### Scenario: Modified bundle fails verification
- **WHEN** any bundle content is modified after signing and `faraday evidence verify
  --offline` is run
- **THEN** verification fails
