# Gate 0 Frontend Decision Coverage

Recorded: 2026-07-30 (Asia/Shanghai)

Authority baseline: `main@1568a8198f525edcff4aac0f48c81d3ac055c2fb`

This ledger proves that every approved detailed-design identifier has an explicit disposition and implementation Gate. It maps, but does not claim implementation of, production UI behavior. The normative behavior remains in `docs/CloudOps-Frontend-Refactor-Plan.md` section 2.4.

## D-01 Through D-34

| ID | Disposition | Implementation Gate | Versioned authority alignment |
| --- | --- | --- | --- |
| D-01 | Preserve Sidebar + content + optional pushed Inspector | 2, 3, Workspaces | Spec 14.3 keeps one main scroll owner and Atlas Inspector boundary |
| D-02 | Preserve Scope only in the Sidebar responsibility | 2 | Spec 8.1 removes duplicate page/Header selectors |
| D-03 | Preserve pinned global Agent and full `/agent` route | 2, 8 | Spec 9.2 preserves shared Consultation ownership |
| D-04 | Refine to quick Inspector plus full work page | 3, 7, 9, 10 | Spec 8 and 14.2 preserve deep links and task ownership |
| D-05 | Preserve Tab row plus Toolbar composition | 3, 4-11 | Plan remains normative; no conflicting Spec rule remains |
| D-06 | Preserve 220/64/520/960 values as browser-adjustable soft targets | 2-11 | Spec 14.4 adopts the approved desktop matrix |
| D-07 | Preserve compact 48px-row, 13-14px-body density as soft targets | 1, 3-11 | Spec 14.5 keeps compact operational typography |
| D-08 | Preserve restrained borders, small radii and overlay-only elevation | 1, 3-11 | Spec 14.5 rejects decorative depth and card walls |
| D-09 | Preserve severity marker plus text Badge and neutral row | 3, 7, 9 | Plan remains normative; Spec keeps semantic status color only |
| D-10 | Refine Loading/Empty/Error/Partial/Stale under FR-CX-004 | 3, 4-11 | Spec 2.2 preserves truthful unavailable states |
| D-11 | Refine relative scan time plus directly visible audit UTC | 1, 3-11 | Spec 14.5 preserves mono/tabular operational text |
| D-12 | Preserve operational first viewport; Atlas becomes a real preview | 4 | Spec 8.1 now defines the Command Center composition |
| D-13 | Refine Overview to read-only Scope investigation and links | 4, 8, 9 | Spec 8.1 explicitly excludes write ownership |
| D-14 | Preserve Overview Incident handoff to Incident Inspector/detail | 4, 9 | Spec 8.1 links Incident to its owning surface |
| D-15 | Preserve unlinked Alerts as secondary operational content | 4, 7 | Spec 8.1 includes active Incidents and unlinked Alerts |
| D-16 | Refine Agent summary to conclusion, Evidence and confidence boundary | 4, 8, 9 | Spec 8.1 prohibits approval from Overview |
| D-17 | Preserve healthy/recent/last-investigation no-active state | 4 | Spec 8.1 explicitly defines the no-active composition |
| D-18 | Refine command placement and confirmation by consequence | 3, 7, 9-11 | Spec 2.3 retains exact confirmation and safety truth |
| D-19 | Refine URL state with replace/push and legacy compatibility | 3, 4-11 | Spec 14.2 preserves URL-restorable state |
| D-20 | Refine column collapse to progressive desktop degradation | 2, 4, 8 | Spec 14.4 replaces phone workflows with desktop collapse |
| D-21 | Preserve context-triggered, structured-new and free-query Agent entries | 4, 7-9 | Spec 8.7 preserves scoped Agent context |
| D-22 | Refine Agent three-column dimensions as soft targets | 8 | Spec 8.7 defines progressive auxiliary-column collapse |
| D-23 | Preserve Incident linear stage flow and deep-linked ZoneNav | 9 | Spec 8.8 preserves Incident lifecycle ownership and links |
| D-24 | Refine `/overview` to Command Center and add lazy `/atlas` | 3, 4 | Spec 8.1 and 8.1.1 now define both routes and legacy normalization |
| D-25 | Preserve Atlas pushed Inspector and renderer resize/restore | 4 | Spec 8.1.1 preserves Inspector and renderer lifecycle |
| D-26 | Refine Alerts to shared Inspector rules and consequence-aware commands | 7 | Spec 8.4 preserves alert-local lifecycle and Incident links |
| D-27 | Preserve Infrastructure resource tabs, table and Inspector | 4 | Spec 8.2 preserves typed resources, details and Context Links |
| D-28 | Refine Logs to virtual rows, Inspector and Evidence handoff | 6 | Spec 8.5 preserves virtualization and Provider context |
| D-29 | Refine Settings to per-section frontend draft and same-page history | 11 | Spec 8.10 preserves explicit validate/apply and secret truth |
| D-30 | Preserve chart-first Monitoring with synchronized table | 5 | Spec 8.3 plus Spec 14.1 lock uPlot as specialist renderer |
| D-31 | Refine Trace full detail to retain canonical `trace_id` Query | 6 | Spec 8.6 preserves Trace detail and related context |
| D-32 | Refine DevOps Inspector/full detail while retaining existing Query | 10 | Spec 8.9 preserves technical detail and exact identity |
| D-33 | Refine live updates with user-controlled rows and continuity truth | 3, 7-10 | Spec 17 retains SSE/Console evidence; plan defines full state contract |
| D-34 | Refine Provider health to Lucide `Bolt` + text and `#providers` link | 2, 11 | Spec 2.4 and 14.1 require Lucide-only visible icons |

## FR-SUP-001 Through FR-SUP-010

| ID | Disposition | Implementation Gate | Versioned authority alignment |
| --- | --- | --- | --- |
| FR-SUP-001 | Incident is the single incident Approval/Delivery/Verification surface | 9, 10 | Spec 8.8/8.9 preserve Incident truth and DevOps global responsibility |
| FR-SUP-002 | Desktop-first progressive collapse; no phone product | 2, 4, 8, 12 | Spec 1, 14.4, 17.3 and 18 now use the approved desktop matrix |
| FR-SUP-003 | Shareable selection; replace for scan; Back restores context | 3, 4, 7, 9, 10 | Spec 14.2 and 17.3 preserve URL/history behavior |
| FR-SUP-004 | Keep quick Inspector and full-work-page boundary consistent | 3, 7, 9, 10 | Spec 8 route ownership remains additive and compatible |
| FR-SUP-005 | Reconnect without moving the reader; stop live claims if incomplete | 3, 7-10 | Spec 2.2 and 17 require truthful state and SSE evidence |
| FR-SUP-006 | Overview may start only a Scope-bound read-only investigation | 4, 8 | Spec 8.1 explicitly excludes execution and configuration |
| FR-SUP-007 | Confirmation strength follows actual consequence | 3, 7, 9-11 | Spec 2.3 and 13.4 retain authority and exact-plan safety |
| FR-SUP-008 | Settings uses per-section frontend draft and explicit apply | 11 | Spec 8.10 preserves validate/diff/apply without a backend Draft claim |
| FR-SUP-009 | Essential table columns stay visible; local secondary preferences | 3, 4-11 | Plan remains normative; Spec 14.2 keeps local UI state out of URL |
| FR-SUP-010 | No built-in Atlas image export in this refactor | 4 | Spec 8.1.1 forbids export-driven `preserveDrawingBuffer` |

## FR-CX-001 Through FR-CX-008

| ID | Disposition | Implementation Gate | Versioned authority alignment |
| --- | --- | --- | --- |
| FR-CX-001 | Inspector Focus, Escape, dismissal, restore and unavailable-target contract | 3, 4, 7, 9, 10 | Spec 14.6 and 17.3 require Focus/restore/topmost Escape evidence |
| FR-CX-002 | Separate restorable URL state from local transient UI state | 3, 4, 6, 7, 9, 10 | Spec 14.2 preserves public paths, Query and deep links |
| FR-CX-003 | Keyboard-accessible relative time and directly visible audit UTC | 3, 4-11 | Spec keeps audit/domain truth; plan supplies the stricter presentation rule |
| FR-CX-004 | Distinguish first load, submit, background refresh and long operation | 3, 4-11 | Spec 2.2 preserves truthful states; plan supplies interaction details |
| FR-CX-005 | Preserve domain states without a universal frontend state machine | 3, 4-11 | Spec 2.2, 8.8, 8.9 and 13.4 preserve backend truth |
| FR-CX-006 | Preserve raw long content, full copy, virtualization, order and anchors | 3, 6, 9 | Spec 8.5/8.6 and 14.6 preserve bounded large-data behavior |
| FR-CX-007 | Keyboard/screen-reader equivalence, reduced motion and safe Provider links | 1, 3-12 | Spec 2.4, 14.6 and 17.3 preserve accessibility and safe context |
| FR-CX-008 | Every visible UI/API/SSE capability is mapped and verified | 0, all Slices, 12 | Prework capability map is `PASS`; Spec 18 requires final production proof |

## Higher-Authority Replacements

| Superseded lower description | Final versioned rule |
| --- | --- |
| Phone Bottom Navigation, phone Drawer and 320px workflow | Removed from target; desktop 1920/1440 plus 1280/1024/zoom degradation is normative |
| Emoji in Shell examples | Semantics retained with Lucide and text; visible UI has no emoji |
| Skeleton for every Loading state | FR-CX-004 distinguishes first load, button submission, background refresh and long operation |
| A new `/:entityId` path for every entity | Existing paths and Trace/DevOps Query contracts remain canonical; additions must be compatible |
| Exact time only on mouse hover | Audit-critical exact UTC is directly visible and copyable without hover |
| Atlas export undecided | No built-in export in this refactor |
| Atlas snapshots, cross-device columns and named views | Deferred; no placeholder or partial entry in this refactor |
| Shared/LAN login and route guards | Not inferred from Local Owner; a deployment-boundary change requires a new ADR |

```text
DESIGN_DECISION_MAPPING=PASS
SOURCE_DESIGN_DECISIONS=52
UNMAPPED_DESIGN_DECISIONS=0
REVIEW_CONCLUSION=COMPLIANT
```
