# Phase 1 Risk Register

| ID | Risk | Status | Phase 1 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R1 | V1 and V2 Alertmanager endpoints are independent and are not atomically dual-written | ACCEPTED | V2 is opt-in; no hidden dual-write; V1 remains rollback path | Traffic cutover plan before production routing |
| R2 | Transactional outbox has no publisher, so pending rows grow | DEFERRED | Pending gauge and indexed pending query; bounded payloads | Phase 2/3 relay design, retry/DLQ/retention SLO |
| R3 | Correlation dimensions with `unknown` can over-group alerts | MITIGATED | Category and service/target included; finite aggregation window; deterministic key version | Validate against representative Alertmanager labels before cutover |
| R4 | MySQL DDL is not one atomic transaction | MITIGATED | Explicit ordered Goose up/down, disposable MySQL 8 tests, documented backup and rollback order | Production change procedure and backup evidence |
| R5 | Incident V2 requires migrated MySQL schema | MITIGATED | Explicit 503 when service is unavailable; V1 unaffected; migration status command | Deployment preflight must run `migrate status` |
| R6 | JSON/evidence/raw data may contain secrets or unbounded content | MITIGATED | Sensitive key filter, request/map/field/JSON limits, DTOs hide raw signal payload and numeric IDs | Add policy tests when new evidence collectors arrive |
| R7 | Public webhooks can be abused despite body bounds | OPEN | Strict JSON validation, body limit and no raw logging | Add receiver authentication/network policy before external exposure |
| R8 | MySQL driver native-password compatibility broadens accepted auth plugins | ACCEPTED | `AllowNativePasswords` restores compatibility with the existing MariaDB/MySQL deployment style; TLS behavior is otherwise unchanged | Prefer TLS and stronger server auth configuration in deployment hardening |
| R9 | Phase 0 refactor documents were not present under repository `docs/refactor` | OPEN | Recreated required Phase 1 ledger/risk/progress documents; did not fabricate missing history | Restore or formally supersede Phase 0 repository documents |
| R10 | Repository audit identified credential-rotation concerns | OPEN | No credential value was copied into code, tests, logs or reports; no external rotation performed | Authorized operator rotates and validates externally |
| R11 | Legacy Redis/Kafka/diagnosis/action consistency remains eventual | DEFERRED | Phase 1 does not touch or couple these flows | Address in explicit later migration slices |

No Phase 1 implementation introduces Eino, GitOps, Argo CD, Loki, Tempo, controller-runtime, repository-wide moves, or legacy deletion.

## Phase 2 additions

| ID | Risk | Status | Phase 2 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R12 | A read-only tool may still be expensive or return sensitive data | MITIGATED | Registry authorization/schema validation, per-tool timeout, fixed allowlist, byte truncation, hash/redaction metadata, no raw API/log exposure | Review each newly proposed tool before allowlisting |
| R13 | At-least-once tool execution can repeat an external read after a crash | ACCEPTED | Only read-only tools; Evidence uses `(agent_run_id, idempotency_key)` uniqueness and checkpoint safe points | Revisit only if non-idempotent tools are proposed |
| R14 | Lease takeover can encounter an orphan RUNNING step | MITIGATED | Expired owner loses optimistic authority; takeover marks orphan step FAILED with `lease_lost` before resume | Monitor takeover metrics in deployment |
| R15 | LLM output may be malformed, unsupported or cite foreign Evidence | MITIGATED | Strict JSON adapters, fixed allowlist, deterministic Evidence ownership/validity checks, one correction, degraded fallback | Expand adversarial model evaluation without weakening validator |
| R16 | Local active-run gauges are process-local rather than a database census | ACCEPTED | Counters and durable DB state are authoritative; gauge is explicitly locally observed | Add a bounded DB collector if fleet-wide queue SLO is required |
| R17 | `doc/` and `*_test.go` were previously ignored, so Phase 1 claims were not clone-reproducible | MITIGATED | Removed both ignore rules and retained the discovered Phase 1 artifacts unchanged | Review staged file list before publication |
| R18 | Agent Runtime currently shares `server-web` process resources | ACCEPTED | Opt-in default-off worker, bounded poll/lease/runtime/tool/model budgets, graceful cancellation | Consider process split only after measured load |
| R19 | No live external model E2E was executed in Phase 2 verification | OPEN | Deterministic fake-model Graph tests and real persistence tests cover runtime contracts; provider adapter compiles and is bounded | Run credentialed staging E2E before production enablement |

Phase 2 still does not implement GitOps, Argo CD, remediation, Loki, Tempo, controller-runtime, Kafka relay/inbox, frontend rewrite or legacy deletion.

## Phase 3 additions

| ID | Risk | Status | Phase 3 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R20 | Provider text or diff attempts prompt injection | MITIGATED | External data marker, strict schemas, sensitive-path/JSON redaction, fixed registry and deterministic diagnosis validator | Keep adversarial fixtures in regression |
| R21 | GitHub/Argo credentials or arbitrary hosts expand authority | MITIGATED | HTTPS fixed base URLs, file credentials, exact allowlists, no secret/effective-config logging, structured errors | Credentialed least-privilege staging review |
| R22 | Mutable image tag is mistaken for deployed Commit | MITIGATED | Complete Pod digest → Registry OCI → Argo → GitHub exact chain; tag/workflow/annotation/database fallbacks removed; `latest` is Unknown | Verify deployed OCI labels in staging |
| R23 | Time proximity promotes unrelated deployment | MITIGATED | Service/workload/namespace/application/revision/digest hard exclusions; confirmed requires exact revision and digest | Review representative production fixtures |
| R24 | Third-party latency holds database locks | MITIGATED | All external reads occur before the short atomic Change+Evidence transaction | Trace during staging load |
| R25 | Rate limits or provider outages produce false certainty | MITIGATED | Typed degraded/Unknown outcomes, bounded retry/Retry-After, no strong diagnosis without confirmed Evidence | Alert on provider result metrics |
| R26 | Credentialed GitHub, Argo CD and external-model E2E were not available locally | OPEN - PRODUCTION BLOCKER | Complete fake/mock, security, Agent trajectory and local gates; integrations default off | Run staging E2E before production enablement |
| R27 | OCI image inspection requires a Docker daemon unavailable in this environment | OPEN - PRODUCTION BLOCKER | Dockerfiles and CI inspect exact labels; static CI/Compose/Helm checks pass | Observe CI Docker job or run local daemon-backed build |
| R28 | Down migration while writers run can violate audit integrity | MITIGATED | Default-off flag, documented stop/lease drain/preserve/down order | Operator rollback rehearsal |

Phase 3 does not add GitOps write, PR creation, Argo CD Sync/rollback, Kubernetes mutation, remediation execution or Phase 4 capability.

## Phase 3.6A additions

| ID | Risk | Status | Code closure control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R29 | Registry reader becomes arbitrary-network or image-write capability | MITIGATED | Fixed HTTPS origin; exact host/repository/realm/redirect allowlists; immutable digest GET only; no layer/list/write API or model arguments | Credentialed staging network trace and RBAC review |
| R30 | Registry credentials leak through errors, cache, Evidence, logs or effective config | MITIGATED | File-only values, bounded generic errors, auth not cached, raw responses omitted, safe Registry ID, credential paths omitted from effective config | Staging secret-mount and log review |
| R31 | Provider outage or partial OCI metadata is promoted to confirmed | MITIGATED | Only complete verified non-truncated chain confirms; auth/outage/missing labels are degraded Unknown; exact mismatches conflict | Alert on bounded provider/resolution metrics |
| R32 | Same-digest worker fan-out overloads Registry | MITIGATED | Concurrent TTL/LRU cache with hard capacity and per-key in-flight collapse; waiter cancellation | Multi-replica staging load observation |
| R33 | Code closure passes locally but provider/rollout behavior is unproven | OPEN - STAGING BLOCKER | Mock TLS Registry, deterministic provider fixtures and full local regression | Credentialed provider/model E2E, multi-replica and rollback rehearsal |

Phase 3.6A introduces no Phase 4 authority and no production write operation.

## Phase 3.6B additions

| ID | Risk | Status | Validation control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R34 | Credentialed staging scopes remain unapproved | OPEN - STAGING BLOCKER | No provider, cluster, model, registry or staging database request was attempted; every unavailable gate is `NOT RUN` | Obtain explicit least-privilege scope before each E2E |
| R35 | The dirty worktree is not represented by the current GitHub commit | OPEN - STAGING BLOCKER | The current HEAD was not used as false provenance for uncommitted image content | Supply an immutable commit that exactly matches the tested source before consistency validation |
| R36 | No local or remote image builder is available | OPEN - STAGING BLOCKER | Static Dockerfile/CI checks were not substituted for daemon-backed Build/Inspect | Provide an approved unprivileged builder and Registry scope |
| R37 | Real Worker crash/takeover rehearsal lacks deploy-time flag wiring and stable crash points | OPEN - STAGING BLOCKER | Local MySQL lease tests remain separate from multi-replica evidence; no failpoint or staging rollout was added | Add a separately reviewed staging harness and expose existing Worker settings before rehearsal |

Phase 3.6B performed no Phase 4 work, external write, rollout, production migration or credentialed staging request.

## Phase 4 additions

| ID | Risk | Status | Phase 4 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R38 | Prompt injection expands remediation authority | MITIGATED | Planner schema omits repository/path/credential/policy/approval; trusted mapping and deterministic policy bind exact target | Keep adversarial planner fixtures |
| R39 | Approval is replayed for a changed plan or patch | MITIGATED | Independent approval row binds plan/patch hashes plus optimistic version; one decision and one ChangeRequest per Plan | Staging administrator audit review |
| R40 | GitHub write credential leaks into read/model paths | MITIGATED | Separate file mount, App token provider and client; no credential persistence or prompt exposure | Least-privilege GitHub App staging validation |
| R41 | Crash creates duplicate branch, commit or PR | MITIGATED | Unique delivery key, one ChangeRequest per Plan, lease takeover, branch/hidden-marker recovery | Real GitHub crash-window staging rehearsal |
| R42 | Base revision moves after approval | MITIGATED | Exact-SHA file read plus before/patch re-hash immediately before write | Preserve exact-SHA evidence in staging run |
| R43 | Local mocks differ from real GitHub/MySQL behavior | PARTIALLY MITIGATED - STAGING E2E DEFERRED | Real MySQL 8.0.46 migration/repository/concurrency/lease/crash gates pass; GitHub security/idempotency remains locally mock-verified | Separately run credentialed GitHub/GitOps/CI staging validation before enablement |

Phase 4 adds no merge, Argo CD write, Kubernetes write, shell/Git execution, filesystem writer, automatic recovery verification or Incident resolution.

## Phase 5 additions

| ID | Risk | Status | Phase 5 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R44 | Mutable branch, tag, or latest sync is mistaken for delivered commit | MITIGATED | Exact merged SHA and exact Argo deployed revision binding; mismatch is terminal fail-closed | Credentialed staging exact-revision trace |
| R45 | Provider outage or one transient sample resolves Incident | MITIGATED | `unavailable` never passes; every required check needs a server-clock continuous stability window | Staging outage and flapping rehearsal |
| R46 | Stale worker overwrites a takeover or duplicates resolution | MITIGATED | Owner lease, heartbeat, expiry takeover, optimistic version, terminal immutability and idempotent Timeline/Outbox keys; real MySQL concurrency test | Multi-replica staging crash rehearsal |
| R47 | Model/user injects query language, resource identity, or verdict | MITIGATED | Trusted compiler, typed Deployment-only subject, no write API, unsupported metric/log/trace plans rejected, no LLM verdict path | Keep adversarial compiler/API tests |
| R48 | Phase 5 read credential expands write authority | MITIGATED | ADR 0030 permits only existing Phase 3 GET-only clients; Phase 4 GitHub write client is absent from dependencies | Least-privilege staging credential review |
| R49 | Credentialed GitHub/Argo/Kubernetes/observability behavior differs from fakes | OPEN - PRODUCTION BLOCKER | Provider tests, full local gates and MySQL 8 pass; staging scopes remain explicitly `NOT RUN` | Supply approved staging repository/Application/cluster/endpoints |
| R50 | Metric/log/trace checks are claimed without safe adapters | MITIGATED | Types/ports exist but compiler rejects them; no raw PromQL/LogQL/Trace input; validation reported `NOT RUN` | Add separately reviewed bounded template adapters |
| R51 | Tested dirty source lacks immutable image/deployment provenance | OPEN - PRODUCTION BLOCKER | No commit, image push or external deployment claim was made | Commit reviewed source and validate exact image/revision chain separately |

Phase 5 adds no automatic merge, Argo CD or Kubernetes mutation, rollback, generic query language, shell, frontend rewrite, infrastructure deployment, legacy deletion, or Phase 6 capability.

## Phase 6 additions

| ID | Risk | Status | Phase 6 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R52 | A profile becomes a hidden arbitrary provider query language | MITIGATED | Strict unknown-field rejection, finite check/comparison enums, fixed infrastructure templates and no query/URL/tenant field | Review every new template in code and ADR |
| R53 | Provider outage, malformed/no-data or NaN/Inf resolves an Incident | MITIGATED | Typed observations fail closed; only explicit log absence treats no-data as success; continuous stability is still mandatory | Credentialed outage/flapping staging rehearsal |
| R54 | Log or trace payload leaks credentials or creates unbounded audit data | MITIGATED | Response/cardinality bounds; current Loki checks persist counts only; Tempo persists no attributes; raw bodies excluded from API/Timeline/Postmortem | Staging response/log redaction review |
| R55 | Postmortem turns a hypothesis into a confirmed fact | MITIGATED | Deterministic classification preserves fact/inference/unknown and Evidence IDs; absent confirmation remains unknown | Review representative resolved Incidents |
| R56 | Crash/takeover duplicates resolution, Outbox or Postmortem | MITIGATED | Existing lease/version checks, idempotent audit keys, unique Postmortem constraints and real MySQL two-owner rehearsal | Multi-replica credentialed staging crash rehearsal |
| R57 | Phase 6 down migration silently loses audit history | MITIGATED | Down drops only Postmortem; explicit data-loss comment/export procedure; Verification checks are retained | Approved backup/export before any production down |
| R58 | Local provider contracts differ from real Prometheus/Loki/Tempo or immutable deployed source | OPEN - PRODUCTION BLOCKER | httptest providers and disposable MySQL prove local semantics only; real endpoints and provenance remain NOT RUN | Credentialed staging plus immutable image/commit/revision evidence |

Phase 6 adds no GitHub merge, Argo CD or Kubernetes mutation, rollback, shell, kubectl, generic query language, infrastructure rewrite, frontend workbench or Phase 7 capability.

## Phase 7 additions

| ID | Risk | Status | Phase 7 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R59 | Browser recomputes Incident or Verification verdicts | MITIGATED | Components consume independent persisted server statuses; comparison, aggregate and stability are labelled server-authoritative and never recalculated | Keep pure presentation tests and DTO review |
| R60 | Existing broad DTO leaks provider/private/internal fields | MITIGATED | Dedicated Workbench allowlist DTOs; response absence tests; legacy DTO remains compatibility-only | Phase 8 caller inventory before DTO removal |
| R61 | Realtime becomes an unbounded or authoritative global state store | MITIGATED | Public-UUID incident scope, auth, refresh-only payload, monotonic suppression, bounded reconnect and query resync | Staging disconnect/soak remains NOT RUN |
| R62 | Slow response overwrites a newer Incident/filter request | MITIGATED | AbortController and request identity on list/detail/section composition; deterministic unit coverage | Browser E2E expansion in a later approved scope |
| R63 | Related Kubernetes context reads the wrong cluster or crosses scope | MITIGATED | Cluster must match authenticated server allowlist before typed bounded namespace query; mismatch fails unavailable; no raw/YAML/log/Secret/write view | Credentialed multi-cluster UI validation NOT RUN |
| R64 | Bounded list summary composition creates unacceptable N+1 latency | OPEN - NON-PRODUCTION BLOCKER | Page cap 50 and independent failure states bound work; no schema/index change authorized | Measure staging query count/latency before production |
| R65 | Legacy navigation label is mistaken for deletion approval | MITIGATED | All routes/code remain and cleanup inventory is explicitly deferred | Separate Phase 8 approval and traffic evidence |

Phase 7 adds no schema change, Provider query capability, GitHub/Argo/Kubernetes mutation, staging deployment, production release, legacy deletion or Phase 8 implementation.

## Phase 8 additions

| ID | Risk | Status | Phase 8 control | Owner / next gate |
| --- | --- | --- | --- | --- |
| R66 | A tracked generated kind kubeconfig exposed client certificate/key material | MITIGATED LOCALLY | File removed and path ignored; setup regenerates local-only credentials; no value copied to reports | Delete/rotate any still-running historical kind cluster credential; Git history rewrite requires separate authorization |
| R67 | Browser WebSocket JWTs in URL query strings leak through URLs or intermediaries | MITIGATED | Frontend uses `cloudops-bearer` subprotocol; backend rejects query tokens; Authorization header remains for non-browser clients; regression tests added | Notify and migrate any external subscriber discovered in staging |
| R68 | Workbench reconnect claim was stronger than source behavior | MITIGATED | Retry count no longer resets on every successful connection; eight-attempt cap, parser/sequence bounds and disconnect resync are tested | Credentialed disconnect/soak remains staging evidence |
| R69 | Legacy deletion could break active callers, history or compliance evidence | OPEN - DESTRUCTIVE CLEANUP DEFERRED | Caller matrix records explicit retention/deprecation/block decisions; no business code or schema deleted | Staging traffic/subscriber inventory, parity, compliance and rollback approval |
| R70 | Release workflow previously used mutable `latest`, tag-based deployment and disabled attestations | MITIGATED IN CODE / UNVERIFIED EXTERNALLY | `v*` only publication, commit-SHA tag, SBOM/provenance, scan, keyless signature, protected environment and digest deploy | Execute reviewed release in credentialed staging/CI and verify Registry attestations/signature/digest |
| R71 | Current uncommitted Phase 8 source cannot have exact commit image provenance | OPEN - PRODUCTION BLOCKER | Local images use no false HEAD provenance; CI can stamp only a checked-out immutable commit | Separate commit/publish authorization, successful image build/inspect and exact deployed-source chain |
| R72 | Local kind E2E is unavailable | OPEN - NON-PRODUCTION BLOCKER | `kind` is missing and no kubectl context exists; Helm/raw manifest schema validation still passes but is not E2E | Provide an approved local kind environment without external credentials |
| R73 | Reachable frontend and Go/toolchain vulnerabilities existed in the baseline | MITIGATED | Lockfile audit fixed all npm advisories; Go 1.26.5 closes reachable standard-library issues; quic-go upgraded to 0.59.1; pinned govulncheck reports zero reachable vulnerabilities | Keep npm audit, govulncheck and image scan enforced in CI |

Phase 8 performs no schema change, legacy data drop, external credential use, Registry push, signature, cluster write, deployment or production release.
