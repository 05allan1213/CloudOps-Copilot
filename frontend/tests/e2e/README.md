# Frontend browser-gate acceptance record

## Scope

The Playwright suite in this directory validates the Incident Workbench presentation layer: keyboard navigation, URL state, responsive layout, focus restoration, fail-closed state messaging, reconnect behavior, retry identity, theme contrast, reduced motion, visual stability, and basic interaction latency.

The Node fixture server is presentation-only. It returns deterministic, contract-shaped responses for browser verification; it does not prove the real OAuth session, API persistence, SSE service, or command path. The backend command endpoint remains a `501 NOT_IMPLEMENTED` skeleton, so real command integration is intentionally reported as `NOT RUN`.

## Exact commands

Validated in this workspace with the installed Chromium binary:

```bash
cd /home/monody/k8s/CloudOps-Copilot/frontend
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

## Browser-gate result

| Gate | Result | Evidence |
| --- | --- | --- |
| Playwright Chromium suite | PASS (8/8) | `tests/e2e/workbench.spec.ts` |
| Dark visual baseline | PASS | `tests/e2e/workbench.spec.ts-snapshots/incident-list-dark-1440-linux.png` |
| Light visual baseline | PASS | `tests/e2e/workbench.spec.ts-snapshots/incident-list-light-1440-linux.png` |
| Keyboard / skip-link / focus restoration | PASS | list, detail, drawer, and decision-dialog cases |
| Responsive matrix | PASS | 320×812, 375×812, 667×375, 720×900, 768×900, 1024×768, 1440×1000 |
| Theme and contrast matrix | PASS | dark/light toggle; primary, secondary, muted, and action contrast checks |
| Reduced-motion matrix | PASS | `prefers-reduced-motion: reduce`; animation and transition durations collapse |
| Layout-shift / interaction budget | PASS | CLS `0`; measured interaction `15 ms` and maximum observed long task `93 ms` in the baseline-generation run |
| External font requests | PASS | no Google Fonts, Typekit, or Adobe Fonts requests |
| Real OAuth/API/SSE/command integration | NOT RUN | requires the backend and live credentials; fixture is not production evidence |

## Bundle measurement

The explicit Element Plus registration removes the previous full-library import and eliminates the Vite large-chunk warning:

| Largest emitted asset | Raw | Gzip |
| --- | ---: | ---: |
| JavaScript | 375.24 kB | 135.05 kB |
| CSS | 102.91 kB | 15.65 kB |

These are local Vite production-build measurements; they are not a hosted-performance claim.

## 3–5 minute demo path

1. Open `/incidents?e2e=list` and show URL-synced filters, keyboard skip-link focus, and the loading/empty/forbidden/unavailable states.
2. Open the fixture Incident and follow the four-zone chain: What Happened → Investigation → Remediation & Delivery → Recovery.
3. Inspect an Evidence row, copy a bounded hash, and show the request/trace identity on an unavailable projection.
4. Switch to the viewer fixture to show approval controls fail closed; switch back to the operator fixture and open the decision dialog to demonstrate focus restoration and the immutable reason field.
5. Trigger the reconnect fixture and then the 503 command fixture; show that the projection remains visible, Live state returns, and retry uses the same idempotency key.

All steps above use the deterministic fixture and are suitable for a local presentation walkthrough only.
