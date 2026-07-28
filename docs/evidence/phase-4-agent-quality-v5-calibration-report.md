# Phase 4 Agent Quality v5 Calibration Report

- Status: `CALIBRATION_PASS`; `AGENT_QUALITY=NOT_RUN`
- Date: 2026-07-21
- Branch: `codex/v3-refactor`
- Exact evaluated source SHA: `33e1f1c`
- Provider/model: `deepseek` / `deepseek-v4-flash`
- Dataset: `incident-agent-eval-v5`
- Aggregation: three independent runs per case, `majority_vote`

## Revision purpose

v5 preserves the v4 data, oracle, deterministic policy and frozen thresholds while adding a manifest hash over the current production Agent runtime and model-identity persistence paths. This restores current-source proof after `872af1c` without rewriting v4 history.

## Frozen identity

| Material | SHA-256 |
|---|---|
| Dataset | `4dc949ddb59474800121829c016229285be18c62a23304b1fb17521b8582a4e1` |
| Oracle | `7e26dde2abf1695d18d17d89a5aa8f6976d9ebac799551595ffc2f58bc5ec1b2` |
| Split | `735fa0f206092f2c1f3d58fafe18345ee1d838d51026333cad46209c78544de3` |
| Metric spec | `2b2f21c1fd52468d7c26040854b6971ffe36f23d2ac87f79c1bdc49b7a355b6a` |
| Tool contracts | `41e7a9e3f5a052c2fcd5e3a570125b5529bc6834afdf9a810c3270f7706e4ab5` |
| Prompt material | `94badd947e0709f8b86df0ae495406510424b38a41597855a8648159c23d3afe` |
| Reducer source | `e90ebf37bda8c2b88845d64ccb008922a34be59432f785e9de353bfe86039034` |
| Runner source | `57278f75df3b539b542f6deb0869bf6c268521736539040e4a6d64f2b409114e` |
| Production runtime source | `08a54d323729653673604fdd3b5db790c0f3d2a71f2c6e6b1615a150b5c1a7a3` |

## Results

| Check | Result |
|---|---|
| Manifest validation | PASS, 29 cases |
| Calibration fixed baseline | PASS, 6/6 runs |
| Quality fixed baseline | measured, 13/13 runs; one expected fixed-pipeline failure |
| Deterministic guardrails | PASS, 10/10 |
| Real-model calibration | PASS, 18/18 runs |
| Real-model quality | NOT RUN at this checkpoint |
| Hostile-input surveys | NOT RUN at this checkpoint |
| `AGENT_QUALITY` | NOT RUN at this checkpoint |

| Metric | Calibration baseline | Real-model calibration |
|---|---:|---:|
| Root-cause accuracy | 1.0000000000 | 1.0000000000 |
| Insufficient precision | 1.0000000000 | 1.0000000000 |
| Insufficient recall | 1.0000000000 | 1.0000000000 |
| Citation recall | 1.0000000000 | 1.0000000000 |
| Average tool calls | 4.1666666667 | 4.8333333333 |
| Maximum tool calls | 6 | 7 |
| Failure cases | 0 | 0 |
| Safety violations | 0 | 0 |

All 18 raw runs completed without provider errors. Credentials were injected only into the model subprocess environment; no credential value or raw provider response was persisted.

## Evidence

- Validation: `/tmp/cloudops-eval-v5-validate-33e1f1c.json`
- Calibration baseline: `/tmp/cloudops-eval-v5-baseline-calibration-33e1f1c.json`
- Quality baseline: `/tmp/cloudops-eval-v5-baseline-quality-33e1f1c.json`
- Deterministic guardrails: `/tmp/cloudops-eval-v5-guardrail-33e1f1c.json`
- Real-model calibration: `/tmp/cloudops-model-v5-calibration-33e1f1c.json`
