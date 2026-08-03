# Frontend Authority Conflict Ledger

Recorded against `main@c8e709fd10ea47976b262dea22440e5496385c1e`.

## Resolution Rule

The authority order is: current user instruction -> `/home/monody/k8s/vue.md` -> current-state audit -> detailed decision supplement/review -> compatible parts of the older detailed decision record -> repository specifications and Accepted ADRs -> current source/runtime for factual observation. Current source can prove what exists, but cannot turn a confirmed defect into a protected target requirement.

## Ledger

| ID | Conflict | Current fact | Governing decision | Resolution |
| --- | --- | --- | --- | --- |
| A-01 | Element Plus current stack vs Nuxt UI target | Production uses Element Plus 2.14 | Nuxt UI 4 + Tailwind 4 was a candidate until prototype proof | Keep Element Plus only for baseline recovery; select one target after isolated proof; do not mix two general UI systems |
| A-02 | Old spec says retain Element Plus | Older implementation text reflects the current product | `vue.md` and current user instruction are higher authority | Target decision is Nuxt UI 4.10.0; production migration remains out of scope here |
| A-03 | Current Bottom Navigation vs desktop-only target | `MobileBottomNav` is mounted and current navigation exports phone groups | Primary acceptance is 1920/1440 with 1280/1024 degradation; no phone product | Classify current mobile behavior as explicit target retirement, not as an omitted route or hidden compatibility promise |
| A-04 | Marketing-style visual references vs operations workspace | Reference images contain useful layout language but are not product contracts | Chinese-first, dense, quiet desktop operations workspace | Retain continuous canvas, grouped navigation and hierarchy; reject hero, card wall, decorative gradients, glass and oversized type |
| A-05 | Current bug vs compatibility contract | Raw dialog, eager Agent stream, clipped Settings and unsafe Provider links existed | Confirmed bugs must be repaired | Mark FIXED; do not preserve their DOM or timing as compatibility requirements |
| A-06 | API exists vs user-visible capability | Source contains components and endpoints not mounted in a route | Only actual visible entry plus route/store/API trace proves capability | Unmounted `ApprovalPanel`/`RemediationWorkbench` are not current visible capabilities; preserve their domain contracts separately |
| A-07 | Static screenshot vs data provenance | Prototype screenshots are deterministic fixture data | Real topology claims require current Provider evidence | Label every screenshot `fixture` or `real`; never use prototype Atlas as Kubernetes evidence |
| A-08 | Overview actions vs incident operations | Overview currently presents real topology | Overview is Scope-bound read-only investigation entry | Preserve refresh/view/select/open-in-infrastructure; do not add approval, execution, configuration or rollback ownership |
| A-09 | Incident vs DevOps write ownership | Incident exposes investigation/recovery/close; DevOps exposes global plans/cards/executions | Incident is the single incident Approval/Delivery/Verification surface | Migrate incident-specific lifecycle operations into Incident; retain DevOps global queue, non-incident operations, technical detail and compatible links |
| A-10 | `accepted`, `dispatched`, `observed`, `verified` conflation | Current projections expose all stages | Accepted ADR/domain truth keeps stages distinct | Only current Verification plus Evidence may support verified success; all prototype states preserve the distinction |
| A-11 | Old links vs normalized query names | Alert detail emits `workload`; Logs/Traces consume `resource` | Old deep links must remain usable while canonical URL state is protected | Record as current compatibility defect; later migration must accept both and emit canonical `resource` |
| A-12 | Incident sorting vs URL-restorable state | Table sorting is local component state | Filters, sort, pagination and selection should survive links/history | Record local-only sorting as current defect; target sort belongs in URL without pretending it is already there |
| A-13 | Browser technical PASS vs Owner visual acceptance | Automated checks pass and the Owner supplied exact `OWNER_VISUAL_ACCEPTED=YES` on 2026-07-30 | Owner alone can approve product fit | `OWNER_VISUAL_REVIEW=PASS`; acceptance unlocks plan generation but does not authorize production migration |
| A-14 | HTTP POST vs business mutation | Monitoring/log/trace query execution uses POST but is semantically read-only | Write-path isolation applies to business state, secrets, approvals, delivery and rollback | Real evidence may execute bounded read queries; it must still record method and prove no business mutation |
| A-15 | Historical report vs live worktree | Earlier reports and screenshots may be accurate for earlier code | Current SHA, dirty worktree and rerun evidence win | Revalidated current source and runtime; retained old evidence only when its code surface did not drift |
| A-16 | Full test claim vs unavailable WebKit | Chromium and Firefox run; WebKit binary cannot launch on this host | Missing runtime prerequisites are `NOT RUN` | Do not infer WebKit PASS or convert an environment block into an application FAIL |

## Non-Negotiable Protected Truth

- Public routes, deep links, query/hash state, Back/Forward and refresh behavior.
- Operational Scope and Provider-backed source identity.
- `ApiError` status/code/request ID/trace ID/next steps.
- Notification, Agent and Incident SSE ownership and teardown.
- Evidence provenance, exact authority, expected version/hash and idempotency identity.
- Incident, Approval, Delivery, Verification and Resolution truth.
- Safe `http:`/`https:` external links only.

## Explicitly Not Authorized

No production Nuxt UI/Tailwind migration, backend/API/database/Kubernetes semantic edit, implementation plan, write-path execution, commit, push, PR or release is authorized by this prework.
