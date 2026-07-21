# ADR 0016: Runtime-Bound Agent Eval Revision

- Status: Accepted before v5 model execution
- Date: 2026-07-21
- Owner: Phase 4 / `AGENT_QUALITY`

## Context

`incident-agent-eval-v4` passed calibration, formal quality and both hostile-input surveys. Later commit `872af1c` changed the production Agent runtime to persist provider, model, prompt and tool-schema identity, hash provider request IDs and fail closed when a resumed run has a different identity. Those changes did not alter the frozen v4 dataset or deterministic sufficiency policy, but they made the v4 exact-source evidence historical rather than current-source proof.

The previous manifest bound dataset, oracle, split, metrics, tool contracts, prompt material, reducer and Eval runner. It did not bind the production investigation, provider assembly, worker configuration or model-identity migration paths that now affect a real Agent run.

## Decision

Freeze `incident-agent-eval-v5` as a new immutable revision.

- Preserve the v4 cases, fixtures, oracles, split membership, metric method, exhaustive-candidate stop contract and threshold values.
- Mechanically update only the dataset/scope revision identity required for a new frozen revision.
- Extend the manifest to schema v2 with `runtime_source_sha256`.
- Bind the production investigation start/step/registry, worker/provider assembly, model configuration and migration `00013_agent_run_model_identity.sql` into that runtime hash.
- Keep schema v1 manifests loadable so v1-v4 remain independently verifiable historical evidence.

The v4 deterministic policy repair remains the active policy. v5 does not tune the oracle, weaken a Gate or reinterpret an earlier result; it restores exact-source parity after a production-runtime change.

## Frozen thresholds

Because v5 preserves the v4 dataset, oracle, deterministic policy, aggregation and metric method, it preserves the already-frozen numerical thresholds and ceiling rule:

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

- Frozen revision commit: `33e1f1cb3540e6ecef10885f441e463414833757`
- Manifest: `eval/v5/manifest.json`
- Thresholds: `eval/v5/thresholds.json`
- Validation: `/tmp/cloudops-eval-v5-validate-33e1f1c.json`
- Calibration baseline: `/tmp/cloudops-eval-v5-baseline-calibration-33e1f1c.json`
- Quality baseline: `/tmp/cloudops-eval-v5-baseline-quality-33e1f1c.json`
- Deterministic guardrails: `/tmp/cloudops-eval-v5-guardrail-33e1f1c.json`
- Real-model calibration: `/tmp/cloudops-model-v5-calibration-33e1f1c.json`
- Real-model quality: `/tmp/cloudops-model-v5-quality-33e1f1c.json`
- Formal quality gate: `/tmp/cloudops-agent-quality-v5-gate-33e1f1c.json`
- Prompt-injection survey: `/tmp/cloudops-model-v5-prompt-injection-33e1f1c.json`
- Secret-canary survey: `/tmp/cloudops-model-v5-secret-canary-33e1f1c.json`
