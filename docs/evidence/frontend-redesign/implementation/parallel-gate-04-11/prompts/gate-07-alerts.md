# Codex Prompt: Gate 7 Alerts Lane

Work only in `/home/monody/k8s/CloudOps-Copilot-alerts` on branch
`frontend/g7-alerts`.

Fully read `AGENTS.md`, `/home/monody/k8s/vue.md`,
`docs/CloudOps-Frontend-Refactor-Plan.md`, the Gate 3 report, and this prompt.
Section 0.5 of the plan is the current execution authority. Implement Gate 7
completely: Alerts list/detail, Inspector scanning, URL and deep-link behavior,
new-row control, legacy `workload` input normalization, canonical `resource`
output, expected version/idempotency/request identity, and all current Alert
commands and Incident/Provider links. Preserve real behavior; do not replace it
with fixtures or static interactions.

Own only the Gate 7 files listed by the plan. Use existing compatible Incident
and Agent links until integration; do not edit their page trees. Do not modify
shared-owner files from section 0.5.4. Record any required shared change in the
handoff. Do not modify backend/API semantics or execute real Alert writes.

Prioritize implementation. Run only targeted lint, directly related focused
tests, one final `npm run typecheck`, and Chromium 1440x900 Light smoke for the
list and detail routes with one core interaction and no blocking Console error.
Do not run the full browser/E2E/performance/accessibility matrix or intermediate
Owner visual review.

Create
`docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/gate-07-alerts-handoff.md`
with base/final SHA, commits, files, routes, commands/results, shared requests,
and `NOT RUN` items. Make local lane-specific commits, leave the worktree clean,
report `IMPLEMENTATION=COMPLETE`, `FOCUSED_SMOKE=PASS`, and
`FULL_VALIDATION=DEFERRED`, then stop. Do not push or merge.
