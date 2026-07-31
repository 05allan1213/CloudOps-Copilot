# Gate 4-11 Parallel Implementation Baseline

Status: `OWNER_APPROVED`

This directory is the handoff surface for the six independent frontend
implementation worktrees authorized on 2026-07-31. Product scope is unchanged;
only implementation-time validation is reduced. The governing rules are in
section 0.5 of `docs/CloudOps-Frontend-Refactor-Plan.md`.

## Baseline

- Gate 3 implementation: `0b1c6d5c518746d197712e6b6574228d07056471`
- Parallel rules baseline: the local commit containing this file and the six
  prompt files below
- Integration worktree: `/home/monody/k8s/CloudOps-Copilot`
- External writes: prohibited
- Real write integration: `NOT RUN` unless the isolation contract and separate
  Owner authorization are both present

## Worktrees

| Lane | Branch | Worktree | Prompt | Handoff |
| --- | --- | --- | --- | --- |
| Gate 4 read-only | `frontend/g4-readonly` | `/home/monody/k8s/CloudOps-Copilot-g4` | `prompts/gate-04-readonly.md` | `gate-04-readonly-handoff.md` |
| Gate 5-6 telemetry | `frontend/g5-g6-telemetry` | `/home/monody/k8s/CloudOps-Copilot-telemetry` | `prompts/gate-05-06-telemetry.md` | `gate-05-06-telemetry-handoff.md` |
| Gate 7 alerts | `frontend/g7-alerts` | `/home/monody/k8s/CloudOps-Copilot-alerts` | `prompts/gate-07-alerts.md` | `gate-07-alerts-handoff.md` |
| Gate 8 agent | `frontend/g8-agent` | `/home/monody/k8s/CloudOps-Copilot-agent` | `prompts/gate-08-agent.md` | `gate-08-agent-handoff.md` |
| Gate 9-10 incident/devops | `frontend/g9-g10-incident-devops` | `/home/monody/k8s/CloudOps-Copilot-incident` | `prompts/gate-09-10-incident-devops.md` | `gate-09-10-incident-devops-handoff.md` |
| Gate 11 settings | `frontend/g11-settings` | `/home/monody/k8s/CloudOps-Copilot-settings` | `prompts/gate-11-settings.md` | `gate-11-settings-handoff.md` |

Each Codex window must use exactly one worktree. The integration worktree must
remain free of page implementation until branches are handed off.

## Lane Delivery Contract

Each lane must:

1. Preserve every capability and backend/domain contract assigned by the plan.
2. Stay inside its file ownership boundary.
3. Run targeted lint, focused tests, one final `npm run typecheck`, and one
   Chromium 1440x900 Light smoke per changed route.
4. Create its unique handoff file with base/final SHA, commits, files, routes,
   commands, results, shared-change requests, and honest `NOT RUN` items.
5. Commit all lane-owned changes locally and leave a clean worktree.
6. Stop without push, PR, publication, backend changes, real writes, or broad
   validation.

The lane completion state is:

```text
IMPLEMENTATION=COMPLETE
FOCUSED_SMOKE=PASS
FULL_VALIDATION=DEFERRED
```

## Local Dependencies And Ports

Worktrees intentionally do not share the integration worktree's
`frontend/node_modules`. A lane may run `npm ci` inside its own `frontend/`
when dependencies are absent. Do not symlink mutable dependency caches between
concurrent worktrees. Suggested dev-server ports are 52741 through 52746 in the
same order as the table above.

## Integration Contract

The integration window merges only clean, completed branches with
`git merge --no-ff`. It owns shared-file changes, regenerated declarations,
cross-lane links, Gate 12A cleanup, real frontend/backend read-only integration,
and the final Owner URL. Gate 12B full validation remains separately authorized
future work.
