# Hosted Supply-Chain Validation Report

## Result

```text
RESULT=PASS
VALIDATED_MAIN_SHA=44a2ede586f0a624da48a82de384c783fc348cbd
REQUIRED_CI_RUN=30353151266
HOSTED_SUPPLY_CHAIN_RUN=30353402682
RUN_ATTEMPT=1
EVENT=workflow_dispatch
REF=refs/heads/main
ENVIRONMENT=none
CLEANUP=PASS
```

This report records the real GitHub-hosted validation executed for the exact human-merged `main` revision. It covers CI, temporary GHCR publication, scanning, SBOM generation, attestations, keyless signing, transparency verification, and destructive cleanup. It does not claim Argo, staging, production, tag, release, or retained production-image validation.

## Delivery chain

| Gate | Result | Evidence |
|---|---|---|
| Implementation merge | `PASS` | PR [#1](https://github.com/05allan1213/CloudOps-Copilot/pull/1), merge `665ac7c699eeaa9e5b7803a3298df79e9781d9bc` |
| Cleanup hardening merge | `PASS` | PR [#2](https://github.com/05allan1213/CloudOps-Copilot/pull/2), merge `91644ee609b34d01dc316abe56ad0c7e9d67be15` |
| Package-delete correction merge | `PASS` | PR [#3](https://github.com/05allan1213/CloudOps-Copilot/pull/3), merge `44a2ede586f0a624da48a82de384c783fc348cbd` |
| Exact merged-SHA CI | `PASS` | run [30353151266](https://github.com/05allan1213/CloudOps-Copilot/actions/runs/30353151266), `2026-07-28T11:02:15Z` to `2026-07-28T11:05:12Z`; all 8 jobs passed, including aggregate `Required CI` |
| Exact merged-SHA hosted validation | `PASS` | run [30353402682](https://github.com/05allan1213/CloudOps-Copilot/actions/runs/30353402682), `2026-07-28T11:05:48Z` to `2026-07-28T11:09:18Z`; non-production context plus all 4 image jobs passed |

The remote `main` ref was checked immediately before dispatch and resolved to the validated SHA. The hosted run reports the same `headSha`, and each artifact records the same SHA, run ID, attempt, workflow path, ref, issuer, and certificate identity.

## Image matrix

| Service | Exact digest | SPDX packages | Rekor log index | Evidence artifact | Result |
|---|---|---:|---:|---|---|
| api | `sha256:480a3e1e55ffe17f080bfba945b2df0e46cc4728f82b1588b92b7cecf119caa6` | 98 | `2270959091` | `hosted-validation-evidence-api-30353402682-1` | `PASS` |
| worker | `sha256:01faceb2e5a6e705a9698cd269a1acbe34441480afb4f8091b23b471eab90440` | 136 | `2270959090` | `hosted-validation-evidence-worker-30353402682-1` | `PASS` |
| migrate | `sha256:1a570f5eb38b41cf2490f7db609ef6f3f3906ef461022b8ae04fb82c1b3e2678` | 40 | `2270961133` | `hosted-validation-evidence-migrate-30353402682-1` | `PASS` |
| demo | `sha256:d7802c95593fa729e7c51cdc943010dc3e014df4c4ecbb2d2e9768216d8ac389` | 55 | `2270960634` | `hosted-validation-evidence-demo-30353402682-1` | `PASS` |

All four artifacts were downloaded individually and accepted only after the following cross-file checks passed:

- `git-sha.txt`, `workflow-run.json`, image identity, provenance subject, SBOM attestation subject, certificate claims, and Cosign annotations all resolve to `44a2ede586f0a624da48a82de384c783fc348cbd` and the corresponding exact digest.
- OCI revision and version labels equal the validated SHA; source equals `https://github.com/05allan1213/CloudOps-Copilot`; platform is `linux/amd64`.
- Trivy reports `0 HIGH`, `0 CRITICAL`, `0 secrets`, and `0 misconfigurations` for every exact digest.
- SPDX 2.3 SBOMs are non-empty. SLSA provenance and SPDX attestation subjects match the exact repository and digest.
- Fulcio issuer is `https://token.actions.githubusercontent.com`; certificate identity is `https://github.com/05allan1213/CloudOps-Copilot/.github/workflows/hosted-supply-chain-validation.yaml@refs/heads/main`.
- Rekor bundles contain inclusion promises, inclusion proofs, checkpoints, integrated times, and log indexes. Cosign verification binds repository, digest, service, and Git SHA.
- `evidence.sha256` and `json-evidence.sha256` validate for every artifact. All recorded GitHub Action dependencies use full 40-character commit pins.

## Registry cleanup

| Service | Subject version | Initial versions | Subject DELETE | Package DELETE | Final subject | Final package | Remaining versions | Referrers | Result |
|---|---:|---:|---:|---:|---|---|---:|---|---|
| api | `1074166280` | 8 | `204` | `204` | `404 / absent` | `404 / absent` | 0 | `subject_not_found` | `PASS` |
| worker | `1074166201` | 8 | `204` | `204` | `404 / absent` | `404 / absent` | 0 | `subject_not_found` | `PASS` |
| migrate | `1074166323` | 8 | `204` | `204` | `404 / absent` | `404 / absent` | 0 | `subject_not_found` | `PASS` |
| demo | `1074165939` | 8 | `204` | `204` | `404 / absent` | `404 / absent` | 0 | `subject_not_found` | `PASS` |

Each `registry-cleanup.json` records `result=pass`, `identity_state=valid`, `digest_state=valid`, `delete_failures=0`, `package_delete_state=deleted`, an empty `post_delete_package_versions` list, and no discoverable referrers.

## Failed-run provenance

Run [30350763424](https://github.com/05allan1213/CloudOps-Copilot/actions/runs/30350763424) correctly failed its strict cleanup gate: subject deletion succeeded, but GitHub Packages rejected deletion of the final package version through the version endpoint, leaving one old version in each validation package. PR #3 added an identity-checked package-level DELETE and final `404`/empty-state verification. The final run restarted from Gate 0 on the new merged SHA; no build, scan, digest, signature, or cleanup evidence from the failed SHA was reused as final proof.

## Remaining NOT RUN

- Product GitHub App runtime read/write: no real App credentialed adapter run.
- Argo Application read, exact merged revision observation, sync, rollout, rollback, or override.
- Staging and production acceptance.
- Release tag, GitHub Release, retained release image publication, or deployment.
- Second real cluster, multi-cluster/multi-tenant acceptance, and production backup/DR exercise.
- Hosted/frozen-dataset repeated Agent Quality evaluation and the separately listed external Prometheus, Alertmanager MCP, Logs/Traces console or dedicated MCP paths.

The project result therefore remains `DONE_WITH_NOT_RUN`, while the previously unrun human merge, exact merged-SHA Required CI, GHCR hosted supply chain, and hosted cleanup gates are now `PASS`.
