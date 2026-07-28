# Phase 4 Agent Quality v4 Report

- Status: `AGENT_QUALITY=PASS`
- Date: 2026-07-21
- Branch: `codex/v3-refactor`
- Exact evaluated source SHA: `f663915`
- Provider/model: `deepseek` / `deepseek-v4-flash`
- Dataset: `incident-agent-eval-v4`
- Aggregation: three independent runs per case, `majority_vote`

## Revision purpose

v4 adds an explicit exhaustive action-candidate contract. When a caller supplies a closed candidate set and the set is depleted, the model must stop insufficient unless deterministic sufficiency permits diagnosis. Production callers with dynamic tool selection leave the flag disabled. This repairs the v3 hostile-survey stop failure without rewriting provider output, leaking oracle answers, or weakening the production reducer.

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

## Formal quality gate

The quality report contains all 13 cases and 39 raw runs. Every run completed without a provider error or safety violation.

| Metric | Baseline | Threshold | Measured | Result |
|---|---:|---:|---:|---|
| Root-cause accuracy | 0.8750000000 | >= 0.90 and strict baseline win | 1.0000000000 | PASS |
| Insufficient precision | 0.8333333333 | >= 0.90 and strict baseline win | 1.0000000000 | PASS |
| Insufficient recall | 1.0000000000 | = 1.00; equality only at ceiling | 1.0000000000 | PASS |
| Citation recall | 0.7567567568 | >= 0.85 and strict baseline win | 0.8648648649 | PASS |
| Average tool calls | 3.7692307692 | <= 6.0 | 4.3076923077 | PASS |
| Maximum tool calls | 6 | <= 7 | 7 | PASS |
| Measured failures | n/a | 0 | 0 | PASS |
| Safety violations | 0 | 0 | 0 | PASS |

Formal quality gate status: `PASS`.

## Hostile-input surveys

| Survey | Runs | Terminal outcome | Failures | Safety violations | Result |
|---|---:|---|---:|---:|---|
| Prompt injection | 3/3 | insufficient evidence | 0 | 0 | PASS |
| Secret canary | 3/3 | insufficient evidence | 0 | 0 | PASS |

Every hostile run exposed all five frozen untrusted surfaces, made one final model decision, omitted further actions, and stopped insufficient. No canary or injection marker appeared in model output, errors, citations or trace metadata.

## Gate summary

| Evidence tier | Result |
|---|---|
| Manifest validation | PASS, 29 cases |
| Calibration | PASS, 18/18 runs |
| Deterministic guardrails | PASS, 10/10 |
| Formal quality | PASS, 39/39 runs |
| Prompt-injection survey | PASS, 3/3 |
| Secret-canary survey | PASS, 3/3 |
| `AGENT_QUALITY` | **PASS** |

`AGENT_QUALITY=PASS` removes the model-quality blocker for later gates. It does not establish Phase 3 clean-kind, Phase 5/6 live external-system gates, Golden E2E, Phase 7A cutover, Phase 7B cleanup or final DoD.

Credentials were injected only into model subprocess environments. No credential value or raw provider response was persisted.

## Evidence

- Validation: `/tmp/cloudops-eval-v4-validate-f663915.json`
- Quality baseline: `/tmp/cloudops-eval-v4-baseline-quality-f663915.json`
- Deterministic guardrails: `/tmp/cloudops-eval-v4-guardrails-f663915.json`
- Real-model quality: `/tmp/cloudops-model-v4-quality-f663915.json`
- Quality gate: `/tmp/cloudops-agent-quality-v4-gate-f663915.json`
- Prompt-injection survey: `/tmp/cloudops-model-v4-prompt-injection-f663915.json`
- Secret-canary survey: `/tmp/cloudops-model-v4-secret-canary-f663915.json`
