# ADR 0011: Eval, Hard Gates and Claim Safety

- Status: Accepted method decision; thresholds and live results NOT RUN
- Date: 2026-07-18
- Owner: Phase 2-7

## Context

Unit tests, fixture Agent tests, real-model quality, provider integration and Golden E2E prove different things. Historical evidence or one successful model run cannot safely support V3 claims.

## Decision

Keep four evidence tiers:

~~~text
PR_FAST
PR_INTEGRATION
MAIN
MANUAL_GOLDEN
~~~

Real MySQL tests own queue/lease/state invariants. Adapter contracts own provider bounds. Agent Eval owns investigation quality and safety. Golden E2E owns the exact integrated flow.

Safety violations must remain zero: write-tool use, scope escape, secret/canary leak, prompt-injection obedience, foreign/nonexistent Evidence, unsupported confirmed claim, invalid signature and budget overrun.

Dataset, oracle, metric implementation and split are frozen and hashed before a real-model baseline. A later Phase 4 threshold ADR sets accuracy/insufficient/citation/tool-efficiency thresholds and aggregation method without backfilling numbers. Each stochastic case runs at least three times.

Every claim is gated by current exact source/image/model/prompt/tool/data hashes. Missing credentials/resources are NOT RUN and block the owning Gate.

## Consequences

- Fixture PASS never substitutes for Agent Quality or Golden E2E.
- Old SHA evidence is never reused for a new commit.
- kind Demo is not production, HA or an SLO.
- Measured values are reported only after actual measurement.

## Rejected Alternatives

- One global test score.
- LLM judge as the only evaluator.
- Set attractive quality thresholds before baseline.
- Soften NOT RUN into partial success.

## Evidence Required

Per-Gate command/result manifest, deterministic safety suite, real-model repeated runs, non-Agent baseline comparison and exact-SHA Golden evidence.
