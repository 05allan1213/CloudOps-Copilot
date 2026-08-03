# Codex Prompt: Gate 9-10 Incident And DevOps Lane

Work only in `/home/monody/k8s/CloudOps-Copilot-incident` on branch
`frontend/g9-g10-incident-devops`.

Fully read `AGENTS.md`, `/home/monody/k8s/vue.md`,
`docs/CloudOps-Frontend-Refactor-Plan.md`, the Gate 3 report, and this prompt.
Section 0.5 of the plan is the current execution authority. Implement Gate 9
Incident list/detail first, including the single incident lifecycle operation
surface, then implement Gate 10 DevOps ownership convergence. Preserve URL,
Inspector/detail, ZoneNav, SSE/backpressure, Evidence, Approval, Delivery,
Verification, Timeline, exact authority/hash/idempotency, global/non-incident
DevOps work, and compatible cross-links. Do not leave duplicate incident write
ownership in DevOps.

Own only the Gate 9-10 files listed by the plan. This lane owns the Incident to
DevOps semantic dependency, but not other page trees or shared-owner files from
section 0.5.4. Record shared needs in the handoff. Do not modify backend/API/
database/Provider/Kubernetes semantics. Do not execute real incident, approval,
delivery, rollback, termination, or DevOps writes.

Prioritize implementation. For each logical Gate run only targeted lint and
directly related focused tests; run one final `npm run typecheck` for the lane.
Run Chromium 1440x900 Light smoke for Incident list/detail and DevOps with one
core non-writing interaction and no blocking Console error. Do not run full
E2E/browser/performance/SSE soak or intermediate Owner visual Gates.

Create
`docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/gate-09-10-incident-devops-handoff.md`
with base/final SHA, commits, files, routes, commands/results, shared requests,
and `NOT RUN` items. Make local lane-specific commits, leave the worktree clean,
report `IMPLEMENTATION=COMPLETE`, `FOCUSED_SMOKE=PASS`, and
`FULL_VALIDATION=DEFERRED`, then stop. Do not push or merge.
