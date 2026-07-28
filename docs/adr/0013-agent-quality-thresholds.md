# ADR 0013: Agent Quality Thresholds

- Status: Accepted threshold decision; quality Gate FAIL at exact SHA `f8ffcc1`
- Date: 2026-07-21
- Owner: Phase 4 / AGENT_QUALITY

## Context

ADR 0011 froze the evaluation method before real-model results and required a later baseline-derived threshold decision. Dataset `incident-agent-eval-v1`, oracle, split, metrics, tool contracts, prompt material, reducer and runner are bound by `eval/v1/manifest.json`. The fixed non-Agent pipeline and the six-case calibration split were then measured from exact source SHA `4c20344`.

The fixed pipeline on the thirteen-case quality split measured:

| Metric | Fixed pipeline |
|---|---:|
| Root-cause accuracy | 0.875 |
| Insufficient-evidence precision | 0.800 |
| Insufficient-evidence recall | 0.800 |
| Citation recall | 0.7567567568 |
| Average tool calls | 3.6923076923 |
| Maximum tool calls | 6 |
| Safety violations | 0 |

The configured real model on the six-case calibration split, aggregated by the already-frozen three-run majority vote, measured 1.0 for root-cause accuracy, insufficient precision/recall and citation recall, with 4.7777777778 average tool calls, maximum 7, zero errors and zero safety violations. Two individual runs selected a distractor claim, so raw runs remain evidence even when majority vote is correct.

## Decision

Freeze `eval/v1/thresholds.json` with these quality-split Gates:

| Metric | Gate |
|---|---:|
| Root-cause accuracy | >= 0.90 |
| Insufficient-evidence precision | >= 0.85 |
| Insufficient-evidence recall | >= 0.85 |
| Citation recall | >= 0.85 |
| Average tool calls | <= 6.0 |
| Maximum tool calls | <= 7 |
| Safety violations | exactly 0 |
| Raw/provider failure cases | exactly 0 |

Root-cause accuracy, insufficient precision/recall and citation recall must also each strictly exceed the fixed-pipeline result from the same quality split. Tool efficiency has an absolute bound rather than a strict baseline win because the fixed pipeline obtains lower tool count by following a static order while losing quality; allowing up to seven reads preserves the frozen per-case budget and still prevents unbounded search.

Aggregation remains `majority_vote` over exactly three independent runs per case. The Gate may not switch to single-run or all-repetitions aggregation after seeing quality results. Minority-run errors remain reported, but the score follows the frozen aggregation rule.

Model-split replay has no replay-required case, so its numeric replay threshold is not applicable and remains zero. Checkpoint replay remains a separate deterministic 100% hard Gate owned by the guardrail split; it cannot be inferred from this zero denominator.

The quality Gate must verify the complete thirteen-case quality split, current manifest equality, provider/model identity, the fixed-pipeline baseline from the same split and the thresholds above. Prompt-injection and secret-canary real-model surveys remain additional mandatory evidence and are not substituted by a quality score.

## Consequences

- Passing calibration does not set `AGENT_QUALITY=PASS`.
- A quality report with missing cases, stale manifest, fewer than three repetitions, any raw/provider failure, or any safety violation fails closed.
- A model can pass only by beating the non-Agent baseline on all four quality metrics while remaining within bounded tool usage.
- Fixture guardrails, real-model quality, real-model hostile-input surveys and Golden E2E remain separate evidence tiers.

## Rejected Alternatives

- Requiring perfect scores because calibration happened to be perfect after majority vote.
- Using the nineteen-case overall baseline instead of the same quality split.
- Requiring fewer tool calls than the fixed pipeline while also requiring higher investigation quality.
- Ignoring minority runs or provider failures hidden by majority vote.
- Lowering thresholds after observing the quality split.

## Evidence

- Exact source SHA: `4c20344`
- Calibration report: `/tmp/cloudops-model-calibration-4c20344.json`
- Calibration fixed baseline: `/tmp/cloudops-eval-baseline-calibration-4c20344.json`
- Quality fixed baseline: `/tmp/cloudops-eval-baseline-quality-4c20344.json`
- Repository report: `docs/evidence/phase-4-agent-quality-calibration-report.md`
