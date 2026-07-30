# Current Production Baseline Recovery

## Result

```text
PRODUCTION_BASELINE_RECOVERY=PASS
STABLE_READONLY_E2E=PASS
WRITE_PATH_E2E=NOT RUN
```

The repair stayed on the current Vue/Element Plus stack. It did not add Nuxt UI, Tailwind, uPlot or TanStack Virtual to production dependencies and did not change backend semantics.

## Repairs

| Area | Repair | Regression protection |
| --- | --- | --- |
| Monitoring dialog | Registered `ElDialog` and CSS; restored modal role, focus entry/trap/restore, Escape/cancel and Light/Dark behavior | Stable test IDs plus real-browser dialog checks and screenshots |
| Monitoring ARIA | Replaced invalid tab semantics with tab/tabpanel ownership | Browser accessibility-role assertions |
| Route focus | Made main content programmatically focusable and handled asynchronous H1/leave-transition race | Incident navigation and Back focus tests |
| Global Agent | Delayed index/detail reads and Consultation SSE until the drawer or `/agent` owns them | Closed drawer sends no `/api/v1/agent/*`; Notification SSE remains live |
| Settings | Moved summary/action breakpoint to avoid 767/768/769 clipping | Screenshots and overflow measurements at 767, 768, 769 and 1024 |
| Provider links | Reused `safeExternalURL` for Monitoring, Logs and Traces | Unit coverage for HTTP/HTTPS and rejection of `javascript:`, `data:`, `vbscript:` and malformed values |
| Atlas | Removed `preserveDrawingBuffer: true` | Real WebGL context shows `preserveDrawingBuffer=false`; screenshot and prototype lifecycle checks |
| Incident test hooks | Added stable filter/result/row IDs without changing API or URL semantics | Accessible-role/data-testid Playwright contracts |
| E2E types | Added `frontend/tsconfig.e2e.json`, `typecheck:e2e` and `@types/node` | Local script, Make target and CI step |
| Warning policy | Added `lint:no-new-warnings` with baseline maximum 2608 | Current result 2564; CI prevents growth without mass-formatting historical debt |
| Browser gate | Rebuilt fixture server around current Bootstrap, Scope, Notification and typed Incident projections | Strict Console/page/response monitoring; stable 2/2 in CI |
| Existing E2E | Replaced obsolete unmounted/mobile/old-copy assertions with current visible Incident contracts | Full current Chromium suite 11/11 |
| Network-stable local evidence | Added opt-in `PLAYWRIGHT_NETWORK_ISOLATED=1` launch support | Default CI is unchanged; local Bubblewrap run avoids a host `eth3` route flap without weakening assertions |

## Browser Checks

### Monitoring

- Initial focus enters the Query Definition title field.
- Shift+Tab remains within the modal.
- Escape and Cancel close and restore focus to the save trigger.
- Light and Dark screenshots are present.
- No Query Definition save or authorization was submitted.

### Settings

- 767, 768, 769 and 1024 widths have no page-level horizontal overflow.
- Summary and action areas switch between column and row without clipping.

### Agent and Notifications

- Closed drawer: no Agent index, detail or Consultation event requests.
- Open drawer: Agent indexes/details and Consultation SSE start.
- Close: Agent-owned stream and reads stop.
- Direct `/agent`: workspace correctly owns Agent data.
- Notification list and `/api/v1/notification-events` remain independent throughout.

### Provider Links and Atlas

- Only `http:` and `https:` Provider targets survive normalization.
- Real Atlas rendered Kubernetes topology with a non-null WebGL context.
- `preserveDrawingBuffer=false`; a cleared direct `readPixels` result is expected after presentation.
- Full context-loss, structured fallback, hidden-page pause and disposal paths are proven in the isolated Three.js prototype, not claimed from the production canvas alone.

## Final Commands

```bash
cd frontend
npm run lint
npm run lint:no-new-warnings
npm run typecheck
npm run typecheck:e2e
npm test
npm run build
npm audit --audit-level=high --registry=https://registry.npmjs.org
npm run test:e2e:stable
npm exec playwright -- test tests/e2e/baseline-readonly.spec.ts --browser=firefox --grep="stable Incident read path"
```

The Chromium and Firefox browser commands were run in an ephemeral Bubblewrap network namespace containing loopback plus a non-routable dummy interface because the host `eth3` repeatedly removed and restored its address/default route. The namespace cannot reach external services; fixture API, Vite and browser share loopback. Chromium stable result is PASS 2/2 and Firefox critical result is PASS 1/1.

## Exact Results

| Gate | Status | Detail |
| --- | --- | --- |
| Lint | PASS | 0 errors, 2564 warnings, 1748 fixable |
| Warning budget | PASS | 2564 <= 2608 |
| App typecheck | PASS | `vue-tsc --noEmit` |
| E2E typecheck | PASS | `tsc -p tsconfig.e2e.json --noEmit` |
| Unit | PASS | 19 files, 67 tests |
| Build | PASS | Vite 7.3.6; entry 153.84 KiB gzip; Three.js 189.46 KiB gzip |
| Audit | PASS | 0 vulnerabilities on official npm registry |
| Chromium stable | PASS | 2/2, mutation-free fixture flows |
| Firefox critical | PASS | 1/1 Incident read path |
| WebKit | NOT RUN | Missing host GTK, GStreamer, WebKit and media libraries |

## Remaining Non-Blocking Debt

- 2564 historical lint warnings remain under the enforced budget.
- The unit harness emits two unresolved `RouterLink` warnings; browser runtime is clean.
- Production build reports third-party Rollup PURE annotation warnings and a raw chunk-size warning; gzip budgets remain within the current targets.
- Current Incident realtime replays a finite stream and can cause an initial burst of projection refreshes; cursor de-duplication prevents duplicate application of the same event, but batching/backpressure belongs in the implementation plan.
