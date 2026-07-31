# Codex Prompt: Gate 8 Agent Lane

Work only in `/home/monody/k8s/CloudOps-Copilot-agent` on branch
`frontend/g8-agent`.

Fully read `AGENTS.md`, `/home/monody/k8s/vue.md`,
`docs/CloudOps-Frontend-Refactor-Plan.md`, the Gate 3 report, and this prompt.
Section 0.5 of the plan is the current execution authority. Implement Gate 8
completely: History/Conversation/Inspector workspace, all three entry modes,
structured context, Global Agent/full-workspace ownership, Agent/Knowledge/
plan/action-card behavior, exact hash/authority/idempotency, Tool/Evidence
states, stream reconnect/cancel/dedup/teardown, long content, and collapsible
desktop panels. Preserve every real Agent capability.

Own only the Gate 8 files listed by the plan. Use current compatible Overview,
Alert, and Incident context contracts; integration will close cross-lane links.
Do not modify shared-owner files from section 0.5.4. Record shared needs in the
handoff. Do not modify backend/API semantics or execute Agent writes.

Prioritize implementation. Run only targeted lint, directly related focused
tests, one final `npm run typecheck`, and one Chromium 1440x900 Light smoke of
the `/agent` route with one core interaction and no blocking Console error. Do
not run multi-viewport/SSE soak/full E2E/browser/accessibility validation or an
intermediate Owner visual Gate.

Create
`docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/gate-08-agent-handoff.md`
with base/final SHA, commits, files, routes, commands/results, shared requests,
and `NOT RUN` items. Make local lane-specific commits, leave the worktree clean,
report `IMPLEMENTATION=COMPLETE`, `FOCUSED_SMOKE=PASS`, and
`FULL_VALIDATION=DEFERRED`, then stop. Do not push or merge.
