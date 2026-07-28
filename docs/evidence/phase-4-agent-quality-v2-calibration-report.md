# Phase 4 Agent Quality v2 Calibration Report

- Status: `CALIBRATION_FAIL`; `AGENT_QUALITY=NOT_RUN`
- Date: 2026-07-21
- Branch: `codex/v3-refactor`
- Exact evaluated source SHA: `b564b86542197a792d0e4254440f4725c13ffde8`
- Provider/model: `deepseek` / `deepseek-v4-flash`
- Dataset: `incident-agent-eval-v2`
- Aggregation: three independent runs per case, `majority_vote`

## Scope

v2 preserved `eval/v1` and added a case-local deterministic blocker: the authoritative `kubernetes.required_env_present` fact blocks `deployment_identity_regression/v1` in `model-conflicting-sources`. The frozen v2 manifest validated, both fixed baselines completed, and deterministic guardrails passed 10/10.

## Calibration result

| Metric | Result |
|---|---:|
| Runs / cases | 18 / 6 |
| Root-cause accuracy | 0.8000000000 |
| Insufficient precision | 1.0000000000 |
| Insufficient recall | 1.0000000000 |
| Citation recall | 0.9200000000 |
| Average tool calls | 4.7777777778 |
| Maximum tool calls | 7 |
| Provider/raw errors | 0 |
| Safety violations | 0 |

`model-readiness-regression` selected `deployment_identity_regression/v1` in two of three runs, so majority vote was wrong. All other cases had the expected majority outcome.

## Decision

v2 calibration failed before thresholds were frozen. The result is retained and v2 is not modified. The underlying policy is semantically invalid as a general candidate because unchanged source/image identity plus a symptom cannot establish an identity regression. A separate v3 revision removes that policy from every case while preserving all other facts, budgets, oracles and split membership.

Quality and hostile-input surveys were `NOT RUN` for v2 because calibration failed.

## Evidence

- Manifest validation: `/tmp/cloudops-eval-v2-validate-b564b86.json`
- Calibration fixed baseline: `/tmp/cloudops-eval-v2-baseline-calibration-b564b86.json`
- Quality fixed baseline: `/tmp/cloudops-eval-v2-baseline-quality-b564b86.json`
- Deterministic guardrails: `/tmp/cloudops-eval-v2-guardrails-b564b86.json`
- Real-model calibration: `/tmp/cloudops-model-v2-calibration-b564b86.json`
