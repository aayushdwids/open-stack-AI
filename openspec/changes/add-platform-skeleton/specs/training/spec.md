## ADDED Requirements

### Requirement: Offline fine-tuning / RL on a private corpus
The system SHALL run optional fine-tuning or RL jobs on a private corpus entirely offline
through a Python training service behind the API, with training telemetry recorded in the
same span store.

#### Scenario: Fine-tune offline
- **WHEN** a training job is run against a local private corpus on a disconnected host
- **THEN** the job completes with no network access and emits training spans into the store

### Requirement: Trained model becomes a signed, consumable bundle
A completed training job SHALL produce a signed model bundle registered in the model
registry, consumable by the same routing and agents as any other model, and SHALL emit a
data card for inclusion in the evidence bundle.

#### Scenario: Route to the trained model
- **WHEN** training completes and the resulting model is registered
- **THEN** an agent configured to use it is served by the trained model through the standard
  routing path
