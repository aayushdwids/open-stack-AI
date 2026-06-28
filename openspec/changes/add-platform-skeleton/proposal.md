## Why

The full platform skeleton must exist from day one so every later slice has a spec home,
even though only the eval/observability slice is built deep first. This change records the
remaining capabilities at requirement-skeleton depth — enough to lock the contracts
(routing/MCP, cloud provisioning, the air-gap bundle and offline spine, training, web UI,
and the paid team-observability tier) without front-loading implementation detail.

## What Changes

- Capture the COMPOSE widening: full routing, external MCP tools, multi-agent
  orchestration.
- Capture TRY: cloud GPU provisioning that shares one config with the offline target (the
  spine).
- Capture OWN+AIR-GAP: the offline static provider plus the signed-bundle create→carry→
  install supply chain (held loosely — plumbing, not identity).
- Capture TRAIN: optional offline fine-tune/RL producing a signed model bundle.
- Capture the local WEB UI as another client of the same API, served air-gapped.
- Capture the PAID team-observability tier (cross-user aggregation, RBAC, audit, alerting,
  dashboards) behind the `enterprise` build tag with offline license gating.

These are skeleton requirements; each is detailed and deepened in its own future change
before implementation, following the slice order in `docs/ROADMAP.md`.

## Capabilities

### New Capabilities
- `compose-routing`: full model routing/fallback, external MCP tool servers, and
  multi-agent orchestration patterns.
- `cloud-provisioning`: declarative cloud GPU provisioning (TRY) that shares one config
  with the offline target.
- `airgap-bundle`: the offline static provider plus the signed-bundle create→transfer→
  install supply chain and reproducible lockfile.
- `training`: optional offline fine-tuning / RL on a private corpus, producing a signed,
  registry-consumable model bundle.
- `web-ui`: the local web UI served from the daemon, working fully air-gapped.
- `team-observability`: the paid, self-hosted, cannot-phone-home tier — cross-user
  aggregation, retention, RBAC, audit trails, alerting, collaborative eval dashboards.

### Modified Capabilities
- (none — these are new skeleton capabilities)

## Impact

- Establishes spec homes and contracts for Slices 2–7 so later deep changes extend rather
  than invent.
- Reaffirms the spine contract (one config, both targets), the air-gap guarantee, and the
  open-core line (free single-user vs paid team-scale).
- No implementation in this change; it is documentation-of-record at requirement depth.
