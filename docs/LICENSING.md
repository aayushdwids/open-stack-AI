# Faraday Licensing & Open-Core Model

> Not legal advice. The CLA/dual-license/relicense mechanics below must be confirmed with
> a lawyer before any relicense, and copyright ownership must be provably separate from any
> employer IP.

## The line

| Capability | Tier | License |
|---|---|---|
| Run the engine, serve inference, run an agent, run an eval | Free | Apache-2.0 |
| See **my own** traces, basic metrics, make an evidence bundle (single user) | Free | Apache-2.0 |
| Cross-user **aggregation**, retention/history, RBAC, audit trails, alerting, collaborative eval dashboards | Paid | Commercial (self-hosted) |

**Rule of thumb:** local single-user = free; anything team-scale (aggregation,
governance, audit) = paid. The paid tier is sold as a **self-hosted license + support**
that runs *inside* the air-gap. It is **not SaaS** and **cannot phone home**.

## How the split is enforced in code

- Engine code lives under `internal/` and is **Apache-2.0**.
- The paid tier lives in a **separate Go module** `enterprise/` with its own commercial
  `LICENSE`, wired into the engine through interfaces the engine defines.
- It compiles **only** with `-tags enterprise`. The default `make build` links no
  proprietary code (Grafana's OSS/Enterprise split). Apache obligations stay clean and
  auditable.

## Offline license validation (the air-gap constraint)

The paid tier **cannot phone home** — it runs in a sealed enclave. So licensing uses
**Ed25519-signed license files**:

- A license payload (`tier`, `seats`, `expiry`, `features`) is signed offline with
  Faraday's private key.
- The **public key is hard-coded in the binary**.
- The daemon verifies the signature **locally** — no network. Expiry is embedded in the
  signed claims; tampering invalidates the signature.
- Delivered by USB/email alongside the bundle.

## Contributions: DCO, not CLA

- Outside contributions are accepted under the **Developer Certificate of Origin** —
  every commit carries `Signed-off-by:` (GitLab's model since 2017; CLAs deter
  contributors).
- The engine is Apache-2.0 (already relicensable by recipients) and the paid tier is
  separately authored, so **no CLA is needed to preserve relicensing rights** for the
  paid tier. A CLA would only be necessary if the *core* were copyleft and we wanted to
  dual-license the core itself.
- CI enforces the `Signed-off-by` trailer on every PR commit.

## Supply-chain integrity (ships with every release)

Each release and each air-gap bundle carries: a **cosign signature** (offline-verifiable
via bundled public key + SET, no Rekor call), an **SBOM** (syft/CycloneDX), an
**AIBOM/ML-BOM** for models+datasets, and **SLSA build provenance** (in-toto). These are
also the raw materials of the evidence bundle.
