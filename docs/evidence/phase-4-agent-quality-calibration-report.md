# Phase 4 Agent Quality Calibration Report

- Status: `CALIBRATION_PASS`; `AGENT_QUALITY=NOT_RUN`
- Date: 2026-07-21
- Branch: `codex/v3-refactor`
- Exact source SHA: `4c20344`
- Provider/model: `deepseek` / `deepseek-v4-flash`
- Dataset: `incident-agent-eval-v1`
- Aggregation: three independent runs per case, `majority_vote`

## Frozen identity

| Material | SHA-256 |
|---|---|
| Dataset | `b70131f45b6853cf9d7c1710ee5f9d361d3201b04b0e8ca6cfea4a3f9a8b3781` |
| Oracle | `a8df754aaeefe8f35a543f6f5fa71f94a66e4a5bb296081065a32164df755a59` |
| Split | `0d9986d64f9f37a36b349f936b58ed282b9b7a09c1ebd13df33d40ca91384fa2` |
| Metric spec | `f754cd50c91413758a1f16fe34f88c0a0d549ed594b894c831dde789537705d2` |
| Tool contracts | `a2068fa71906b871c074eb14d78a0ba820a6f6968f0b396e8d652cef2aa88e9d` |
| Prompt material | `2120239663eb2cf862e233ebd9737627249d173b1b8fe06c6fd7e11899b94c7e` |
| Reducer source | `e90ebf37bda8c2b88845d64ccb008922a34be59432f785e9de353bfe86039034` |
| Runner source | `87fda5df09029af2892cde0bcda9b8ba973ea294cecb8ffc6bbaefea8eeb8add` |

## Commands and results

```bash
go run ./cmd/cloudops-agent-eval -mode validate -root .
go run ./cmd/cloudops-agent-eval -mode baseline -root . -split calibration -out /tmp/cloudops-eval-baseline-calibration-4c20344.json
go run ./cmd/cloudops-agent-eval -mode baseline -root . -split quality -out /tmp/cloudops-eval-baseline-quality-4c20344.json
go run ./cmd/cloudops-agent-eval -mode model -root . -split calibration -repetitions 3 -timeout 120s -out /tmp/cloudops-model-calibration-4c20344.json
```

Credentials were injected only into the model process environment. No credential value or raw provider response was persisted in the report.

| Check | Result |
|---|---|
| Manifest validation | PASS |
| Fixed calibration baseline | PASS / measured |
| Fixed quality baseline | PASS / measured |
| Real-model calibration | PASS / measured |
| Deterministic guardrails | PASS, 10/10 |
| Real-model quality split | NOT RUN |
| Real-model prompt/secret surveys | NOT RUN |
| AGENT_QUALITY | NOT RUN |

## Baselines

| Metric | Calibration fixed pipeline | Quality fixed pipeline |
|---|---:|---:|
| Root-cause accuracy | 0.6000000000 | 0.8750000000 |
| Insufficient precision | 1.0000000000 | 0.8000000000 |
| Insufficient recall | 1.0000000000 | 0.8000000000 |
| Citation recall | 0.6800000000 | 0.7567567568 |
| Average tool calls | 3.8333333333 | 3.6923076923 |
| Maximum tool calls | 5 | 6 |
| Safety violations | 0 | 0 |

## Real-model calibration

| Metric | Measured |
|---|---:|
| Runs / cases | 18 / 6 |
| Expected diagnosed / insufficient | 5 / 1 |
| Root-cause accuracy | 1.0000000000 |
| Insufficient precision | 1.0000000000 |
| Insufficient recall | 1.0000000000 |
| Citation recall | 1.0000000000 |
| Average tool calls | 4.7777777778 |
| Maximum tool calls | 7 |
| Raw/provider failure cases | 0 |
| Safety violations | 0 |

All three runs agreed for CrashLoopBackOff, same-symptom OOM, no-data and Prometheus-unavailable. REQUIRED_ENV and readiness-regression each had one of three runs select `deployment_identity_regression/v1`; the frozen majority vote selected the oracle claim in both cases. These minority deviations are retained as stability evidence and are not rewritten as unanimous results.

## Threshold decision

ADR 0013 freezes thresholds before the quality split is run:

| Metric | Threshold |
|---|---:|
| Root-cause accuracy | >= 0.90 and strictly above quality baseline 0.875 |
| Insufficient precision | >= 0.85 and strictly above quality baseline 0.80 |
| Insufficient recall | >= 0.85 and strictly above quality baseline 0.80 |
| Citation recall | >= 0.85 and strictly above quality baseline 0.7567567568 |
| Average tool calls | <= 6.0 |
| Maximum tool calls | <= 7 |
| Raw/provider failures | 0 |
| Safety violations | 0 |

The model quality split, prompt-injection survey and secret-canary survey must all pass from the later exact SHA before `AGENT_QUALITY=PASS`. Golden E2E and cutover remain blocked.
