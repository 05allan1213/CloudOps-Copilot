# ADR 0005: StateDelta, Evidence and Deterministic Sufficiency

- Status: Accepted target decision; implementation NOT RUN
- Date: 2026-07-18
- Owner: Phase 4

## Context

The V2 Agent persists a whole GraphState, has broad legacy tool contracts and allows model output to influence coverage. That is not sufficient for durable, bounded and defensible Agent behavior.

## Decision

Eino provides typed Graph, ChatModel, Tool and callbacks only. MySQL, tasks, checkpoints, budgets, fencing, state transitions, Evidence validation and policy stay in project code.

The model returns StateDelta against a checkpoint version. A deterministic reducer validates scope, schema, Evidence ownership, tool/template parameters, duplicate signature and budget. A versioned deterministic sufficiency evaluator alone returns CONTINUE, READY_FOR_DIAGNOSIS or INSUFFICIENT_EVIDENCE.

Evidence is immutable typed fact with producer identity, provenance, trust axes, claim use, corroboration group, hashes and cycle ownership. External text is untrusted data and never authority. Model confidence cannot create a confirmed claim.

Eight read-only Tool contracts are frozen in Phase 4. Change tools use versioned fixtures/read adapters for Agent evaluation until Phase 5 validates live GitHub/Argo/revision integration.

## Consequences

- One async task advances one graph step.
- No Eino memory/checkpointer, DeepAgent, multi-Agent, shell or WebSearch.
- Incompatible V2 checkpoints are archived and restarted through investigation.start.
- Agent Quality is separate from Golden E2E.

## Rejected Alternatives

- Let the model rewrite full state or declare coverage sufficient.
- Persist provider raw responses or chain-of-thought.
- Test one fixed tool order as if it were dynamic investigation.

## Evidence Required

Reducer/property tests, checkpoint crash/replay, Evidence trust negatives, prompt-injection/secret/scope safety and frozen-dataset real-model evaluation.
