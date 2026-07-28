# ADR 0014: Agent Eval v3 Policy and Quality Thresholds

- Status: Accepted before v3 quality execution
- Date: 2026-07-21
- Owner: Phase 4 / `AGENT_QUALITY`

## Context

`incident-agent-eval-v1` is preserved with its formal quality failure. v2 added a case-local contradiction blocker and fixed `model-conflicting-sources`, but real-model calibration at exact SHA `b564b86542197a792d0e4254440f4725c13ffde8` still failed: two of three `model-readiness-regression` runs selected `deployment_identity_regression/v1`.

That candidate policy treated unchanged source/image identity plus a runtime symptom as sufficient evidence of an identity regression. Unchanged identity is not positive regression evidence, so the policy could deterministically authorize a claim that its facts did not establish. No prompt or oracle change can repair that sufficiency defect.

## Decision

Freeze `incident-agent-eval-v3` as a separate revision. v3 removes `deployment_identity_regression/v1` from every case and preserves all other candidate policies, facts, fixtures, limits, oracles and split membership. v1 and v2 remain unchanged and independently verifiable.

The v3 fixed quality baseline at exact SHA `b2a7c0d` measured:

| Metric | Fixed pipeline |
|---|---:|
| Root-cause accuracy | 0.8750000000 |
| Insufficient precision | 0.8333333333 |
| Insufficient recall | 1.0000000000 |
| Citation recall | 0.7567567568 |
| Average tool calls | 3.7692307692 |
| Maximum tool calls | 6 |
| Safety violations | 0 |

The configured model then passed the six-case v3 calibration split with 18/18 completed runs, unanimous majority outcomes, 1.0 for all four quality metrics, average/maximum tool calls 4.7777777778/7, zero errors and zero safety violations.

Freeze these v3 quality gates before running the quality split:

| Metric | Gate |
|---|---:|
| Root-cause accuracy | >= 0.90 |
| Insufficient precision | >= 0.90 |
| Insufficient recall | = 1.00 |
| Citation recall | >= 0.85 |
| Average tool calls | <= 6.0 |
| Maximum tool calls | <= 7 |
| Raw/provider or majority-vote failure cases | 0 |
| Safety violations | 0 |

Root-cause accuracy, insufficient precision/recall and citation recall must also beat the fixed pipeline on the same quality split. Strict improvement remains required whenever the baseline is below 1.0. When the baseline is exactly 1.0, a measured value of exactly 1.0 is accepted as equality at the mathematical ceiling; any measured value below 1.0 fails.

Aggregation remains `majority_vote` over exactly three independent runs per case. Quality must pass before the prompt-injection and secret-canary real-model surveys run.

## Rejected alternatives

- Mutating v2 after observing its calibration result.
- Lowering or removing the insufficient-recall comparison because the baseline reached 1.0.
- Treating unchanged identity as affirmative evidence of a deployment identity regression.
- Removing other valid distractor policies or changing the oracle to accept the invalid claim.

## Evidence

- v2 failure report: `docs/evidence/phase-4-agent-quality-v2-calibration-report.md`
- v3 manifest: `eval/v3/manifest.json`
- v3 calibration baseline: `/tmp/cloudops-eval-v3-baseline-calibration-b2a7c0d.json`
- v3 quality baseline: `/tmp/cloudops-eval-v3-baseline-quality-b2a7c0d.json`
- v3 deterministic guardrails: `/tmp/cloudops-eval-v3-guardrails-b2a7c0d.json`
- v3 real-model calibration: `/tmp/cloudops-model-v3-calibration-b2a7c0d.json`
