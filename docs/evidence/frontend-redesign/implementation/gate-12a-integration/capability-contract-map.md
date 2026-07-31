# Gate 12A Final Capability and Contract Map

## Result

```text
CAPABILITY_CONTRACT_MAP=PASS
UNMAPPED_CAPABILITIES=0
ROUTE_UI_OWNERSHIP=NUXT_UI_ONLY
PUBLIC_COMPONENT_ROUTES_LAZY=14/14
REAL_READONLY_INTEGRATION=PASS
WRITE_PATH_E2E=NOT RUN
```

This map describes the final Gate 12A production tree based on the complete
six-lane merge plus the Gate 12A cleanup delta. Fixture-only lane evidence is
not used to claim real integration; the integration column refers to the live
read-only browser run documented in `browser/results.md`.

## Public Route Ownership

| Public route | Production owner and primary capability | URL/deep-link contract | Live read source | Gate 12A result |
| --- | --- | --- | --- | --- |
| `/` | Router redirect | Redirects to `/overview` | Shell Bootstrap/Scope/notifications | `PASS` |
| `/overview` | Nuxt UI Operations Command Center | Legacy Atlas Query replaces to `/atlas`; Incident uses `selected`; Agent uses `investigation` | Overview, Incidents, Alerts, Agent, DevOps projections | `MIGRATED_NUXT_UI`, `PASS` |
| `/atlas` | Nuxt UI shell + Three.js/structured specialist renderer | `view=structured`, `resource`; canvas is default | Kubernetes-backed Overview topology | `MIGRATED_NUXT_UI`, `PASS` |
| `/infrastructure` | Nuxt UI resource table + Inspector | Cluster/namespace/kind/search/time/resource selection | Topology, resources, resource detail/events | `MIGRATED_NUXT_UI`, `PASS` |
| `/monitoring` | Nuxt UI query workspace + uPlot | Resource/mode/metric/time/execution/definition | Monitoring catalog, histories, execution/detail, definitions/authorizations reads | `MIGRATED_NUXT_UI`, `PASS` |
| `/alerts` | Nuxt UI alert table + Inspector | Filters/sort/pagination/selected Incident context | Alert list | `MIGRATED_NUXT_UI`, `PASS` |
| `/alerts/:alertId` | Nuxt UI alert detail | Stable path ID and canonical context links | Alert detail | `MIGRATED_NUXT_UI`, `PASS` |
| `/logs` | Nuxt UI query workspace + TanStack virtual rows + Inspector | Canonical `resource`, query/time/level/text/trace/tail/wrap/selected; legacy `workload` input | Logs catalog and stored query/history detail | `MIGRATED_NUXT_UI`, `PASS` with retained-result limitation |
| `/traces` | Nuxt UI search workspace + virtualized Trace waterfall | Canonical `resource`; `trace_id` full detail; stored search | Trace catalog, search history/detail, Trace/span reads | `MIGRATED_NUXT_UI`, `PASS` |
| `/agent` | Nuxt UI History/Conversation/Inspector workspace | `consultation` or `investigation`; legacy `run` accepted and replaced | Investigations, consultations, guidance, runbooks, plans/cards | `MIGRATED_NUXT_UI`, `PASS` |
| `/incidents` | Nuxt UI Incident table + read-only Inspector | Filters/sort/cursor/selected are URL-owned; Back closes and restores | Incident collection plus selected projections | `MIGRATED_NUXT_UI`, `PASS` |
| `/incidents/:incidentId` | Nuxt UI seven-zone Incident lifecycle owner | Stable path ID plus run/evidence/zone context | Incident, alerts, signals, timeline, Evidence, Agent, remediation, Delivery/Verification projections | `MIGRATED_NUXT_UI`, `PASS` |
| `/devops` | Nuxt UI global/non-incident operations workspace | `view`, `subject`, `operation` | DevOps, Agent, resources and Provider projections | `MIGRATED_NUXT_UI`, `PASS` |
| `/settings` | Nuxt UI section-draft and Revision workspace | Stable section anchors including `#providers`; draft remains local | Settings, storage, scopes and Bootstrap diagnostics | `MIGRATED_NUXT_UI`, `PASS` with apply `BACKEND_GAP` |
| Catch-all | Nuxt UI 404 | Exact unknown path remains visible | Router plus shared Shell reads | `MIGRATED_NUXT_UI`, `PASS` |

## User-facing Capability Map

| Capability area | Preserved production behavior | Safety/ownership disposition |
| --- | --- | --- |
| Shell and Scope | Grouped desktop navigation, collapse preference, exact active Scope, Provider health, notifications, theme, Global Agent, Skip Link, route-heading focus | One Nuxt UI system; no phone navigation; no Scope activation was executed |
| Overview | Incident/Alert risk, Agent conclusion/Evidence boundary, Delivery/Verification truth, recent changes, real Atlas preview, Scope-bound read-only investigation entry | No approval, execution, configuration, or rollback on Overview |
| Atlas | Real topology canvas, structured equivalent, selection, resize, context-loss fallback, pause/cleanup, deep links | Three.js only as specialist renderer; no image export |
| Infrastructure | Typed resources/topology/events, dense table, selection Inspector, Monitoring/Logs/Traces/Agent context | Provider truth preserved; no synthetic nodes |
| Monitoring | Guided/expert queries, history/execution, uPlot series/table, definitions and authorization UI | Reads passed; query start, save, authorize, and revoke writes `NOT RUN` |
| Alerts | URL filters, list/detail, Inspector, acknowledgement/silence and Incident/Agent entry surfaces | Reads passed; versioned/idempotent commands `NOT RUN` |
| Logs | Guided/expert context, history, virtualization, raw copy, wrap, Inspector, Evidence/Consultation UI, Provider link | Historical failed read and recovery passed; result-creating and Evidence writes `NOT RUN` |
| Traces | Search/detail, virtual waterfall, Span Inspector, Tags/events/resources, raw copy, Evidence/Consultation UI | Reads passed; Evidence/Consultation writes `NOT RUN` |
| Agent | Context/free-query entry, History/Conversation/Inspector, SSE lifecycle, Tool/Evidence/Guidance/Knowledge/Runbook, plan/card/authority surfaces | Reads passed; message, Knowledge, authorization, and execution writes `NOT RUN` |
| Incident | List/Inspector/history, seven-zone lifecycle, exact Approval, Delivery observation, deterministic Verification, Timeline, Resolution | Sole incident Approval/Delivery/Verification operation surface; all writes `NOT RUN` |
| DevOps | Global/non-incident queues, technical detail, operation identity, Provider branch and compatible Incident links | No duplicate incident write entry; authorization/execution `NOT RUN` |
| Settings | Five independent local section drafts, validation identity, summaries, conflict/rebase, explicit apply, itemized outcomes, leave protection, Secret hygiene, Revision history | Reads passed; all writes `NOT RUN`; atomic revision compare-and-set remains `BACKEND_GAP` |

## Canonical Cross-surface Contracts

| Producer -> consumer | Canonical output | Compatibility input |
| --- | --- | --- |
| Legacy Overview Atlas -> Atlas | `/atlas?view=structured&resource=...` or `/atlas?resource=...` | `/overview?view=atlas|canvas|structured&resource=...` |
| Overview -> Incident | `/incidents?selected=<incident-id>` | Direct `/incidents/:incidentId` remains valid for full work |
| Overview/Alert/Incident -> Agent | `/agent?investigation=<investigation-id>` for existing selected runs; structured context for new investigation | Agent accepts legacy `run` and canonicalizes it |
| Alert/Infrastructure -> Logs/Traces/Monitoring | `resource=<stable-resource-id>` | Telemetry readers accept legacy `workload` input |
| Incident -> DevOps detail | Existing `view/subject/operation` Query | Incident remains primary incident operation owner |
| Provider health -> Settings | `/settings#providers` | Async anchor scroll/focus preserved |

## Safety and Truth Contracts

| Contract | Final production disposition | Gate 12A evidence |
| --- | --- | --- |
| Typed API errors | Status/code/request ID/trace ID/replay identity/next steps preserved by shared presentation | Production implementation present; forced error matrix `NOT RUN` |
| SSE and async lifecycle | Domain-specific connecting/live/reconnecting/disconnected/stale states, bounded dedupe, cancellation and teardown retained | Existing read surfaces passed; soak/failure forcing `NOT RUN` |
| Evidence provenance | Full identity/time/source remains visible and copyable | Read projections passed; creation `NOT RUN` |
| Exact authority/hash | Approval, Agent, DevOps and Settings surfaces retain fail-closed identity and consequence-specific confirmation | Presentation present; authorization/write commands `NOT RUN` |
| Delivery/Verification truth | Accepted/dispatched/observed/verified remain distinct; no Provider health inference as Verification success | Incident/Overview/DevOps read projections passed |
| Provider links | Contextual typed/allowlisted targets, safe new-tab behavior | Read-only link rendering/navigation checked where available |
| Real write protection | Browser blocked all methods outside GET/HEAD/OPTIONS; no write was attempted or blocked | `WRITE_PATH_E2E=NOT RUN` |

## Detailed Decision Coverage

| Decision IDs | Reachable production ownership |
| --- | --- |
| `D-01` to `D-03`, `D-34` | Shell layout, Scope ownership, Agent pin, Provider health and Lucide-only semantics |
| `D-04` to `D-11`, `D-18` to `D-20`, `D-33`; `FR-SUP-002` to `FR-SUP-005`, `FR-SUP-007`, `FR-SUP-009`; `FR-CX-001` to `FR-CX-005`, `FR-CX-007` | Shared Workspace, density, state, URL/history, Inspector/focus/scroll, async/SSE trust, confirmation, desktop degradation, accessibility and safe links |
| `D-12` to `D-17`, `D-21`, `D-24`, `D-25`, `D-27`; `FR-SUP-006`, `FR-SUP-010` | Overview Command Center, Atlas, Infrastructure and Scope-bound read-only Agent handoff |
| `D-28`, `D-30`, `D-31`; `FR-CX-006` | Monitoring, Logs, Traces, virtualization, raw values and complete copy |
| `D-26` | Alert list/detail/Inspector and consequence-specific local commands |
| `D-21`, `D-22` | Agent entry priority and History/Conversation/Inspector workspace |
| `D-23`, `D-32`; `FR-SUP-001` | Incident seven-zone lifecycle and DevOps responsibility convergence |
| `D-29`; `FR-SUP-008` | Settings section drafts, explicit apply, Revision history and conflict handling |
| `FR-CX-008` | This final bidirectional capability map and zero-unmapped result |

The plan's exact missing-ID scan returned no output. That proves normative ID
mapping, while route/runtime evidence above establishes current reachability.

## Retired and Deferred

- Element Plus, its icons/theme mapping, legacy SCSS theme sources, mobile
  Bottom Navigation, legacy route ownership, and unconsumed Incident files are
  retired with zero production matches.
- Atlas image export, cross-device column preference, named full views, and
  shared/LAN/multi-user authentication remain explicitly outside this scope.
- Full Gate 12B validation and every write command family remain `NOT RUN`.
