## ADDED Requirements

### Requirement: Offline eval suites
The system SHALL run eval suites entirely offline, supporting at least three suite kinds:
`code_passk` (execute generated code against unit tests in the sandbox and compute pass@k),
`deterministic` (rule-based checks such as exit-zero, regex, JSON-schema, exact-match), and
`judge` (LLM-as-judge scored against a local model). Datasets MUST be resolvable from
bundled fixtures or local paths with no network access.

#### Scenario: Code pass@k against a bundled dataset
- **WHEN** a `code_passk` suite runs against a bundled mini dataset with `k: 1`
- **THEN** generated solutions are executed against the dataset's unit tests in the sandbox
  and a pass@k score is produced, with no network access

#### Scenario: Deterministic checks need no model
- **WHEN** a `deterministic` suite runs
- **THEN** its checks evaluate without invoking any model and produce pass/fail per case

#### Scenario: Judge uses the local model
- **WHEN** a `judge` suite runs with a configured local judge model
- **THEN** scoring calls the local model via the `/v1` surface and records scores, making
  no call to any external service

### Requirement: Reproducible, recorded eval runs
Each eval run SHALL be recorded with its inputs pinned for reproducibility — dataset
digest, random seed, model/backend identity, and config — and each run SHALL emit eval
spans into the telemetry store.

#### Scenario: Run records reproducibility metadata
- **WHEN** an eval suite completes
- **THEN** the recorded run includes the dataset digest, seed, model identity, and resulting
  metric values

#### Scenario: Eval emits spans
- **WHEN** an eval suite runs
- **THEN** the store contains `eval {suite}` spans recording the suite, metric, and score

### Requirement: Thresholds, regression, and CI gating
The system SHALL evaluate configured thresholds and optional run-over-run regression
comparison, and `faraday eval run` SHALL exit non-zero when a configured gate
(`threshold` or `regression`) fails, so it can gate CI.

#### Scenario: Threshold gate fails the run
- **WHEN** a suite's score is below its configured threshold and `ci.fail_on` includes
  `threshold`
- **THEN** `faraday eval run` exits non-zero and reports which threshold failed

#### Scenario: Passing run exits zero
- **WHEN** all configured thresholds are met and no gated regression is detected
- **THEN** `faraday eval run` exits zero

#### Scenario: Regression detected against baseline
- **WHEN** a run scores materially below the configured baseline and `ci.fail_on` includes
  `regression`
- **THEN** the run exits non-zero and reports the regression versus the baseline
