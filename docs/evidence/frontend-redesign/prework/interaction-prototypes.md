# Core Interaction Prototypes

Recorded: 2026-07-30 (Asia/Shanghai)

## Result

```text
INTERACTION_PROTOTYPES=PASS
CHROMIUM=PASS (9/9 total prototype tests)
FIREFOX=PASS (1/1 critical read-only flow)
WEBKIT=NOT RUN
WRITE_PATH_E2E=NOT RUN
```

The tests use deterministic isolated fixture state. They validate frontend ownership and truth-preserving behavior; they do not claim production backend write semantics or a completed UI migration.

## URL, History and Inspector

| Contract | Browser proof | Result |
| --- | --- | --- |
| Shareable state | Severity, page, time range and selected Incident survive direct entry and reload | PASS |
| Inspector open | Selection is reflected in the URL and focus enters the Slideover | PASS |
| Rapid selection | In-Inspector previous/next uses replace semantics rather than adding a history entry per row | PASS |
| Full work page | `/incidents/:incidentId` is a pushed routable surface, not transient Inspector state | PASS |
| Close/Back | Closing removes `selected`; Back closes/restores Inspector while preserving filter, page and scroll | PASS |
| Focus restoration | Close returns focus to the triggering row control | PASS |
| Dirty state | Closing opens a non-dismissible confirmation; Escape cannot discard edits | PASS |
| Old query | Legacy `incident` and `workload` parameters remain accepted while context is preserved | PASS |
| Invalid/deleted/denied | Each renders a distinct truthful state without clearing valid query context | PASS |

Temporary hover, menu and confirmation state is not placed in the URL. The production migration must add canonical sort/direction state because the current Incident table sort remains component-local; the prototype does not relabel that current defect as preserved behavior.

## SSE Lifecycle

The exceptional-state prototype covers and distinguishes:

```text
Connecting -> Live -> Reconnecting -> Disconnected -> Stale
           -> Cursor expired -> Resyncing -> Live | Resync failed
           -> Torn down
```

Duplicate cursor events increment an ignored counter and do not apply twice. Resync failure retains loaded context and the stale claim; only a successful bounded resync returns to Live. Teardown explicitly reports that the event source and listeners were disposed. The production Incident and Agent streams must preserve request identity, cursor de-duplication and ownership-specific cleanup.

## Settings Draft and Apply Truth

| Contract | Browser proof | Result |
| --- | --- | --- |
| Local draft | Edits remain local until explicit Apply | PASS |
| Validation | Missing revision summary and unsafe Provider protocol show field-level errors | PASS |
| Concurrent revision | Conflict disables Apply until the current revision is reloaded | PASS |
| Partial result | Per-item result states explicitly say atomic success was not claimed | PASS |
| Retry | Failed Provider item can be retried without implying independent Verification success | PASS |
| Leave protection | Navigation opens a non-dismissible discard dialog; Stay and Leave paths are deterministic | PASS |

The prototype does not invent a backend Draft resource or atomic transaction contract.

## Agent Desktop Degradation

Incident, Alert, Service, Scope, time range and Evidence remain present in the compact context strip at every supported desktop width.

| Viewport | Context rail | Evidence rail | Main task / overflow | Result |
| --- | --- | --- | --- | --- |
| 1920x1080 | Visible | Visible | Investigation dominant; no page overflow | PASS |
| 1440x900 | Visible | Collapsed | Evidence available through explicit control | PASS |
| 1280x800 | Visible | Collapsed | Main investigation remains usable | PASS |
| 1024x768 | Collapsed | Collapsed | Both available through labeled controls; no page overflow | PASS |

This is progressive desktop collapse, not a phone layout.

## Incident Ownership and Exceptional Truth

The complete Incident prototype presents Evidence, Approval, Delivery and Verification as one incident lifecycle and exposes no duplicate Approval, Delivery, execution or rollback command in DevOps. The fixture deliberately shows that `accepted`, `dispatched`, `observed` and `verified` are separate. A visible Verified stage is accompanied by `尚无当前 Verification 支持`, so no later state is inferred.

The state catalog covers Permission Denied, Partial, Stale, Disconnected, Expired Authority, Hash Changed, Provider Disagreement, Accepted-not-observed, Observed-not-verified and Verification Failed. Every state uses text plus a Lucide icon and semantic styling; valid Incident/request/trace context remains visible.

## Data Scale and Stability

The same browser pass verified 10,000 Logs, 2,500 Trace spans, 5,000 timeline entries and 20,000 table rows with fewer than 100 rendered DOM rows, full-value copying and stale-request cancellation. Layout checks passed at 1920, 1440, 1280 and 1024, 125%/150% browser zoom and 200% root text. Reduced-motion durations resolve at `0.00001s` without removing state meaning.

## Browser Evidence

Final Chromium JSON: 9 expected, 0 unexpected, 0 flaky. Firefox passed the critical Shell/theme/Inspector/Monitoring read-only flow. WebKit launch was attempted but host GTK, GStreamer, WebKit and media libraries are absent, therefore:

```text
WEBKIT=NOT RUN
```

This environment absence is not an application failure. Real business write paths remain `NOT RUN` because isolated credentials, target and cleanup guarantees were not provided.

## Evidence

- `prototypes/cloudops-prework/tests/contracts.spec.ts`
- `prototypes/cloudops-prework/tests/visuals.spec.ts`
- `prototypes/cloudops-prework/tests/cross-browser.spec.ts`
- `output/playwright/prototype/chromium-results.json`
- `output/playwright/prototype/firefox-results.json`
- `output/playwright/prototype/webkit-results.json`
- `output/playwright/prototype/metrics/agent-collapse.json`
- `output/playwright/prototype/review/index.json`
