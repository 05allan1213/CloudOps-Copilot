# Alerts / Incidents Lane Handoff

```text
LANE=Alerts/Incidents
BRANCH=frontend/final-alert-incidents
BASE_SHA=3559a8db55f6985f30de86cf16479a369708ca26
FINAL_SHA=recorded by git rev-parse HEAD after the local lane commit
IMPLEMENTATION=COMPLETE
FOCUSED_VALIDATION=PASS
FULL_VALIDATION=NOT RUN
REAL_FUNCTION_INTEGRATION=NOT RUN
```

## Scope

- `/alerts`: Alert lifecycle queue, high-frequency status tabs, progressive filters, typed error/loading states, Inspector context, Incident/Agent links, and existing expected-version plus Idempotency-Key command semantics.
- `/alerts/:alertId`: status-first Alert detail with progressive technical identity and compact Incident relationships.
- `/incidents`: lifecycle-oriented Incident work queue, URL-synchronised filtering/sorting/pagination, read-only Inspector, lifecycle stages, focus and scroll restoration.
- `/incidents/:incidentId`: existing full detail route retained and smoke-checked; Evidence, Approval, Delivery, Verification, Resolution, SSE and write safety remain server-owned.

## Modified production files

```text
frontend/src/components/alerts/AlertQueue.vue
frontend/src/components/incidents/IncidentLifecycle.vue
frontend/src/components/incidents/IncidentFilterBar.vue
frontend/src/components/incidents/IncidentInspector.vue
frontend/src/components/incidents/IncidentTable.vue
frontend/src/views/alerts/AlertDetailView.vue
frontend/src/views/alerts/AlertsView.vue
frontend/src/views/incidents/IncidentListView.vue
```

## Validation

`FOCUSED_VALIDATION=PASS`

- `npm run typecheck`: PASS.
- Focused Vitest: PASS, 8 files and 56 tests.
- Focused ESLint: PASS, 0 errors (27 existing/format warnings remain).
- `git diff --check`: PASS.
- Chromium `1440x900 Light` using fixture-backed `http://127.0.0.1:5176`:
  - `/incidents`: PASS. Queue rendered, selected URL updated, Inspector opened, seven lifecycle stages rendered, close restored URL and trigger focus, no horizontal overflow.
  - `/incidents/00000000-0000-4000-8000-000000000001`: PASS. Full detail rendered persisted Agent, Evidence, Approval, Delivery, Verification and Resolution sections.
  - `/alerts`: typed API error state rendered correctly, but fixture route is absent; see `BACKEND_GAP`.
  - `/alerts/00000012-0000-4000-8000-000000000001`: typed detail error state rendered correctly; no horizontal overflow.

## Backend gap

`BACKEND_GAP`: live `cloudops-api` returned HTTP 500 for `/api/v1/bootstrap`, `/api/v1/scopes`, `/api/v1/notifications`, `/api/v1/notification-events`, and `/api/v1/alerts`. The repository fixture server also has no `/api/v1/alerts` list/detail route and returned `RESOURCE_NOT_FOUND` (404). No static or mock Alert data was introduced to mask this.

`BACKEND_GAP` shared integration request: `WorkspaceInspector` Teleport content has no stacking z-index while `main.app-main` is `z-index: 1`; this caused close controls to be covered by the main content. This lane applies a local z-index integration override for Alerts/Incidents. The shared component should later own the canonical z-index.

## Boundaries

- Real Alert acknowledge/silence/expire-silence and Incident approval/delivery/verification writes: `NOT RUN`; no isolated target, restricted credentials, cleanup, or separate authorization.
- Full lint/unit/E2E/build, dark-mode and multi-viewport matrix, zoom, performance, large-data, staging/production validation: `NOT RUN` per the implementation plan.
- No Go backend, database, API contract, Provider, router semantics, or deployment behavior changed. No push, PR, publication, or external write performed.
