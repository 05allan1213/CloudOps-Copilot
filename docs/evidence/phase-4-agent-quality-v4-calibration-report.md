# Phase 4 Agent Quality v4 Calibration Report

- Status: `CALIBRATION_PASS`; `AGENT_QUALITY=NOT_RUN`
- Date: 2026-07-21
- Branch: `codex/v3-refactor`
- Exact evaluated source SHA: `66c1e24`
- Provider/model: `deepseek` / `deepseek-v4-flash`
- Dataset: `incident-agent-eval-v4`
- Aggregation: three independent runs per case, `majority_vote`

## Frozen identity

| Material | SHA-256 |
|---|---|
| Dataset | `c114dbd44a90e02e770c430a724405d396da7f89888bbde34fe91dd28624fd2e` |
| Oracle | `dc184a0a643575347b96a96a61a90e42219e54cd999ab0498dfdd73e2b589cc0` |
| Split | `5fbb977feaac83259902f5719011ef482c315daa070ba2254f455e22d1567d56` |
| Metric spec | `0a1c403efc90872b2af0e06debb864d441bbbf78a0126163424bcf43ddbbb2a4` |
| Tool contracts | `7a197f83ba6cd0b926697732e48af879324351643af10b1ad3d5fb92b0179e1d` |
| Prompt material | `94badd947e0709f8b86df0ae495406510424b38a41597855a8648159c23d3afe` |
| Reducer source | `e90ebf37bda8c2b88845d64ccb008922a34be59432f785e9de353bfe86039034` |
| Runner source | `ce39d0b7ada09e1ea6c8d4c1dcd8bd484a5daa83c453312d5261e62e98597a07` |

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
| Average tool calls | 4.1666666667 | 4.8333333333 |
| Maximum tool calls | 6 | 7 |
| Failure cases | 0 | 0 |
| Safety violations | 0 | 0 |

All raw runs completed without provider errors. Credentials were injected only into the model subprocess environment; no credential value or raw provider response was persisted.

## Evidence

- Validation: `/tmp/cloudops-eval-v4-validate-66c1e24.json`
- Calibration baseline: `/tmp/cloudops-eval-v4-baseline-calibration-66c1e24.json`
- Quality baseline: `/tmp/cloudops-eval-v4-baseline-quality-66c1e24.json`
- Deterministic guardrails: `/tmp/cloudops-eval-v4-guardrails-66c1e24.json`
- Real-model calibration: `/tmp/cloudops-model-v4-calibration-66c1e24.json`
