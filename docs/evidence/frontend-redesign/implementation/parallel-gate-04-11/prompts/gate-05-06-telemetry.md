# Codex Prompt: Gate 5-6 Telemetry Lane

Work only in `/home/monody/k8s/CloudOps-Copilot-telemetry` on branch
`frontend/g5-g6-telemetry`.

Fully read `AGENTS.md`, `/home/monody/k8s/vue.md`,
`docs/CloudOps-Frontend-Refactor-Plan.md`, the Gate 3 report, and this prompt.
Section 0.5 of the plan is the current execution authority. Implement Gate 5
Monitoring first, then Gate 6 Logs and Traces. Preserve guided/expert queries,
URL state, cancellation, histories, Provider context, Evidence/Consultation,
uPlot, semantic Trace rendering, raw copy, and virtualization. Implement every
assigned capability even though validation is intentionally light.

Own only the Gate 5-6 files listed by the plan. Keep telemetry models and
specialist adapters inside this lane. Do not modify shared-owner files from
section 0.5.4; record any required shared change separately. Do not modify
Go/backend/API semantics, database, Provider, or Kubernetes behavior.

Prioritize implementation. For each logical Gate run only targeted lint,
directly related focused tests, then run one final `npm run typecheck` for the
lane. Run one Chromium 1440x900 Light smoke for each changed route, covering one
core interaction and blocking Console errors. Do not run full lint/unit/build/
E2E, the browser matrix, data-scale/performance suites, real integration, or
real Evidence/Consultation/definition/authorization writes.

Create
`docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/gate-05-06-telemetry-handoff.md`
with base/final SHA, commits, files, routes, commands/results, shared requests,
and `NOT RUN` items. Make local lane-specific commits, leave the worktree clean,
report `IMPLEMENTATION=COMPLETE`, `FOCUSED_SMOKE=PASS`, and
`FULL_VALIDATION=DEFERRED`, then stop. Do not push or merge.
