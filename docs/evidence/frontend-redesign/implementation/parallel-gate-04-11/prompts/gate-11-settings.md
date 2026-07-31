# Codex Prompt: Gate 11 Settings Lane

Work only in `/home/monody/k8s/CloudOps-Copilot-settings` on branch
`frontend/g11-settings`.

Fully read `AGENTS.md`, `/home/monody/k8s/vue.md`,
`docs/CloudOps-Frontend-Refactor-Plan.md`, the Gate 3 report, and this prompt.
Section 0.5 of the plan is the current execution authority. Implement Gate 11
completely: Provider, Scope, system, policy, secret reference, revision/history,
storage, per-section frontend drafts, validation, change summaries, explicit
apply, concurrent revision, truthful partial result/retry, leave protection,
secret non-disclosure, and `#providers` behavior. Preserve all current Settings
capabilities and real typed client semantics.

Own the Gate 11 files listed by the plan and act as the only page-lane owner of
`frontend/src/api/platform.ts`. Do not change its backend semantics. Do not
modify other shared-owner files from section 0.5.4; record shared needs in the
handoff. Do not execute validate/apply/secret/provider-test writes against a
real environment.

Prioritize implementation. Run only targeted lint, directly related focused
tests, one final `npm run typecheck`, and one Chromium 1440x900 Light smoke of
`/settings` with one core non-writing interaction and no blocking Console error.
Do not run zoom/multi-viewport/full browser/E2E/accessibility validation or an
intermediate Owner visual Gate.

Create
`docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/gate-11-settings-handoff.md`
with base/final SHA, commits, files, routes, commands/results, shared requests,
and `NOT RUN` items. Make local lane-specific commits, leave the worktree clean,
report `IMPLEMENTATION=COMPLETE`, `FOCUSED_SMOKE=PASS`, and
`FULL_VALIDATION=DEFERRED`, then stop. Do not push or merge.
