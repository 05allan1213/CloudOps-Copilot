# Gate 3 Browser Results

```text
B1=PASS
B2=NOT RUN
B3=PASS
B4=PASS
B5=PASS
B6=NOT RUN
B7=NOT RUN
B8=NOT RUN
REAL_READONLY_INTEGRATION=NOT RUN
WRITE_PATH_E2E=NOT RUN
DATA_CLASSIFICATION=FIXTURE_PRESENTATION_EVIDENCE
```

## Runtime

- Page: `http://127.0.0.1:52732/workspace`
- Tool: Playwright CLI, session `gate3-workspace`
- Browser: Chromium 1228
- Locale/timezone: `zh-CN`, `Asia/Shanghai`
- Motion: `prefers-reduced-motion: reduce`
- Fixture: imports real production Gate 3 components, registers no production
  Workspace route, and makes no CloudOps API or Provider request

This run proves the shared components' presentation and interaction contracts.
It is not real read-only or write integration evidence.

## Retained Captures

| File | Result |
| --- | --- |
| `b1-states-1440-light.png` | Final Light exceptional states, realtime state, and confirmation commands |
| `b1-states-1440-dark.png` | Final Dark equivalent hierarchy and status meaning |
| `b1-async-lifecycle-light.png` | Background refresh, submit loading/input retention, and cancellable long-operation presentation |
| `b3-table-1280-light.png` | 20,000-row virtual table and desktop degradation |
| `b3-inspector-1024-light.png` | 1024 desktop Inspector push layout |
| `b4-zoom-125.png` | 125% zoom degradation |
| `b4-zoom-150.png` | 150% zoom degradation |
| `b4-text-200-risk-modal.png` | 200% text risk Modal after overlay-layer fix |
| `b4-text-200-inspector.png` | 200% text long-title Inspector and fact layout |

## B1: Light, Dark, Console, and Network

- Light and Dark retain the same information hierarchy, compact density,
  focus semantics, and state labels.
- Loading, Empty, Error, Partial, Stale, Disconnected, Permission Denied,
  Expired, Invalid target, and Deleted target were rendered through the shared
  production compositions.
- Realtime presentation exercised Connecting, Live, Reconnecting,
  Disconnected, Stale, Cursor expired, Resyncing, Resync failed, and Stopped.
  Only Live exposed the live class and claim.
- Acknowledgement, Configuration, Approval, Rollback, and Forced termination
  opened distinct confirmation content and required facts.
- During background refresh, revision 12 stayed visible, the refresh status was
  announced, and the button was disabled; completion replaced it with revision
  13. During simulated submit, the button was disabled and its Nuxt UI loading
  state was visible. The exact edited input remained after the expected failure
  and the inline error appeared. Its generic code remained visible, while no
  unsupported idempotent-replay claim was rendered.
- The long-operation composition showed stage `等待 Provider observed`,
  elapsed `00:00:42`, and Cancel only because cancellation was declared. After
  activation it showed `取消已请求` and explicitly did not infer that
  the Provider had stopped.
- Final Console result: 2 informational messages, 0 errors, 0 warnings.
- Final Network result: successful static/Vite resources plus Iconify
  Lucide HTTP 200 responses. Failed requests: 0. CloudOps API/Provider requests:
  0.

## B3: Desktop Degradation and Scale

| Viewport | Surface | Page overflow | Result |
| --- | --- | ---: | --- |
| 1280x800 | 20,000-row dense table | 0px | `PASS` |
| 1024x768 | table plus pushed Inspector | 0px | `PASS` |

- Virtual body rows ranged from 24 to 37 across the tested viewports, below the
  required 100-row boundary for the 20,000-row source.
- Measured row height was approximately 49.9px against the 48px density target.
- The Inspector begins at the 56px Header boundary and never covers the Header.
- Optional column choices persist in LocalStorage and do not change the URL.
- Critical columns remain pinned. Long values are fully copyable.

## B4: Zoom, Text Enlargement, and Long Values

- Chromium 125% and 150% zoom retained the main task without page-level
  horizontal overflow or overlapping controls.
- At 1024x768 with root text enlarged to 32px (200%), the forced-termination
  Modal measured 620x640 at `y=64`; its title was fully contained at
  `y=96..144`.
- The Modal content layer used `z-index: 101`, above the sticky Header at 40.
  The body had `clientHeight=302`, `scrollHeight=837`, and `overflow-y=auto`.
  The footer remained visible at `y=568..704`.
- Modal page overflow and dialog overflow were both 0. Target, Effect,
  Authority, Version, and irreversible-consequence label/value rectangles did
  not intersect.
- The 200% long-title Inspector measured 460x712 at the 56px Header boundary.
  Its title remained fully contained, body had `clientHeight=277` and
  `scrollHeight=936`, and footer remained visible at `y=552..768`.
- Operational Scope, Provider, Observed UTC, and Exact hash label/value
  rectangles did not intersect. Page and Inspector horizontal overflow were 0.
- Long error code, request ID, trace ID, resource name, hash, Chinese copy, and
  exact UTC all wrapped or remained available without incoherent overlap.

## B5: Keyboard, Focus, History, and Motion

- Enter and Space activate a focused table row and open its Inspector.
- First Inspector open pushes a shareable `selected` URL. Rapid target switching
  replaces the same history entry. Full work pushes its own route.
- Back closes the Inspector and restores the list URL, table scroll, document
  scroll, and trigger Focus. Forward restores the selection.
- Direct selected URLs and invalid/deleted/denied/expired URLs retain their
  explicit target rather than selecting the first row.
- Inspector entry Focus lands on the H2. Dirty state blocks outside/Escape/close
  dismissal until the explicit discard dialog is resolved.
- With a confirmation above the Inspector, Escape closes only the topmost
  dismissible surface and later restores the correct trigger.
- Skip Link remains keyboard visible. Reduced motion removes spinner animation
  while retaining the connecting/resyncing state text and icon.

## Deferred Matrix

- B2 1920x1080: `NOT RUN`; not required for the focused Gate 3 exit.
- B6 Firefox: `NOT RUN`.
- B6 WebKit: `NOT RUN`.
- B7 real UI -> API -> Provider: `NOT RUN`; no real request was made.
- B8 isolated UI -> API -> persistence/Provider plus cleanup: `NOT RUN`; the
  required isolation, identity, cleanup, and authorization are absent.
