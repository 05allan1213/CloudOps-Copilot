# Capability and Contract Map

Recorded against `main@c8e709fd10ea47976b262dea22440e5496385c1e` and the current dirty prework tree.

## Result

```text
CAPABILITY_CONTRACT_MAP=PASS
UNMAPPED_CAPABILITIES=0
```

`保留` means the behavior and contract stay in the same responsibility area. `迁移` means the behavior moves to the confirmed target surface. `兼容入口` means old links continue to resolve while the canonical target is emitted. `明确废弃` is used only where the current user instruction already authorizes retirement.

## Public Route Map

| Current route | Visible owner | URL state and deep-link contract | Target disposition | Verification |
| --- | --- | --- | --- | --- |
| `/` | Router | Redirect to `/overview` | 保留 | Router unit test |
| `/overview` | Overview | `view`, `resource` | 保留 as Scope-bound read-only Atlas | Real Canvas/structured screenshots and `GET /api/v1/overview` |
| `/infrastructure` | Infrastructure | `cluster`, `namespace`, `kind`, `search`, `from`, `to`, `resource` | 保留 | Real typed topology/resource traversal |
| `/monitoring` | Monitoring | `cluster`, `namespace`, `resource`, `mode`, `metric`, `query`, `from`, `to`, `execution`, `definition` | 保留 | Real query/dialog check and fixture regression |
| `/alerts` | Alerts | `status`, `severity`, `namespace`, `search`, `incident`, cursor/limit | 保留 | Route traversal and typed alert APIs |
| `/alerts/:alertId` | Alert detail | Alert ID plus generated cross-Workspace queries | 保留; legacy `workload` becomes 兼容入口 | Deep-link traversal; mismatch recorded below |
| `/logs` | Logs | `cluster`, `namespace`, `resource`, `mode`, `text`, `trace_id`, `levels`, `limit`, `tail`, `wrap`, `query`, `from`, `to` | 保留 | Current typed query/evidence flow |
| `/traces` | Traces | `cluster`, `namespace`, `resource`, `mode`, `service`, `operation`, `status`, `min_duration_ms`, `max_duration_ms`, `limit`, `search`, `trace_id`, `from`, `to` | 保留 | Current search/detail/span/evidence flow |
| `/agent` | Agent | `agent_view`, `investigation` | 保留 | Direct owner lifecycle and browser test |
| `/incidents` | Incident list | `status`, `severity`, `service`, `attention`, `resource`, `alert`, `from`, `to`, `limit`, `cursor` | 保留 | Stable Chromium/Firefox URL and Back test |
| `/incidents/:incidentId` | Incident detail | Incident ID, stable zone hash, `run`, `evidence` | 保留 and expand as incident lifecycle owner | Typed projection and realtime E2E |
| `/devops` | DevOps | `view=operations|identity`, `subject`, `operation` | 保留 global/non-incident responsibility | Store tests and route traversal |
| `/settings` | Settings | Stable `#providers` anchor; draft remains local until explicit apply | 保留 | Breakpoint, validation and prototype draft tests |
| Catch-all | 404 | Original unknown path | 保留 | Route traversal and router test |

## Global Shell Capabilities

| Capability | Current UI -> code -> API/SSE | Target responsibility | Protected contract and risk |
| --- | --- | --- | --- |
| Grouped desktop navigation | Sidebar/Header -> `navigation.ts` -> Router | 保留 | Ten Workspace paths, current route indication, keyboard focus |
| Phone Bottom Navigation | `MobileBottomNav.vue` -> mobile navigation groups | 明确废弃 as target phone product | Current capability is documented; route availability remains on desktop |
| Sidebar collapse | AppLayout/AppSidebar -> localStorage | 保留 | Preference failure must not make navigation unusable |
| Skip link and route-heading focus | AppLayout -> async H1 observer -> Router transitions | 保留 | Path change focuses current H1; query/hash changes do not steal focus; Back restores row focus |
| Operational Scope | Header/AppLayout -> `getBootstrap`, `getScopes`, `activateScope` | 保留 | Active scope, cluster query, partial refresh error and Provider identity remain truthful |
| Provider health | Bootstrap/Workspace status -> platform/Workspace APIs | 保留 | available/partial/unavailable/disabled/not-configured are not collapsed |
| Notifications | Inbox -> list/read/read-all -> `/notifications`, `/notification-events` | 保留 | Notification SSE is independent from Agent; context links remain routable |
| Theme | `useTheme`/Shell -> localStorage/media -> Light/Dark styles | 保留 | Equal hierarchy and semantic states, not color-only |
| Global Agent pin/drawer | Header -> GlobalAgentPanel -> Agent store/API/SSE | 保留 | Closed state is idle; open drawer or `/agent` owns reads and stream; close tears down |
| Error identity | All Workspaces -> `ApiError` | 保留 | status, code, request ID, trace ID, replay flag and next steps stay visible where relevant |

## Workspace Capability Map

| Workspace | Current visible actions | API/SSE and real source | Target disposition | Contract test / migration risk |
| --- | --- | --- | --- | --- |
| Overview | Refresh; Canvas/structured switch; select resource; inspect relationship; open Infrastructure; retry/settings link | `GET /api/v1/overview`; Kubernetes typed Provider | 保留 | No synthetic nodes; structured fallback on no WebGL/provider/empty; no write controls |
| Infrastructure | Refresh; namespace/kind/search/time filter; select resource/owner/peer; inspect facts/events; open settings | Bootstrap, topology, resource list/detail/events; Kubernetes typed Provider | 保留 | URL-selected resource and time range; partial/unavailable truth; bounded detail |
| Monitoring | Guided/expert mode; resource/time selection; start/cancel query; open history/execution/definition; save definition; authorize/revoke; external Provider link | Monitoring catalog/queries/definitions/authorizations; Prometheus Provider | 保留 | POST query is read execution; save/authorization are real writes and remain NOT RUN in real E2E; modal/focus contract fixed |
| Alerts list | Filter/search/incident scope; paginate; open detail | Alert list/detail; Alertmanager Provider | 保留 | URL filters and deep links; permission/error identity |
| Alert detail | Refresh; acknowledge; silence/expire; create/attach Incident; start investigation; open Logs/Traces/Incident links | Versioned/idempotent alert commands; Alertmanager plus Incident/Agent | 迁移 incident-specific operations toward Incident entry while retaining alert-local acknowledgement/silence | Expected version and idempotency key mandatory; real commands NOT RUN |
| Logs | Guided/expert query; time/level/text/trace filters; tail/wrap; history; row selection; save Evidence; create Consultation; external Elasticsearch | Logs catalog/queries/evidence, Agent consultation; Elasticsearch Provider | 保留 | Full values/copy, bounded results, expired history, URL state; real evidence writes NOT RUN |
| Traces | Guided/expert search; filters; open trace/span; select spans; save Evidence; freeze context/create Consultation; external Tempo | Trace catalog/search/detail/evidence, Agent consultation; Tempo Provider | 保留 current renderer plus virtualize large lists | Full trace/span copy and Evidence provenance; canonical `resource`; writes NOT RUN |
| Agent | History tabs; select Investigation/Consultation; send/cancel; attach context; save Knowledge; propose/authorize plan/card | Agent investigations/consultations/events, knowledge, runbook, operation plans/action cards; LLM and stored Evidence | 保留 | Scope/Evidence context always visible; exact hash/authority/idempotency; real mutation E2E NOT RUN |
| Incident list | URL filter/reset; server pagination/load more; local table sorting; open detail | Incident collection API | 保留 | Query/Back/cache/error states; sorting currently local and must become URL-restorable |
| Incident detail | Refresh projections; zone/hash navigation; load more; start investigation; recovery decision; close; retry; open alerts/context links | Incident typed projections, finite realtime stream, versioned/idempotent commands | 迁移 all incident Approval/Delivery/Verification ownership here | accepted/dispatched/observed/verified separation; initial finite-stream refresh burst needs batching/backpressure |
| DevOps | Operations/identity views; select subject/operation; authorize/execute global plan/card; inspect freeze/candidate/baseline/delivery/provider branches | `/api/v1/devops`, operation plan/action-card execution; GitHub/Argo/Kubernetes branches | 保留 global queue, non-incident actions and technical detail | No duplicate incident write entry; exact hash and authority required; real execution NOT RUN |
| Settings | Edit revision summary/general/scope/provider/policy/secret refs; validate; test Provider; apply; inspect revision/history/storage/partial worker status | settings/scopes/storage, validation, configuration revision, secret and Provider-test APIs | 保留 | Local draft is not backend Draft; apply is explicit; conflicts/partial outcomes stay itemized; real writes NOT RUN |
| 404 | Explain unknown route and return to product | Router only | 保留 | No redirect loop or hidden write |

## Incident Detail Projection Ownership

| Projection | Current source | Stable link/state | Target |
| --- | --- | --- | --- |
| Incident header and persisted context | `/incidents/:id` | Route ID | 保留 |
| Alerts/signals | `/alerts`, signal projection | Zone hash and alert context links | 保留 |
| Timeline | `/timeline?after_id=` | Zone hash, cursor order | 保留; virtualize at scale |
| Evidence | `/evidence?cursor=` | `evidence` query selection | 保留; provenance and full-value copy |
| Agent investigations | `/investigations?cursor=` | `run` query selection | 保留 |
| Recovery decision | `/decision` | expected version, reason, idempotency | 保留 in Incident |
| Remediation plan/Approval | API and unmounted historical component exist | No current visible mounted entry | 迁移 into Incident only; do not claim current UI coverage |
| Delivery | Typed API exists; current detail does not mount `DeliveryRail` | No duplicate DevOps incident action | 迁移 incident delivery status/action into Incident |
| Verification and Resolution | `/verifications`, `/resolution-report` | Zone hash; current Evidence needed for success | 保留 and strengthen |
| Realtime | `/incidents/:id/events`, cursor/Last-Event-ID | connecting/connected/reconnecting/disconnected | 保留 with cursor de-dup, bounded resync and teardown |

## Command Safety Map

| Command family | Required contract | Real E2E |
| --- | --- | --- |
| Scope activation | Explicit scope ID; refresh Bootstrap; partial-refresh truth | NOT RUN |
| Notification read/read-all | Notification identity/cursor | NOT RUN |
| Monitoring definition/authorization | Execution or definition identity; explicit revoke | NOT RUN |
| Alert commands | Expected version plus unique `Idempotency-Key` | NOT RUN |
| Log/Trace Evidence and Consultation | Query/search/span identity, time/scope provenance | NOT RUN |
| Agent message/Knowledge/plan/card | Consultation/message identity; exact hash/authority; idempotency | NOT RUN |
| Incident investigation/recovery/close/remediation | Expected version/hash, reason, HTTP 202, request/trace identity, idempotent replay | NOT RUN |
| DevOps execution | Exact expected hash and valid authority | NOT RUN |
| Settings validation/apply/secret/provider test | Validation ID/draft hash/revision, explicit secret handling, per-item partial result | NOT RUN |

## Current Defects Kept Separate From Capabilities

| Defect or risk | Classification | Required target treatment |
| --- | --- | --- |
| Alert detail emits `workload` while Logs/Traces consume `resource` | Current compatibility defect | Accept old `workload`, normalize to `resource`, emit only canonical query |
| Incident table sort is component-local | Current state defect | Put stable sort/direction in URL and test refresh/Back |
| Finite Incident SSE replay can fan out an initial set of projection reads | Performance risk | Coalesce by resource/cursor and add backpressure without losing truth |
| Historical Approval/Delivery components are not mounted | Not a current user-visible capability | Do not count as preserved UI; preserve domain/API contract and add only in Incident target |
| 118 raw color literals, 11 `:deep`, 10 `:global`, 22 `!important` in production source | Style debt | Migrate through the canonical token pipeline; no mass rewrite in baseline phase |
| Current phone Bottom Navigation | Target retirement authorized | Remove only during planned Shell migration; keep routes available on desktop |

## Bidirectional Samples

- Overview selection: UI `resource` -> route query -> `GET /api/v1/overview` -> Kubernetes Provider -> structured/Canvas/Inspector.
- Incident filter: UI fields -> normalized route query -> incident collection API -> typed rows -> detail route -> Back restores query and focus.
- Notification: `/notification-events` -> Inbox item -> typed `context_link` -> Router destination.
- Agent: drawer/open route ownership -> store -> consultation detail -> consultation EventSource -> teardown on close/unmount.
- Settings: local draft -> validate -> validation ID/hash -> explicit apply -> revision/worker partial result; no backend Draft is invented.

```text
UNMAPPED_CAPABILITIES=0
```
