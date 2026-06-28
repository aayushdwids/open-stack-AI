# Tasks — Platform Skeleton

This change is documentation-of-record at requirement depth. Each capability is deepened
into its own change and implemented in slice order (`docs/ROADMAP.md`). Tasks here track
the deepening, not the full build.

## 1. Lock the skeleton contracts

- [ ] 1.1 Confirm the spine contract (one config, both targets) is reflected in
  `cloud-provisioning` and `airgap-bundle` specs.
- [ ] 1.2 Confirm the open-core line (free single-user vs paid team-scale) is reflected in
  `team-observability`.
- [ ] 1.3 Confirm the air-gap guarantee (offline, zero-egress, offline-verifiable signing)
  is consistent across `airgap-bundle`, `training`, `web-ui`, and `team-observability`.

## 2. Deepen per slice (future changes)

- [ ] 2.1 Slice 2: deepen `compose-routing` into a full change (routing, external MCP,
  multi-agent) and implement.
- [ ] 2.2 Slice 3: deepen `cloud-provisioning` and implement; prove the spine (one config,
  two targets).
- [ ] 2.3 Slice 4: deepen `airgap-bundle` (offline provider + signed bundle pipeline) and
  implement; held loosely.
- [ ] 2.4 Slice 5: deepen `training` and implement (optional).
- [ ] 2.5 Slice 6: deepen `web-ui` and implement.
- [ ] 2.6 Slice 7: deepen `team-observability` and implement behind the `enterprise` tag.
