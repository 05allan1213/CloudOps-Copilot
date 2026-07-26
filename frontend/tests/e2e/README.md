# Frontend fixture validation record

Validated locally on 2026-07-26.

## Scope and provenance

The Playwright suite validates the current Incident Workbench presentation contract: keyboard and native-link navigation, URL and scroll restoration, responsive layout, focus restoration, fail-closed projection states, finite and reconnecting SSE behavior, Local Owner command feedback, retry identity, and interaction latency.

The Node fixture server is presentation-only. It returns deterministic `/api/v1` responses and does not prove the MySQL-backed API, production SSE runtime, Kubernetes or GitHub Providers, or a real UI-to-Provider integration. Fixture results must not be promoted to task-level MCP evidence.

The browser deliberately renders only fields in the current public contract. Missing service, workload, AgentStep, and Evidence trust fields remain explicitly not projected; the fixture does not synthesize them.

## Commands

The local proxy must bypass the host HTTP proxy for loopback health checks:

```bash
cd /home/monody/k8s/CloudOps-Copilot/frontend
npm run build
npm test
NO_PROXY=127.0.0.1,localhost no_proxy=127.0.0.1,localhost \
  npm run test:e2e -- --grep-invert "themes, motion"
```

## Current result

| Gate | Result | Evidence |
| --- | --- | --- |
| Vue/TypeScript and Vite build | PASS | `npm run build`; 1812 modules transformed |
| Vitest unit suite | PASS | 47 tests in 11 files |
| Non-visual Playwright fixture suite | PASS | 17 serial scenarios |
| Local Owner 403 failure path | PASS | command feedback remains fail-closed and preserves request identity |
| SSE finite/reconnect behavior | PASS | cursor dedupe, `Last-Event-ID`, visible projection, and Live restoration |
| Keyboard focus and list scroll restoration | PASS | detail H1 focus and back-navigation scroll verified |
| ESLint | NOT RUN | not required for this focused implementation check |
| Visual snapshot suite | NOT RUN | intentionally excluded from this focused run |
| Browser or DevTools MCP | NOT RUN | no Browser MCP resource or template is available in this session |
| Real UI -> `/api/v1` -> MySQL/Provider integration | NOT RUN | task runtime is not yet complete; fixture proof is not a substitute |

## Fixture matrix

The 17 passing scenarios cover list navigation and states, dataset edges and pagination, timeouts, the four-zone detail chain, empty and unavailable projections, Investigation and Evidence states, Timeline pagination, SSE resume/reconnect, command retry and 202/403/409/422 responses, stale and expired Plans, Verification outcomes, no-change recovery, command timeout, and interaction latency.

The visual, real-data, and Provider gates remain open until Task 0 has a runnable local lifecycle and the required MCP capabilities are available.
