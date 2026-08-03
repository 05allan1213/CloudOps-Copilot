# Gate 2 Browser Results

```text
B1=PASS
B2=PASS
B3=PASS
B4=NOT RUN
B5=PASS
B6=NOT RUN
B7=NOT RUN
B8=NOT RUN
REAL_READONLY_INTEGRATION=NOT RUN
WRITE_PATH_E2E=NOT RUN
OWNER_VISUAL_GATE_SHELL=PASS
```

## Runtime

- Application: `http://127.0.0.1:52731/incidents`
- Deterministic fixture: `http://127.0.0.1:18082`
- Tool: Playwright CLI, named session `gate2-shell-final`
- Browser: Chromium 1228
- Locale/timezone: `zh-CN`, `Asia/Shanghai`
- Motion: `prefers-reduced-motion: reduce`
- Data classification: `FIXTURE`

The Vite proxy made fixture requests same-origin. This browser run proves production Shell rendering and interaction against deterministic projections; it does not prove a real Provider-backed read chain or any real write side effect.

The Owner reviewed the retained Shell materials and returned `OWNER_VISUAL_GATE_SHELL=PASS` on 2026-07-31.

## Retained Captures

| File | Result |
| --- | --- |
| `shell-1920-light.png` | B2 expanded Light Shell |
| `shell-1920-dark.png` | B2 expanded Dark Shell |
| `shell-1440-light-final.png` | B1 final Light Shell and legacy Incident boundary |
| `shell-1440-dark-final.png` | B1 final Dark Shell and legacy Incident boundary |
| `owner-shell-review-1440-light.png` | Owner review frame with Header status, active navigation, and Notification badge |
| `shell-1280-light.png` | B3 expanded desktop degradation |
| `shell-1024-light.png` | B3 Light 64px rail |
| `shell-1024-dark.png` | B3 Dark 64px rail |
| `agent-1440-light.png` | B5 Agent Slideover, read-only consultation, and Focus surface |
| `double-overlay-1440-light.png` | B5 Notification above Agent; topmost overlay behavior |
| `skip-link-focus-1440-light.png` | B5 visible Skip Link and Focus ring |

Diagnostic artifacts retained outside the evidence commit payload:

- `output/playwright/gate2-shell-clean/gate2-agent-focus.webm` (23 MB keyboard/Focus recording)
- `output/playwright/gate2-shell-clean/.playwright-cli/traces/trace-1785433785598.trace`

## B1 and B2: Primary Shell and Theme

| Viewport | Theme | Sidebar | Overflow | Result |
| --- | --- | ---: | --- | --- |
| 1920x1080 | Light | 220px | none | `PASS` |
| 1920x1080 | Dark | 220px | none | `PASS` |
| 1440x900 | Light | 220px | none | `PASS` |
| 1440x900 | Dark | 220px | none | `PASS` |

Light and Dark retained equivalent information hierarchy and density. Switching theme persisted through reload. Sidebar preference also persisted through reload. The Header did not overlap route content and the document did not acquire a second scrollbar or page-level horizontal scroll.

The inspected route is still an explicit `LEGACY_ELEMENT_PLUS` Workspace behind the `MIGRATED_NUXT_UI` Shell. This is deliberate transition evidence, not an Incident migration claim.

## B3: Desktop Degradation

| Viewport | Theme | Sidebar | Result |
| --- | --- | ---: | --- |
| 1280x800 | Light | 220px | expanded Shell remains usable, Header does not overlap |
| 1024x768 | Light | 64px | rail mode, visible route content, no page-level overflow |
| 1024x768 | Dark | 64px | equivalent rail mode and route content |

The rail retained nine ordinary Workspace controls, the separate Agent pin, Owner identity, and collapse/expand control. Every icon-only link exposed an accessible name and a visible Tooltip. The 1024 viewport remained a desktop workflow; no Bottom Navigation or phone Drawer appeared.

## B5: Keyboard, Focus, History, and Overlay Contracts

- Provider health expands on mouse hover and keyboard Focus and identifies the fixture's available Provider truth. Other Provider health states were not rendered in this browser run.
- Activating Provider health opens `/settings#providers`; the asynchronously rendered anchor settled approximately 68px below the viewport top and the Settings H1 received route Focus.
- The Skip Link becomes visible on Focus and enters `main#main-content`.
- Direct route entry focuses the route H1. Back/Forward restored route state, and a recorded `scrollY=420` history position restored correctly.
- With Agent below Notification, the first Escape closed only Notification. The next Escape closed Agent and restored Focus to the original Agent trigger.
- Agent was idle while closed. Opening loaded only read endpoints and one consultation event stream. Closing left zero active Agent SSE connections.
- Notification read and read-all updated the deterministic projection while Notification SSE remained independent of the Agent lifecycle.
- Reduced motion retained state changes while removing visible transition timing.

The fixture-backed Notification POSTs are presentation/interaction evidence only. They are not B8 because no real persistence, Provider effect, isolated identity, or cleanup proof exists.

## Console and Requests

Final Console query:

```text
Total messages: 3
Errors: 0
Warnings: 0
```

Final relevant request inspection showed:

```text
GET /api/v1/notifications?limit=50 -> 200
GET /api/v1/bootstrap -> 200
GET /api/v1/scopes -> 200
GET /api/v1/incidents?limit=50 -> 200
GET /api/v1/settings -> 200
GET /api/v1/storage-status -> 200
GET /api/v1/agent/investigations?limit=100 -> 200
GET /api/v1/agent/consultations?limit=100 -> 200
GET /api/v1/knowledge-items?limit=100 -> 200
GET /api/v1/runbook-guidance -> 200
GET /api/v1/operation-plans?limit=100 -> 200
GET /api/v1/agent/consultations/<fixture-id> -> 200
GET /api/v1/agent/consultations/<fixture-id>/events -> 200
GET https://api.iconify.design/lucide.json?... -> 200
```

The local API identities above are fixture routes. The Iconify request is external static icon data, not CloudOps API or Provider traffic; it is retained for Gate 12 CSP/offline-delivery review.

## Deferred Matrix

- B4 zoom and 200% text: `NOT RUN`; later page Gates and Gate 12 own the complete matrix.
- B6 Firefox: `NOT RUN`.
- B6 WebKit: `NOT RUN`.
- B7 real UI -> API -> Provider read-only chain: `NOT RUN`.
- B8 isolated UI -> API -> persistence/Provider write and cleanup: `NOT RUN`.
