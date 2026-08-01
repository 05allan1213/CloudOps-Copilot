# Settings lane handoff

## Identity

- Branch: `frontend/final-settings`
- Worktree: `/home/monody/k8s/CloudOps-Copilot-final-settings`
- Base shared-system commit: `3559a8db55f6985f30de86cf16479a369708ca26`
- Fixed CPA reference: `7976b16f6c2fb957a050c0593e571c59dc836f9b`
- External delivery: `NOT RUN`

## Implementation

Settings now uses a CPA-aligned single-section workspace rather than a long
configuration page. It keeps legacy hashes (`#providers`, `#revision-history`),
Simple/Full display modes, search and section navigation, active Revision
summary/technical disclosure, Provider summary-to-detail editing, human units,
local drafts, diff, validation, revision preflight, conflict/rebase handling,
apply outcome, Secret hygiene, and leave protection.

Changed ownership files:

```text
frontend/src/views/settings/SettingsView.vue
frontend/src/views/settings/settingsWorkspace.ts
frontend/src/views/settings/settingsWorkspace.test.ts
frontend/src/views/settings/platformClient.test.ts
```

The existing `frontend/src/api/platform.test.ts` remains unchanged and keeps
the original `configurationDraft` test. No API, Router, SSE, or backend source
was changed.

## Focused validation

```bash
npm test -- --run src/views/settings/settingsDraft.test.ts \
  src/views/settings/settingsWorkspace.test.ts \
  src/views/settings/platformClient.test.ts \
  src/api/platform.test.ts
npx eslint src/views/settings/SettingsView.vue \
  src/views/settings/settingsWorkspace.ts \
  src/views/settings/settingsWorkspace.test.ts \
  src/views/settings/platformClient.test.ts --max-warnings 0
npm run typecheck
git diff --check
```

- Focused tests: `PASS`, 4 files and 15 tests.
- Responsibility-file ESLint: `PASS`.
- Typecheck: `PASS`.
- Diff check: `PASS`.
- Full frontend suite/build: `NOT RUN` by implementation cadence.

## Chromium smoke

Runtime: real Chromium, `1440x900`, Light, frontend
`http://127.0.0.1:5178`, read-only backend `http://127.0.0.1:18080`.

- `/settings`: `PASS`; one System section, search, Simple/Full tabs, active
  Revision summary, and disabled apply controls rendered.
- `/settings#providers`: `PASS`; hash navigation rendered the Provider summary
  list and selected Provider detail.
- Provider summary -> detail: `PASS`; Elasticsearch detail became selected and
  typed fields rendered.
- Draft interaction: `PASS`; System value and Revision summary produced a
  visible one-item diff and enabled validation.
- Browser Console: `PASS`, 0 errors and 0 warnings.
- `POST /api/v1/settings/validate`: `PASS` transport, result `FAIL` by real
  provider checks (`PROVIDER_UNAVAILABLE`: GitHub HTTP 401 and Argo connection
  failure). This was the only write-like request issued during smoke; no apply,
  Secret creation, or Provider test was issued.

## Boundaries

```text
IMPLEMENTATION=COMPLETE
FOCUSED_VALIDATION=PASS
FULL_VALIDATION=NOT RUN
REAL_FUNCTION_INTEGRATION=NOT RUN
WRITE_PATH_E2E=NOT RUN
BACKEND_GAP=POST /api/v1/configuration-revisions lacks atomic expected-active-revision compare-and-set
```

The backend apply contract accepts only `validation_id` and `draft`; the
frontend preserves the current typed contract and reports the missing
expected-active-revision compare-and-set as `BACKEND_GAP` rather than faking
client-side concurrency authority.
