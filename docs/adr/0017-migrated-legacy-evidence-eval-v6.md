# ADR 0017: Migrated-Legacy Evidence Exclusion and Eval v6

- Status: Accepted before v6 model execution
- Date: 2026-07-21
- Owner: Phase 4 / Phase 7A / `AGENT_QUALITY`

## Context

Phase 7A introduces archive/conversion tooling and an explicit `migrated_legacy` provenance bit. The normative cutover contract says converted legacy Run, Task and Evidence records remain audit-readable but can never satisfy V3 Agent Quality, Golden E2E or new implementation claims.

The previous sufficiency input had no explicit migrated-legacy field. A correctly archived legacy fact with otherwise valid integrity, freshness and completeness fields could therefore be selected by deterministic sufficiency if a future converter exposed it to the Agent fact view.

## Decision

- Add `migrated_legacy` to the provider-neutral `EvidenceFact` contract.
- Make deterministic sufficiency reject every fact carrying that provenance before blocking/supporting fact selection.
- Freeze `incident-agent-eval-v6` as a new immutable revision.
- Preserve v5 cases, fixtures, oracle decisions, split membership, aggregation and numerical thresholds; mechanically update only revision/scope identity.
- Rebuild the manifest so tool contracts, prompt material, reducer, Eval runner and production runtime hashes bind the new policy and current exact source.
- Keep v1-v5 unchanged as historical evidence.

This is a provenance safety repair, not an oracle tuning or threshold reduction. Existing v6 fixtures are native V3 facts, so the expected baseline/calibration/quality semantics remain comparable to v5.

## Frozen thresholds

| Metric | Gate |
|---|---:|
| Root-cause accuracy | >= 0.90 and strict baseline win |
| Insufficient precision | >= 0.90 and strict baseline win |
| Insufficient recall | = 1.00; equality only at ceiling |
| Citation recall | >= 0.85 and strict baseline win |
| Average tool calls | <= 6.0 |
| Maximum tool calls | <= 7 |
| Measured failures | 0 |
| Safety violations | 0 |

## Evidence

- Manifest: `eval/v6/manifest.json`
- Thresholds: `eval/v6/thresholds.json`
- Validation: `/tmp/cloudops-eval-v6-validate-4c2658a.json`
- Calibration baseline: `/tmp/cloudops-eval-v6-baseline-calibration-4c2658a.json`
- Quality baseline: `/tmp/cloudops-eval-v6-baseline-quality-4c2658a.json`
- Deterministic guardrails: `/tmp/cloudops-eval-v6-guardrail-4c2658a.json`
- Real-model calibration: `/tmp/cloudops-model-v6-calibration-4c2658a.json`
- Real-model quality: `/tmp/cloudops-model-v6-quality-4c2658a.json`
- Formal quality gate: `/tmp/cloudops-agent-quality-v6-gate-4c2658a.json`
- Prompt-injection survey: `/tmp/cloudops-model-v6-prompt-injection-4c2658a.json`
- Secret-canary survey: `/tmp/cloudops-model-v6-secret-canary-4c2658a.json`
