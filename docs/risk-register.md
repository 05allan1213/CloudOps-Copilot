# V3 Risk Register

> Status: Phase 2 local schema/queue/API/registry evidence recorded; subject-bound handlers remain explicitly assigned to Phases 4-6 and later-phase/external risks remain open
>
> Normative source: [`CloudOps-Incident-Agent-V3-Refactor-Design.md`](CloudOps-Incident-Agent-V3-Refactor-Design.md)
>
> Phase 0 audited source: `main@2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e`
>
> Phase 1 implementation is the local commit `main@748fa3f946321ead2cdbf6ef0e710e05566a620c`; its parent is `1ea0c3a21ed3ed1f822399f205afac225b1d5464`, and that 247-path commit preserves the original staged binary diff identity

## 1. Status and Severity

Treatment status:

- `CONTROLLED`: a current control is implemented and evidenced for the stated boundary.
- `OPEN`: the risk is real and assigned to a later Gate; no mitigation is claimed yet.
- `ACCEPTED`: an explicit Demo limitation is within V3 non-goals and must remain honestly documented.
- `NOT RUN`: the required live validation was not executed; no old/mock evidence substitutes for it.
- `BLOCKER`: the owning phase cannot pass until the exit condition is met.

Severity combines likelihood and impact:

| Impact / Likelihood | Low | Medium | High |
|---|---|---|---|
| Low | LOW | LOW | MEDIUM |
| Medium | LOW | MEDIUM | HIGH |
| High | MEDIUM | HIGH | CRITICAL |

Phase 0 `PASS` means the risk is evidenced, owned and gated. It does not mean an `OPEN`, `NOT RUN` or `BLOCKER` risk is mitigated.

## 2. Architecture and Cutover

| ID | Severity | Risk and trigger | Current evidence | Treatment / owner | Status |
|---|---|---|---|---|---|
| ARCH-001 | HIGH | Empty root directories are mistaken for a completed root module, causing a parallel implementation | Root `go.mod`/`go.sum`, feature-first `internal/` and root migrations build; no nested module, replace or old import remains | Phase 1 mechanical move, root dependency checks and negative scans | CONTROLLED locally in Phase 1; upstream publication and hosted CI remain NOT RUN |
| ARCH-002 | CRITICAL | API process continues to run Agent/Delivery/Verification and carries write/read credentials | API `go list -deps` and `go tool nm` contain zero legacy worker/LLM/GitHub-write/delivery-worker entries; API uses repository-only facades. Fast Demo compatibility still initializes K8s when explicitly enabled, and shared legacy config remains | Phase 1 process package split and binary negative checks; later credential/K8s-token hardening is separate | Worker-loop boundary CONTROLLED locally; K8s-token/config residual OPEN for later phase |
| ARCH-003 | CRITICAL | Compose, raw Kubernetes and Helm drift, allowing a shortcut deployment to be mistaken for V3 | 14-service Compose, 18 raw files and a 54-resource Chart | Delete parallel sources only after kind+Helm replacement and Golden proof | OPEN; Phase 7B BLOCKER |
| ARCH-004 | HIGH | Runtime/task cutover and product/contract cutover are conflated, enabling early irreversible deletion | Phase 2 added compatibility schema/code only; scans found no conversion or `CUTOVER_V3`, and no old/new Worker pair was run | Design and ledger distinguish Phase 2 code Gate, Phase 7A cutover/Golden, Phase 7B contract release | CONTROLLED locally through Phase 2; live cutover remains NOT RUN |
| ARCH-005 | HIGH | Root/process/API changes alter current `/api/v2` behavior and hide regressions | Root ordinary/race tests, exact V2 route table, representative handler responses, frontend tests and API facades pass; V3 routes are separately mounted | Exact move map, compatibility facades, immutable migrations and V2 regression Gate | CONTROLLED locally through Phase 2; deployed traffic parity NOT RUN |
| ARCH-006 | MEDIUM | Frontend root-path ownership is unclear and creates a second UI | Current UI is under `server-monitor/frontend`; target is root `frontend` | Keep current path through Phase 1; move once during Phase 6 Workbench adaptation | CONTROLLED in architecture |
| ARCH-007 | HIGH | An early-phase Helm/preflight profile starts incomplete Worker/OAuth/model integration, causing CrashLoop or making Phase 3 depend on Phase 4-6 credentials | Phase-order review found the original final bootstrap list was unconditional | Explicit Phase 3/4/5/6 profiles, rendered negative tests, and credential checks enabled only in the owning profile | Design CONTROLLED; Phase 3 implementation/clean-kind proof OPEN |

## 3. Data Compatibility and Async Runtime

| ID | Severity | Risk and trigger | Current evidence | Treatment / owner | Status |
|---|---|---|---|---|---|
| DATA-001 | CRITICAL | Editing deployed migration history invalidates schema provenance | Root `migrations/00001-00007` retain every recorded blob and SHA-256 byte-for-byte; Phase 2 adds only `00008` and immutable hash tests pass | Immutable blobs; CI checksum test; forward migrations only | CONTROLLED locally through Phase 2; external history audit remains separate |
| DATA-002 | CRITICAL | Explicit schema expansion makes existing/fresh databases unreadable or changes legacy data | `00007` plus exact `00008` blob/SHA above; disposable MySQL 8.0 fresh/existing version-8 parity SHA `90b697...`, unchanged ten-table sentinel SHA `01320e...`, repeat/concurrent/lock and DML-only runtime proof | Explicit Goose ownership; production AutoMigrate scan empty; retain legacy rows until later archive/contract Gates | CONTROLLED locally through Phase 2; existing deployed database inventory NOT RUN |
| DATA-003 | CRITICAL | 11-to-7 state compression loses in-flight truth or falsely resolves an Incident | V2 has top-level FAILED and direct resolved-Signal path | Maintenance lock, deterministic mapping, child-priority rules, append migration Event | OPEN; Phase 7A BLOCKER |
| DATA-004 | CRITICAL | Three legacy leases and new async tasks both claim the same logical work, or removal of legacy loops silently drops business execution | Final V3 worker dependency/source/nm scans contain only `async_tasks` claim and no legacy claim package/symbol. Missing subject-bound operations fail construction before claim; Phase 2 implements only `investigation.start` under the corrected ownership boundary | Phases 4-6 implement Agent single-step, V3 remediation prepare, one-write change, one-source delivery observe and one-check verification without legacy lease writes; Phase 6 proves full Worker readiness; Phase 7A later drains/converts live data | Phase 2 claim boundary CONTROLLED; operations OPEN in Phases 4-6; Phase 7A live-cutover BLOCKER |
| DATA-005 | CRITICAL | Treating domain outbox rows as jobs creates duplicate work/external effects | Outbox only has Add/PendingCount and no claim/relay | Archive all outbox rows; tasks derive only from versioned child converters | CONTROLLED in spec; conversion NOT RUN |
| DATA-006 | HIGH | V2 GraphState checkpoint is resumed as V3 StateDelta with stale/foreign Evidence | 00002 stores whole checkpoint and local lease | Versioned converter with schema/hash/cycle/tool/budget checks; failure cancels and starts new Run | OPEN; Phase 4/7A BLOCKER |
| DATA-007 | HIGH | V2 Verification profile/Loki observations are treated as V3 samples or recovery proof | 00005/00006 lack cycle/no-change/inconclusive/sample semantics | Convert only compatible typed profiles; otherwise cancel and investigate | OPEN; Phase 6/7A BLOCKER |
| DATA-008 | CRITICAL | Old Approval authorizes a new PR without V3 hash binding | 00004 binds only plan/patch hash | Old Approval is archive-only; new Plan and human Decision required for any write | CONTROLLED in spec; Phase 5 negatives OPEN |
| DATA-009 | HIGH | V2 Postmortem narrative becomes V3 Evidence/ResolutionReport | `postmortems` stores narrative JSON and one row/Incident | Preserve id/hash/time in legacy archive only | CONTROLLED in spec; archive NOT RUN |
| DATA-010 | HIGH | Backfill silently drops/duplicates rows or crosses cycles | Current row counts/hashes were not queried | Batch ledger with ranges/counts/input-output hashes and fail-closed cutover | NOT RUN; Phase 7A BLOCKER |
| DATA-011 | HIGH | Generated active keys or canonical identities collide due to concat/collation errors | Disposable MySQL 8.0 information-schema expression checks and duplicate-key negatives passed for Incident, AgentRun, Plan, ChangeRequest, VerificationRun and versioned trigger identity | Versioned fixed-width binary/composite identity, fixed collation/length, real MySQL negatives | CONTROLLED locally in Phase 2; deployed schema inventory NOT RUN |
| DATA-012 | HIGH | A V2 `RESOLVED` row created by Signal alone violates V3 recovery invariant | `incident/service.go:280-339` | Preserve resolved only with compatible passing Verification; otherwise investigate + attention | OPEN; Phase 7A BLOCKER |

## 4. External Side Effects, GitHub and Argo CD

| ID | Severity | Risk and trigger | Current evidence | Treatment / owner | Status |
|---|---|---|---|---|---|
| SIDE-001 | CRITICAL | Worker crashes after GitHub accepted branch/commit/PR and repeats the write | V2 worker advances multiple writes without V3 marker/fencing | One write phase per Task, pre-write intent, stable logical operation key and read reconciliation | OPEN; Phase 5 BLOCKER |
| SIDE-002 | CRITICAL | Direct K8s scale/patch path bypasses Git, Approval and Argo | `internal/infra/k8schange`, fast-demo API/script | Remove from runtime/deploy assets; negative RBAC and source scans | OPEN; Phase 7A BLOCKER |
| SIDE-003 | CRITICAL | A resolved Signal cancels a Task while an external request is already in flight | V2 lacks durable external write intent boundary | Marker before call; running/unknown write only reconciles; stale worker cannot persist | OPEN; Phase 5 BLOCKER |
| SIDE-004 | CRITICAL | Helm `--atomic` or database rollback starts an old binary after V3 state exists | Current CI tag deploy directly invokes Helm with kubeconfig | CUTOVER marker refusal; forward-fix only after claim; delete direct deploy job | OPEN; Phase 7A/7B BLOCKER |
| GH-001 | HIGH | GitHub App `contents:write` can affect more than the allowed path if Worker is fully compromised | GitHub IAM cannot enforce path-level permission | Fixed repo/base/path adapter, no merge API, ruleset/no-bypass, explicit threat-model limit | OPEN live audit; fully compromised Worker ACCEPTED non-goal |
| GH-002 | CRITICAL | Same-named or stale CI check is accepted for the remediation PR | Current clients do not bind full producer/workflow identity | Require current unique head whose tree/post-image matches Approval, plus check name/producer/workflow/head | OPEN; Phase 5 BLOCKER |
| GH-003 | HIGH | Base moves or PR head/tree is modified after Approval | Current V2 hashes are incomplete | Re-run preflight before each write; no rebase/update branch; invalidate and investigate | OPEN; Phase 5 BLOCKER |
| GH-004 | HIGH | Existing branch/commit without complete Draft PR is "completed" during cutover under old Approval | Legacy artifacts may exist with unknown side effect | Reconcile and archive; never issue the missing write; attention + new Plan | CONTROLLED in ledger; live inventory NOT RUN |
| ARGO-001 | CRITICAL | Argo skips the approved merged SHA and deploys a successor; system falsely calls it recovered | No current V3 Application/evidence | Require sync revision and successful syncResult revision equal exact merged SHA; successor -> failed | OPEN; Phase 6/7 BLOCKER |
| ARGO-002 | CRITICAL | CloudOps or CI actively syncs/rolls back, breaking Git-as-truth | Current CI directly deploys Helm; no V3 Argo RBAC asset | App get-only identity; source scan and `can-i` negatives; Git-only write | OPEN; Phase 5/7 BLOCKER |
| ARGO-003 | HIGH | AppProject kind/repo restrictions are misrepresented as object-name security | AppProject cannot restrict object names | Single source/path, rendered-manifest policy and adapter allowlist; honest boundary | OPEN; Phase 3/5 Gate |

## 5. Observability and Evidence

| ID | Severity | Risk and trigger | Current evidence | Treatment / owner | Status |
|---|---|---|---|---|---|
| OBS-001 | CRITICAL | Replacing the current stack breaks one of Metric/Log/Trace/K8s paths while UI still looks healthy | Current stack is hand-written Prom/Elastic/Fluent Bit/Jaeger | Real kind data-path contracts and same-request correlation before removing old assets | OPEN; Phase 3 BLOCKER |
| OBS-002 | CRITICAL | `no_data` or source outage is interpreted as zero errors and passes Verification | Current V2 semantics lack full V3 sample/source health contract | Require healthy/complete/in-retention query and min samples; outage -> inconclusive | OPEN; Phase 6 BLOCKER |
| OBS-003 | HIGH | ECK/Elastic/Filebeat versions are incompatible or Beat v1beta1 changes | No tested V3 version matrix | Pin Chart/package/image digests and test one supported matrix; record beta risk | NOT RUN; Phase 3 BLOCKER |
| OBS-004 | HIGH | Filebeat reads unrelated namespace logs or loses/duplicates offsets | Current Fluent Bit is cluster-scoped; V3 path absent | Namespace autodiscover allowlist, canary negative and registry restart boundary | NOT RUN; Phase 3 BLOCKER |
| OBS-005 | HIGH | OTLP sender spoofs target workload identity in kind without auth/NetworkPolicy | V3 Demo has no authenticated OTLP/CNI boundary | k8sattributes cache validation, cross-signal authority, spoof negative; document threat model | OPEN; Phase 3 BLOCKER, cluster-compromise ACCEPTED |
| OBS-006 | HIGH | Prometheus/Tempo query "read-only" is claimed as infrastructure IAM | Those APIs lack per-client query RBAC in this topology | Treat as code/config capability boundary; no admin/lifecycle endpoints | CONTROLLED in design; contract OPEN |
| OBS-007 | HIGH | Telemetry revision labels are treated as deployment authority | Current components mix revision concepts | Kubernetes imageID + Registry + GitHub + Argo remain authority; telemetry only correlates | OPEN; Phase 3/5 Gate |
| OBS-008 | HIGH | External text injects instructions or secrets into Agent state | Logs/Event/diff/PR/Runbook are untrusted text | Normalize/redact before model; schema/reducer/tool allowlist; canary eval | OPEN; Phase 4 BLOCKER |
| OBS-009 | MEDIUM | Incident/Run/User IDs become Prometheus labels and exhaust series | Current/future instrumentation surface is broad | Enum/bounded labels only; static review and cardinality tests | OPEN; Phase 3-6 Gate |

## 6. Resources, Credentials and Runtime Environment

| ID | Severity | Risk and trigger | Current evidence | Treatment / owner | Status |
|---|---|---|---|---|---|
| RES-001 | HIGH | Full ECK/Tempo/Argo/Prometheus stack exceeds laptop CPU/memory/disk | Only old static requests/limits: about 3376Mi/7872Mi; V3 peak unknown | Preflight fail-closed; clean-install peak measurement; freeze Demo profile | NOT RUN; Phase 3 BLOCKER |
| RES-002 | MEDIUM | Existing ports/containers/kind conflict with a clean Demo and get deleted automatically | Two old Compose groups and `cloudops-demo` exist; cluster API returns EOF | Preflight reports conflict and stops; never auto-delete user state | CONTROLLED policy; cleanup NOT RUN |
| RES-003 | HIGH | Single-node data components are described as HA/production/DR | V3 explicitly targets local kind single replicas | Keep explicit non-goal and evidence wording | ACCEPTED Demo limitation |
| CRED-001 | HIGH | Committed/local default passwords and JWT are mistaken for deployable credentials | `.env.example`, Chart/raw Secret and local auth contain change-me/local values | Pre-created Secret refs; OAuth; secret scan; remove local user/JWT | OPEN; Phase 5/7 BLOCKER |
| CRED-002 | CRITICAL | Ignored kubeconfig/private key leaks or is reused as evidence | `server-monitor/docker/kubeconfig` exists ignored, mode 0600; contents not read | Never stage/log; delete during bounded cleanup; CI secret scan | OPEN cleanup; current non-disclosure CONTROLLED |
| CRED-003 | HIGH | GitHub App, Argo, Elastic, OAuth or LLM credential scope is broader than intended | No current V3 live credential audit | Separate identities/files, `can-i`/installation/ruleset negatives and rotation runbook | NOT RUN; owning live Gates BLOCKED |
| CRED-004 | HIGH | oauth2-proxy accepts spoofed headers or forwards access/session secrets to API | V3 auth not implemented | Clear/overwrite identity headers; strip OAuth token/session cookie; forged-header contract | OPEN; Phase 5 BLOCKER |
| CRED-005 | HIGH | CSRF token is not bound to the authenticated identity because proxy owns session | Phase 2 V3 skeleton signs provider/login-bound short-lived tokens and passes cross-user, expiry, missing/forbidden Origin, CORS/header and role contract tests using the compatibility authenticator | API-signed identity-bound token, memory-only frontend transport, Origin and expiry checks; repeat against oauth2-proxy in owning phase | CONTROLLED locally for compatibility API; oauth2-proxy integration OPEN |
| CRED-006 | MEDIUM | GitHub login is treated as immutable numeric subject | oauth2-proxy contract exposes login, not proven numeric ID | Audit provider+login+authenticated request time and state mutability limitation | ACCEPTED V3 limitation |

## 7. CI, UI, Documentation and Golden E2E

| ID | Severity | Risk and trigger | Current evidence | Treatment / owner | Status |
|---|---|---|---|---|---|
| CI-001 | HIGH | CI reports green while stale explicit test paths fail or root V3 files do not trigger | Root Go matrix/build paths cover `cmd/`, `internal/`, `migrations/`, Dockerfile and Makefile; stale `internal/copilot/*/eval` steps are removed; actionlint PASS | Keep root path matrix and run exact local commands; hosted execution remains separate | CONTROLLED locally in Phase 1; hosted run NOT RUN |
| CI-002 | HIGH | Static validators pass but real MySQL/concurrency/kind behavior is untested | Disposable MySQL migration, generated-key/EXPLAIN, SKIP LOCKED, fencing, idempotency, Incident concurrency and shutdown takeover checks pass; kind/hosted behavior was intentionally not run | Keep PR integration and MANUAL_GOLDEN separate; literal NOT RUN for external systems | Phase 2 persistence/runtime mechanics CONTROLLED locally; kind/hosted remain OPEN |
| UI-001 | HIGH | viewer cannot inspect complete remediation, or browser computes/obscures verdict | Current viewer skips remediation and UI has no complete diff/commands | Server projection; viewer read access; operator-only commands; UI contract/a11y tests | OPEN; Phase 6 BLOCKER |
| UI-002 | MEDIUM | SSE reconnect loses updates or exposes Bearer token | Current client has Authorization and no Last-Event-ID | Cookie auth, cursor/Last-Event-ID and refresh-only hints | OPEN; Phase 6 BLOCKER |
| DOC-001 | HIGH | README or external V2 spec is treated as normative and reintroduces Kafka/Loki/Argo Rollouts/direct demo | README V2 language/broken links; external V2 spec calls itself authoritative | V3-only authority banner; architecture/ADR/evidence separation; docs-only path | CONTROLLED in Phase 0 after staged update |
| E2E-001 | CRITICAL | V2 direct-scale/fixed-model demo is reused as V3 Golden proof | Historical report used direct K8s and old SHA | Golden requires real GitOps PR/human merge/Argo exact SHA/real model/current SHA | NOT RUN; Phase 7 BLOCKER |
| E2E-002 | CRITICAL | Old image/run evidence is attached to a new commit | Running images are `8212b96...`/`2acec159...`, not HEAD | Rebuild and rerun every final exact SHA; manifest binds source/image/GitOps/run URL | NOT RUN; Phase 7 BLOCKER |
| E2E-003 | CRITICAL | Fixture Agent quality is presented as real-model quality | Runtime-bound v6 at `6c5eb09` passed 18/18 calibration, the frozen 39/39 formal quality Gate, prompt-injection 3/3 and secret-canary 3/3 with zero safety violations; migrated legacy facts are excluded by policy | Keep fixture guardrails, real-model quality and hostile surveys as separate exact-source evidence; rerun a new frozen revision after any bound Eval/runtime change | CONTROLLED for current bound Agent source; Golden remains separate |
| E2E-004 | HIGH | Missing OAuth/App/ruleset/Argo/model credentials is softened into partial PASS | Required external systems were not used in Phase 0 | Literal NOT RUN and stop at Gate; no mock substitution | NOT RUN; Phase 7 BLOCKER |
| E2E-005 | MEDIUM | 3-5 minute walkthrough is presented as recovery SLO | Historical demo timings differ and include shortcuts | Measure bad merge -> detect -> Agent -> approvals -> fix -> sync -> stable pass by segment | NOT RUN; Phase 7 BLOCKER |
| E2E-006 | HIGH | Contract cleanup destroys rollback/audit before the Golden chain is accepted | Legacy deletion was previously listed in same broad phase | Release A cutover+Golden; independent Release B contract with export/zero-caller proof | CONTROLLED in ledger; execution NOT RUN |

## 8. Phase 0 Review

| Review control | Status |
|---|---|
| Architecture migration risks covered | PASS |
| Data compatibility and cutover risks covered | PASS |
| External side-effect and GitHub/Argo risks covered | PASS |
| Observability and Evidence risks covered | PASS |
| Resource and credential risks covered | PASS |
| Golden E2E and provenance risks covered | PASS |
| Every CRITICAL/HIGH risk has a treatment/owner and either an exit condition or an explicit accepted boundary | PASS |
| Real mitigation/live validation for future Gates | NOT RUN or OPEN as listed |

No risk in this register authorizes scope expansion. A new material risk found during implementation must be appended, assigned and gated before work continues.

## 9. Phase 2 Review

| Review control | Status |
|---|---|
| Forward schema, immutable history, parity and legacy-data preservation | PASS |
| MySQL queue claim/takeover/retry/dead/replay/heartbeat/version fencing | PASS |
| Four-pool backpressure, isolation and bounded shutdown recovery | PASS |
| V3 API skeleton and V2 regression | PASS |
| Old lease claim packages and symbols absent from V3 worker | PASS |
| Typed registry, `investigation.start` and missing-operation fail closed | PASS |
| Subject-bound business operations | NOT RUN; owning Phases 4-6 |
| Backfill, live state/task conversion, cutover marker, concurrent old/new workers | NOT RUN |
| External GitHub/Argo/Kubernetes writes or deployment | NOT RUN |

The authorized phase-boundary correction removes the circular requirement to implement Phase 4-6 semantics in Phase 2. Phase 3 may proceed with observability and Demo only; the incomplete Worker stays disabled, and fail-closed startup remains safety evidence rather than evidence that missing business operations exist.
