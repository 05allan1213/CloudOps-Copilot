# Shared frontend system baseline

## Identity and scope

- Parent exemplar SHA: `8202f477ab695da04f33b13c1187607e97be7889`
- Commit subject: `feat(frontend): establish shared CPA workspace system`
- Shared owner: integration line (`main`)
- Dependencies, lockfile, Vite entry, API client, Router semantics, and backend: unchanged
- Build and dependency audit: `NOT RUN` because no dependency or build-entry change occurred
- External delivery: `NOT RUN`

## Read-only contracts for domain lanes

`frontend/src/styles/tokens.css` remains the single production source for raw
design values. Domain pages consume the semantic/component layer and must not
add a parallel theme. This baseline adds stable page gutter, section spacing,
toolbar, status-row, dense-list, icon, spinner, and accepted shadow tokens.

The shared workspace compositions are:

| Contract | Intended use |
| --- | --- |
| `WorkspacePageFrame` | Content, text, or full-width route frame without replacing page-specific composition |
| `WorkspaceHeader` | Page title, human-readable context, and primary actions |
| `ContextToolbar` | High-frequency filters, secondary controls, and a single primary action zone |
| `WorkspaceStatusRow` | Compact live, partial, warning, error, or progress statement with optional metadata/actions |
| `WorkspaceDenseList` | Keyboard-native two-line operational queue with selection and severity marker |
| `DenseDataTable` | Stable-field comparison only; optional columns, selection, virtualization, and exact-value copy |
| `WorkspaceInspector` | URL-owned detail surface with focus restoration and dirty-state protection |
| `WorkspaceTechnicalDetails` | Nuxt UI collapsible disclosure for UUID, hash, UTC, raw JSON, and copyable exact values |
| `CopyFeedbackButton` | Nuxt UI/Lucide copy action with Clipboard fallback and explicit success/failure announcement |
| `WorkspaceState` / `ApiErrorNotice` | Loading, skeleton, empty, partial, stale, disconnected, target-invalid, and API error states |
| `RealtimeTrustStatus` | SSE continuity claim; only the `live` state may assert current continuity |
| `WorkspaceOperationProgress` / `RiskConfirmation` | Long-operation and risk-class feedback without inventing backend authority |

All controls continue to use Nuxt UI primitives and Lucide icons. The shared
components do not own typed API calls, Router query codecs, SSE state, or domain
stores; those remain with their route/domain owners. A lane that needs a shared
change must report the consumer, minimum interface, proposed test, and whether
it blocks the lane instead of modifying integration-owned files.

## Exemplar equivalence

- `/overview` now consumes `WorkspacePageFrame`; its accepted page-specific
  structure, data flow, spacing, and interaction remain unchanged.
- `/agent` consumes the same frame and uses the shared copy-feedback control in
  Conversation and Evidence surfaces.
- `DenseDataTable` uses the same shared copy behavior while preserving its
  existing visible feedback row.
- Shell, Theme, Agent Store, Router, SSE lifecycle, and context Snapshot
  ownership were not changed.

## Focused validation

```bash
npx eslint <new shared TS/Vue files> --max-warnings 0
npx eslint <all modified TS/Vue files>
npx vitest run \
  src/api/agent.test.ts \
  src/components/agent/agentAccessibility.test.ts \
  src/components/overview/overviewModel.test.ts \
  src/components/workspace/workspacePresentation.test.ts \
  src/composables/useCopyFeedback.test.ts \
  src/composables/useRealtimeCleanup.test.ts \
  src/composables/useTheme.test.ts \
  src/composables/useWorkspaceInspector.test.ts \
  src/composables/useWorkspaceQuery.test.ts \
  src/router/routes.test.ts \
  src/router/scrollBehavior.test.ts \
  src/stores/agentWorkspace.test.ts \
  src/utils/agentContext.test.ts \
  src/utils/contextLink.test.ts
npm run typecheck
git diff --check
```

- New shared files lint: `PASS`, 0 warnings.
- All modified TS/Vue files lint: `PASS`, exit 0 and no errors. The accepted
  exemplar still reports 94 pre-existing template-format warnings; no warning
  remains in a newly added shared file.
- Focused Vitest: `PASS`, 14 files and 50 tests.
- Typecheck: `PASS`.
- Diff check: `PASS`.

## Chromium equivalence smoke

Runtime: real Chromium, `1440x900`, Light, frontend
`http://127.0.0.1:5173`, read-only backend
`http://127.0.0.1:18080`.

- `/overview`: `PASS`; all five typed sources rendered, refresh remained usable,
  and the Agent Dock opened without blocking the main page.
- Operations Atlas: `PASS`; one WebGL canvas rendered at approximately
  `1152x409`. Screenshot-pixel sampling found values from 16 through 254 and
  3,253 colored samples, so the canvas was not blank.
- Dock to `/agent`: `PASS`; the same Consultation, Snapshot, message stream,
  resource/time context, and Evidence remained selected.
- Agent History expansion: `PASS`.
- Shared copy action: `PASS`; the Owner message copy button completed through
  the shared control.
- Browser Console: `PASS`, 0 errors and 0 warnings.

No write command was issued. This focused route smoke does not prove complete
API, Provider, MySQL, or write-path behavior.

## Status

```text
SHARED_DESIGN_SYSTEM=PASS
EXEMPLAR_EQUIVALENCE=PASS
PARALLEL_BASELINE_READY=YES
FULL_VALIDATION=NOT RUN
WRITE_PATH_E2E=NOT RUN
BACKEND_GAP=NONE_FOR_SHARED_BASELINE
```
