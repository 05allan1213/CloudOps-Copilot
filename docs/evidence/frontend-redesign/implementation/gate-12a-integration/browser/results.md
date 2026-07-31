# Gate 12A Real Read-only Browser Results

## Result

```text
BROWSER=CHROMIUM
VIEWPORT=1440x900
THEME=LIGHT
ROUTE_CASES=16/16_PASS
REAL_READONLY_INTEGRATION=PASS
WRITE_PATH_E2E=NOT RUN
OWNER_FINAL_VISUAL_ACCEPTED=NOT RUN
```

## Runtime and Guard

- Frontend: `http://127.0.0.1:5173/`
- Backend: `http://127.0.0.1:18080`; `/readyz` HTTP 200
- Browser: Playwright CLI named session `g12a`, Chromium, `zh-CN`,
  `Asia/Shanghai`, 1440x900, Light
- Data: live backend and current Provider-backed read projections, not fixture
- Allowed methods: `GET`, `HEAD`, `OPTIONS`
- Observed/blocked non-read requests: 0/0
- Screenshots: runtime diagnostics under `output/playwright/gate-12a/browser/`;
  intentionally excluded from the Gate commit with other generated output

Every route case returned its HTML document with HTTP 200, reached a visible
H1, completed its initial workspace loading state, and had no page error,
failed request, Console error/warning, unbounded horizontal overflow, or
blocked write. Every observed application API response was HTTP 200.

## Route Matrix

| Case | Requested path | Final path / visible H1 | API responses | Result |
| --- | --- | --- | ---: | --- |
| Root redirect | `/` | `/overview`, `运维态势` | 9 | `PASS` |
| Overview | `/overview` | `/overview`, `运维态势` | 9 | `PASS` |
| Atlas canvas | `/atlas` | `/atlas`, `运行拓扑` | 5 | `PASS` |
| Atlas structured | `/atlas?view=structured` | same canonical Query, `运行拓扑` | 5 | `PASS` |
| Infrastructure | `/infrastructure` | canonical cluster/time Query, `基础设施资源` | 8 | `PASS` |
| Monitoring | `/monitoring?execution=<real-id>` | canonical execution context, `监控` | 10 | `PASS` |
| Alerts | `/alerts` | `/alerts`, `告警` | 5 | `PASS` |
| Alert detail | `/alerts/<real-id>` | same detail path, real alert title | 5 | `PASS` |
| Logs | `/logs?query=<real-id>` | canonical failed execution context, `日志` | 8 | `PASS` |
| Traces | `/traces?search=<real-id>` | canonical stored search context, `链路` | 8 | `PASS` |
| Agent | `/agent?investigation=<real-id>` | same selection Query, `Agent 调查工作区` | 10 | `PASS` |
| Incident list/Inspector | `/incidents?selected=<real-id>` | same selection Query, `Incident` | 11 | `PASS` |
| Incident detail | `/incidents/<real-id>` | same detail path, real Incident title | 17 | `PASS` |
| DevOps | `/devops` | `/devops`, `DevOps Workspace` | 7 | `PASS` |
| Settings | `/settings` | `/settings`, `设置与 Revision` | 6 | `PASS` |
| 404 | `/gate-12a-not-found` | same unknown path, `页面不存在` | 4 | `PASS` |

The shared Shell readers account for Bootstrap, Scope, notification list, and
notification-event requests on each route. Route-specific readers included
Overview/topology/resources, Monitoring execution/history, Alerts list/detail,
Logs/Traces history/detail, Agent history/detail/guidance/plans, Incident
list/projections, DevOps projection/resources, and Settings/storage.

## Cross-route Contracts

| Flow | Observation | Result |
| --- | --- | --- |
| Legacy Atlas Query | `/overview?view=structured&resource=...` replaced to canonical `/atlas?view=structured&resource=...` | `PASS` |
| Overview -> Incident | Emitted `/incidents?selected=<id>` and opened the selected Inspector | `PASS` |
| Overview -> Agent | Emitted `/agent?investigation=<id>` and restored the selected investigation | `PASS` |
| Incident list history | Row -> Inspector -> full detail -> Back -> close restored URL, scroll, and originating-row focus | `PASS` |
| Settings anchor | `/settings#providers` scrolled and focused the Provider section after async load | `PASS` |
| Alert context | Incident and Agent links reached compatible canonical targets | `PASS` |
| Telemetry resource key | Cross-page producers emitted `resource`; target readers retained legacy `workload` input compatibility | `PASS` |

## Inspector and Layout

At 1440x900, Incident, Alerts, Infrastructure, and DevOps used the integrated
520px pushed Inspector contract:

```text
route-content-right=891
inspector-left=920
gap=29
document-width=1440
```

The Inspector never covered route controls and did not create page-level
horizontal overflow. Infrastructure selection refresh kept its 34-row list
mounted. Close made no additional API request and restored the prior list URL,
scroll context, layout margin, and trigger focus.

The generic overlap detector returned six pairs on Settings. All six belong to
Nuxt UI number-input internal decrement/value/increment geometry. Inspection of
`settings.png` confirmed no unrelated controls, labels, or content overlap.
All other route cases returned zero overlap pairs.

## Logs Limitation

All 23 stored Log executions available from the backend were expired or
failed. The selected historical failed execution rendered truthfully. The
invalid-entry Inspector and its recovery path passed, but a valid retained
result row did not exist. Starting a new query would have issued a POST solely
to manufacture evidence and was therefore not performed.

```text
LOGS_STORED_HISTORY_READ=PASS
LOGS_INVALID_SELECTION_RECOVERY=PASS
LOGS_REAL_RETAINED_RESULT_INSPECTOR=NOT RUN
```

## Atlas Note

Canvas and structured views both rendered. An earlier canvas pixel probe
reported nonblank pixels and headless Chromium `ReadPixels` performance
warnings; the final route audit reported no Console warning. This is not an
Atlas performance claim. Performance, memory, WebGL lifecycle stress, and the
200-node matrix remain Gate 12B `NOT RUN`.

## Deferred

Dark, other viewports, zoom, Firefox, WebKit, comprehensive keyboard and
accessibility checks, performance, large-data, SSE soak, and write paths were
not run. They are Gate 12B or separately authorized write-path work.
