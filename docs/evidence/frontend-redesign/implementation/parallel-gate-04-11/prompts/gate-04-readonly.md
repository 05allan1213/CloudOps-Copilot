# Codex Prompt: Gate 4 Read-only Lane

Work only in `/home/monody/k8s/CloudOps-Copilot-g4` on branch
`frontend/g4-readonly`.

Fully read `AGENTS.md`, `/home/monody/k8s/vue.md`,
`docs/CloudOps-Frontend-Refactor-Plan.md`, the Gate 3 report, and this prompt.
Section 0.5 of the plan is the current execution authority. Implement Gate 4
completely: 404, Overview Command Center, Infrastructure, additive `/atlas`,
legacy Atlas Query compatibility, real topology, structured equivalent,
Inspector behavior, and Three.js lifecycle. Preserve all real API/Provider and
domain semantics; do not modify Go/backend/database/Kubernetes behavior.

Own only the Gate 4 files listed by the plan. This lane is the sole page lane
allowed to change `frontend/src/router/routes.ts`, and only for additive Atlas
and legacy Atlas compatibility. Do not modify `frontend/src/api/platform.ts` or
any other shared-owner file from section 0.5.4. Record a shared-change request
instead of crossing that boundary.

Prioritize implementation. Run only targeted lint, directly related focused
tests, one final `npm run typecheck`, and one Chromium 1440x900 Light smoke for
each changed route. Do not run the old B1-B8 matrix, full lint/unit/build/E2E,
performance soak, real backend integration, or intermediate Owner visual Gate.
Do not execute real writes.

Create
`docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/gate-04-readonly-handoff.md`
with base/final SHA, commits, files, routes, commands/results, shared requests,
and `NOT RUN` items. Make local lane-specific commits, leave the worktree clean,
report `IMPLEMENTATION=COMPLETE`, `FOCUSED_SMOKE=PASS`, and
`FULL_VALIDATION=DEFERRED`, then stop. Do not push or merge.
