# Accepted CPA exemplar baseline

## Identity

- Start HEAD: `e9555ea4620058fa36f8014585668d21bc92eb8d`
- Frozen HEAD: `8202f477ab695da04f33b13c1187607e97be7889`
- Local commit: `feat(frontend): freeze accepted CPA exemplar`
- Branch state after commit: `main...origin/main [ahead 30]`
- External delivery: `NOT RUN`

## Start inventory

`git status --short --branch`, `git rev-parse HEAD`, `git diff --stat`, and
`git diff --name-status` confirmed the Owner-approved dirty baseline contained
only these 13 modified files:

```text
frontend/src/components/agent/AgentConversation.vue
frontend/src/components/agent/AgentHistory.vue
frontend/src/components/agent/AgentInspector.vue
frontend/src/components/agent/GlobalAgentPanel.vue
frontend/src/components/layout/AppHeader.vue
frontend/src/components/layout/AppLayout.vue
frontend/src/components/layout/AppSidebar.vue
frontend/src/components/layout/SidebarMenu.vue
frontend/src/composables/useTheme.ts
frontend/src/style.css
frontend/src/styles/tokens.css
frontend/src/views/agent/AgentWorkspaceView.vue
frontend/src/views/overview/OverviewView.vue
```

The initial unstaged diff was 2,203 insertions and 1,255 deletions. Freeze-stage
changes were limited to the persisted-theme compatibility blocker and the
`UTooltip`/`Transition` non-element-root Console warning. The final cached diff
contained the same 13-file allowlist: 2,203 insertions and 1,254 deletions.

## Focused validation

```bash
git diff --check
npx vitest run \
  src/api/agent.test.ts \
  src/components/agent/agentAccessibility.test.ts \
  src/components/overview/overviewModel.test.ts \
  src/composables/useRealtimeCleanup.test.ts \
  src/composables/useTheme.test.ts \
  src/router/routes.test.ts \
  src/router/scrollBehavior.test.ts \
  src/stores/agentWorkspace.test.ts \
  src/utils/agentContext.test.ts \
  src/utils/contextLink.test.ts
npm run typecheck
```

- Diff check: `PASS`
- Focused Vitest: `PASS`, 10 files and 40 tests
- Typecheck: `PASS`
- Full lint/unit/E2E/build: `NOT RUN`

## Chromium smoke

The frontend was started on a free loopback port with
`VITE_API_PROXY_TARGET=http://127.0.0.1:18080`; the real Chromium CLI wrapper
opened `/overview` and `/agent` at `1440x900` with the persisted theme set to
Light.

- `/overview`: `PASS`, current typed API data rendered and the Agent Dock opened
  without blocking the main workspace.
- Dock to `/agent`: `PASS`, Consultation, Snapshot, messages, and page context
  remained continuous; History expansion remained interactive.
- Final browser Console: `PASS`, 0 errors and 0 warnings.
- Comprehensive real-function integration: `NOT RUN`.
- Write-path E2E: `NOT RUN`.

The repository fixture server did not implement three newer Overview reads and
returned fixture-only 404 responses for `/overview`, `/alerts`, and `/devops`.
The final smoke therefore used the already-running read-only loopback backend;
all probed reads returned HTTP 200. This is route-smoke evidence only and is not
promoted to Provider, MySQL, SSE-write, or release evidence.

## Status

```text
ACCEPTED_EXEMPLAR_BASELINE=PASS
OVERVIEW_IMPLEMENTATION=COMPLETE
AGENT_IMPLEMENTATION=COMPLETE
FOCUSED_VALIDATION=PASS
FULL_VALIDATION=NOT RUN
BACKEND_GAP=NONE_FOR_EXEMPLAR_SMOKE
```
