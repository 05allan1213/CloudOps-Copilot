# Frontend browser-gate acceptance record

Validated locally on 2026-07-23.

## Scope and provenance

The Playwright suite in this directory validates the Incident Workbench presentation layer: session bootstrap boundaries, keyboard and native-link navigation, URL and scroll restoration, responsive layout, focus restoration, fail-closed projection states, finite and reconnecting SSE behavior, command feedback, retry identity, theme contrast, reduced motion, visual stability, and basic interaction latency.

The Node fixture server is presentation-only. It returns deterministic, contract-shaped responses for browser verification; it does not prove the real OAuth session, MySQL-backed API, production SSE service, or production command integration.

The production command path is implemented and wired in `internal/startup/container.go`, `internal/bootstrap/api/api.go`, and `internal/command/port.go`. The fixture's optional `501` response is only a frontend presentation state and must not be described as the production backend behavior.

## Exact commands

Validated in this workspace with the installed Chromium binary:

```bash
cd /home/monody/k8s/CloudOps-Copilot/frontend
npm run lint
npx vue-tsc --noEmit
npm run test
npm run build
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/home/monody/.cache/ms-playwright/chromium-1228/chrome-linux64/chrome npm run test:e2e
```

To use a freshly installed Playwright browser instead:

```bash
cd frontend
npx playwright install chromium
npm run test:e2e
```

Visual baselines are regenerated only when the presentation contract intentionally changes:

```bash
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/home/monody/.cache/ms-playwright/chromium-1228/chrome-linux64/chrome npm run test:e2e:update
```

The production image and static-delivery parity gate used:

```bash
cd /home/monody/k8s/CloudOps-Copilot
docker build --target cloudops-api -t cloudops-api:frontend-followup-local .
```

The emitted files in local `frontend/dist` and image `/app/static` were then compared by relative path and SHA-256.

## Validation result

| Gate | Result | Evidence |
| --- | --- | --- |
| ESLint | PASS | no findings |
| Vue/TypeScript typecheck | PASS | `vue-tsc --noEmit` |
| Vitest unit suite | PASS (46/46) | 10 test files |
| Vite production build | PASS | 12 emitted files |
| Playwright Chromium suite | PASS (19/19) | one serial authoritative run of `tests/e2e/workbench.spec.ts` |
| Dark visual baseline | PASS | `tests/e2e/workbench.spec.ts-snapshots/incident-list-dark-1440-linux.png` |
| Light visual baseline | PASS | `tests/e2e/workbench.spec.ts-snapshots/incident-list-light-1440-linux.png` |
| Responsive matrix | PASS | 320x812, 375x812, 414x896, 667x375, 720x900, 768x900, 1024x768, 1440x1000 |
| Theme and contrast matrix | PASS | dark/light toggle; primary, secondary, muted, and action contrast are at least 4.5:1 |
| Reduced-motion matrix | PASS | animation and transition durations collapse to at most 1 ms |
| Layout-shift / interaction budget | PASS | CLS `0`; interaction `13 ms`; maximum observed long task `96 ms` |
| External font requests | PASS | no Google Fonts, Typekit, or Adobe Fonts requests |
| Direct local CDP runtime probe | PASS | 414x812, 3 rows, document/main widths 414/414, semantic 404 `A[href="/incidents"]`, no console or page errors |
| Docker `cloudops-api` target | PASS | frontend rebuilt in the pinned Node image and copied into `/app/static` |
| Local build vs image static parity | PASS (12/12) | relative paths and SHA-256 values matched exactly |
| Playwright browser MCP | NOT RUN | MCP requires missing `/opt/google/chrome/chrome` |
| Chrome DevTools MCP | NOT RUN | MCP initialization returns `Target.setDiscoverTargets: Target closed` |
| Real OAuth/API/SSE/command integration | NOT RUN | live API containers reset all requests while restarting on Docker DNS and kubeconfig-permission failures |

## Authoritative 19-test browser matrix

| # | Browser contract | Result |
| ---: | --- | --- |
| 1 | Keyboard navigation, skip link, URL-synced filters, loaded-row sorting, and detail focus | PASS |
| 2 | List loading, empty, forbidden, and unavailable states | PASS |
| 3 | OAuth redirect on 401 and recoverable session boundaries on 403/503 | PASS |
| 4 | 1/20/50-row datasets, long content, cursor append, native Ctrl-click, and back/scroll restoration | PASS |
| 5 | Stable retryable list timeout | PASS |
| 6 | Four-zone detail chain, dialog focus restoration, responsive layout, and 414px long-content containment | PASS |
| 7 | Viewer permissions and projection failures fail closed without hiding recovery truth | PASS |
| 8 | Every frozen Investigation and Evidence presentation state | PASS |
| 9 | Timeline cursor append beyond 200 persisted events without replacement | PASS |
| 10 | Finite SSE resume with `Last-Event-ID`, dedupe, foreign-event rejection, focus, and scroll preservation | PASS |
| 11 | Bounded reconnect keeps the projection visible and restores Live state | PASS |
| 12 | Command 503 feedback, disabled states, and same-key retry | PASS |
| 13 | Command 202/403/409/422 contracts without unsafe retries | PASS |
| 14 | Expired and stale Plans fail closed before submission | PASS |
| 15 | Failed, timed-out, inconclusive, NOT RUN, and passed Verification states remain distinct | PASS |
| 16 | No-change recovery omits Plan/Delivery while retaining a passing ResolutionReport | PASS |
| 17 | Command timeout remains pending, then becomes retryable with one preserved identity | PASS |
| 18 | Theme, motion, contrast, zoom-equivalent width, external-font, and visual-baseline gates | PASS |
| 19 | Basic interaction stays below the input-feedback budget | PASS |

## Bundle measurement

The explicit Element Plus registration keeps the production bundle below the Vite large-chunk warning threshold:

| Largest emitted asset | Raw | Gzip |
| --- | ---: | ---: |
| JavaScript | 379.44 kB | 136.38 kB |
| CSS | 104.82 kB | 15.98 kB |

These are local Vite production-build measurements, not a hosted-performance claim.

## Live integration boundary

Read-only checks used `curl --noproxy '*'` against both exposed API containers. `/livez`, `/readyz`, `/api/v3/session/csrf`, and `/api/v3/incidents?limit=1` all ended with connection reset, HTTP `000`, and curl exit `56`.

Recent container logs show two independent startup blockers:

- `cloudops-v2-demo-server-web-1`: Docker DNS cannot resolve `mysql` (`127.0.0.11:53: server misbehaving`).
- `server-monitor-server-web-1`: startup cannot read `/opt/server-monitor/docker/kubeconfig` and also reports intermittent Docker DNS failure for Kafka.

No live Incident or command mutation was attempted. Real OAuth, MySQL-backed Query, SSE, Command, GitHub, Argo, and Verification evidence therefore remains `NOT RUN`.

## 3-5 minute fixture demo path

1. Open `/incidents?e2e=list` and show URL-synced filters, keyboard skip-link focus, and the loading/empty/forbidden/unavailable states.
2. Open the fixture Incident and follow the four-zone chain: What Happened -> Investigation -> Remediation & Delivery -> Recovery.
3. Inspect an Evidence row, copy a bounded hash, and show the request/trace identity on an unavailable projection.
4. Switch to the viewer fixture to show approval controls fail closed; switch back to the operator fixture and open the decision dialog to demonstrate focus restoration and the immutable reason field.
5. Trigger the finite/reconnect SSE fixtures and then the 503/timeout command fixtures; show resume identity, stable projection truth, restored Live state, and same-key retry.

All demo steps use the deterministic fixture and are suitable only for a local presentation walkthrough.
