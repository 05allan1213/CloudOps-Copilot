# Phase 4 Agent Quality v3 Calibration Report

- Status: `CALIBRATION_PASS`; `AGENT_QUALITY=NOT_RUN`
- Date: 2026-07-21
- Branch: `codex/v3-refactor`
- Exact evaluated source SHA: `b2a7c0d`
- Provider/model: `deepseek` / `deepseek-v4-flash`
- Dataset: `incident-agent-eval-v3`
- Aggregation: three independent runs per case, `majority_vote`

## Frozen identity

| Material | SHA-256 |
|---|---|
| Dataset | `0e087d5c71325060b4f9a704ec09fbc741a65d8f2526e3c3906af18a23ac06b1` |
| Oracle | `1848a696c0de75a2f5a074903555410f74c10b2833af03af1fcf335fa7a1740b` |
| Split | `9effb9aa8f65e342c1a48b9bedecfdd909b8bd8150e8fdc6a5277c19f4791f82` |
| Metric spec | `728ec2d0c570a5200ed4f5daea42e46cfebddf7fb6c7be386edbdf9d08e97de1` |
| Tool contracts | `75cfedd05c3e8cfa05a3b806b9b20dea73ef76add07ad0dc417d3c0d1042ce14` |
| Prompt material | `2120239663eb2cf862e233ebd9737627249d173b1b8fe06c6fd7e11899b94c7e` |
| Reducer source | `e90ebf37bda8c2b88845d64ccb008922a34be59432f785e9de353bfe86039034` |
| Runner source | `87fda5df09029af2892cde0bcda9b8ba973ea294cecb8ffc6bbaefea8eeb8add` |

## Results

| Check | Result |
|---|---|
| Manifest validation | PASS, 29 cases |
| Calibration fixed baseline | PASS / measured |
| Quality fixed baseline | PASS / measured |
| Deterministic guardrails | PASS, 10/10 |
| Real-model calibration | PASS, 18/18 runs |
| Real-model quality | NOT RUN |
| Hostile-input surveys | NOT RUN |
| `AGENT_QUALITY` | NOT RUN |

| Metric | Calibration baseline | Real-model calibration |
|---|---:|---:|
| Root-cause accuracy | 1.0000000000 | 1.0000000000 |
| Insufficient precision | 1.0000000000 | 1.0000000000 |
| Insufficient recall | 1.0000000000 | 1.0000000000 |
| Citation recall | 1.0000000000 | 1.0000000000 |
| Average tool calls | 4.1666666667 | 4.7777777778 |
| Maximum tool calls | 6 | 7 |
| Failure cases | 0 | 0 |
| Safety violations | 0 | 0 |

All six majority outcomes were correct. Every raw run completed without a provider error or safety violation.

## Threshold decision

ADR 0014 and `eval/v3/thresholds.json` freeze the quality gates before quality execution. Equality with a baseline is allowed only when both values are exactly 1.0; below the ceiling, strict improvement remains mandatory.

Credentials were injected only into the model subprocess environment. No credential value or raw provider response was persisted.

## Evidence

- Validation: `/tmp/cloudops-eval-v3-validate-b2a7c0d.json`
- Calibration baseline: `/tmp/cloudops-eval-v3-baseline-calibration-b2a7c0d.json`
- Quality baseline: `/tmp/cloudops-eval-v3-baseline-quality-b2a7c0d.json`
- Deterministic guardrails: `/tmp/cloudops-eval-v3-guardrails-b2a7c0d.json`
- Real-model calibration: `/tmp/cloudops-model-v3-calibration-b2a7c0d.json`
