# CloudOps-Copilot Domain

CloudOps-Copilot provides one product language for observing cloud-native systems, responding to operational problems, and delivering controlled changes.

## Language

**CloudOps Operations Platform**:
A unified product for operational visibility, incident response, Agent-assisted investigation, controlled delivery and platform administration. The Incident Workbench is one domain within the product, not the product itself.
_Avoid_: Incident-only product, Incident Agent demo, monitoring portal

**Product Contract**:
The sole supported first-party CloudOps interface and persisted product vocabulary, identified as V1 only where an explicit contract identity is required. Internal domain language remains semantic rather than generation-prefixed.
_Avoid_: V2, V3, runtime generation, phase-owned domain

**Incident Workbench**:
The domain in which an operational problem is investigated, remediated and verified as one traceable Incident lifecycle.
_Avoid_: Entire platform, generic dashboard

**Signal**:
An immutable observation received from a monitoring or external source. A Signal is source evidence, not a user-managed Alert or Incident lifecycle.
_Avoid_: Alert, Incident

**Alert**:
A time-bounded operational condition that may be acknowledged, silenced, investigated, correlated or resolved without becoming an Incident.
_Avoid_: Signal, Incident, notification

**Alert Acknowledgement**:
The Owner's durable indication that a firing Alert has been seen. It does not suppress provider notifications or claim that the condition has recovered.
_Avoid_: Silence, resolution, assignment

**Alert Silence**:
A time-bounded provider-backed suppression of notifications for a defined Alert match. It does not change whether the underlying Alert is firing or resolved.
_Avoid_: Acknowledgement, resolution, deletion

**Escalation Policy**:
A versioned rule that may promote a matching Alert into an Incident based on explicit severity, scope, duration or recurrence conditions.
_Avoid_: Unconditional ingestion, hidden correlation, notification route

**Incident**:
A coordinated response case for investigation, remediation and recovery verification. An Incident may group multiple related Alerts, while an Alert may exist without an Incident.
_Avoid_: Alert, raw signal

**Provider-backed Capability**:
A capability presented and governed as part of CloudOps while its source facts or external effects remain owned by a specialized cloud-native provider.
_Avoid_: External link, embedded provider console, duplicated provider

**Workspace**:
A first-class CloudOps product area with its own route, operating context and related actions. A Workspace is not a section hidden inside another domain's detail page.
_Avoid_: Dashboard card, Incident Detail section, external console

**Context Link**:
A typed navigation relationship that opens an exact internal resource or an allowlisted provider resource with enough identity and time context to continue the same investigation.
_Avoid_: Provider home page, manually assembled URL, unlabelled external link

**Owner Notification**:
A durable, deduplicated notice that an Alert state, Agent outcome, authorization request or Operation result requires the Owner's awareness, linked to its exact operating context.
_Avoid_: Alert, Signal, raw telemetry change, email, provider notification route

**Notification Inbox**:
The native CloudOps record of Owner Notifications and their read state. It remains authoritative when selected urgent notifications are also mirrored by the browser.
_Avoid_: Browser notification history, Alert list, external messaging channel

**Local Owner Context**:
The trusted local-use boundary in which the sole Owner can access every native CloudOps workspace without a login flow. It does not grant or represent a provider identity.
_Avoid_: User session, provider credential, public access

**Owner**:
The sole person who operates and configures this personal CloudOps project within the Local Owner Context. Owner authority does not replace confirmation, audit or external-provider safety controls.
_Avoid_: Viewer, operator, platform administrator, anonymous user

**Bootstrap Configuration**:
The minimal local facts required to start CloudOps and reach its durable settings. Changes take effect only after an explicit local restart.
_Avoid_: Runtime setting, provider preference, editable application setting

**Operational Configuration**:
Owner-managed settings that define Agent behavior, provider connections and cloud-native operating scope after CloudOps has started.
_Avoid_: Environment-only configuration, deployment manifest, Bootstrap Configuration

**Configuration Revision**:
An immutable version of Operational Configuration, including references to the secret versions it requires. Work already in progress remains bound to its original Configuration Revision.
_Avoid_: Mutable global settings, latest environment variables, unversioned secret

**Operational Scope**:
The currently selected cluster, environment, namespace set and time range that constrains native Workspace queries and Context Links.
_Avoid_: Global implicit context, merged multi-cluster graph, provider home page

**Workload**:
A Kubernetes controller-level operating identity, currently a Deployment, StatefulSet or DaemonSet, whose Pods realize the desired runtime state.
_Avoid_: Pod, Service, arbitrary Kubernetes object

**Kubernetes Resource Projection**:
A bounded, sanitized and typed CloudOps representation of a real Kubernetes resource within one Operational Scope.
_Avoid_: Raw YAML, unstructured object dump, fabricated resource

**Topology Relationship**:
An attributable structural relationship between Kubernetes Resource Projections, derived from ownership, selection, endpoints, scheduling or backend references.
_Avoid_: Visual proximity, inferred dependency, telemetry correlation

**Topology Snapshot**:
A bounded observation of resource projections and topology relationships collected from one active cluster at a known time and Configuration Revision.
_Avoid_: Kubernetes authority, telemetry archive, multi-cluster graph

**Operations Atlas**:
The real-resource topology view of one active cluster, progressively revealing Kubernetes structure while telemetry and Agent activity remain identifiable overlays.
_Avoid_: Decorative 3D scene, synthetic dependency graph, multi-cluster hairball

**Cloud-Native Evidence Plane**:
The provider-backed set of attributable Kubernetes, metric, Alert, log and trace observations correlated by resource identity and time for native Workspaces and Agent reasoning.
_Avoid_: Duplicated telemetry store, provider console collection, unproven model context

**Evidence**:
A bounded, sanitized and attributable observation retained by CloudOps with its source identity, collection time, effective query and Operational Scope.
_Avoid_: Raw telemetry archive, model statement, unsupported conclusion

**Operational Loop**:
The product's single observe, detect, investigate, decide, act and verify progression. It may complete without an Incident or DevOps change, but every outcome returns to observable verification.
_Avoid_: Mandatory GitOps pipeline, disconnected dashboards, chat-only diagnosis

**Live Mode**:
The normal product state in which every resource, observation, Alert, Evidence item and Agent result comes from configured real Providers and CloudOps domain records.
_Avoid_: Seeded dashboard, hidden fixture, fabricated topology

**Demonstration Scenario**:
An explicitly activated, project-owned cloud-native workload and controlled fault that produces real Provider telemetry and exercises the Operational Loop without claiming to be Live Mode.
_Avoid_: Frontend mock, fake API response, production incident

**Query Definition**:
A versioned, bounded read-only observability query that the Owner can run directly or authorize the Agent to execute within its declared provider and Operational Scope.
_Avoid_: Unrestricted provider access, mutable query text, Agent-generated command

**Query Authorization**:
The Owner's revocable authority for the Agent to execute either one exact query once or one Query Definition version repeatedly within fixed provider, scope and resource bounds.
_Avoid_: Permanent provider permission, approval of future edits, unrestricted Agent access

**Query Execution**:
An audited attempt to run one exact normalized query, bound to its actor, Provider, Operational Scope, Configuration Revision, time range and resource limits. It retains execution metadata and attributable references, not a long-lived copy of the Provider result.
_Avoid_: Query Definition, telemetry archive, unrestricted Provider request

**Operation Plan**:
An immutable proposal for one external or high-impact effect, binding its exact target, parameters, diff, preconditions, risk, expiry and verification intent.
_Avoid_: Chat suggestion, shell command, mutable draft, blanket permission

**Action Authorization**:
The Owner's decision permitting one exact action card or Operation Plan to execute. Any material change produces a new action identity and requires a new decision.
_Avoid_: Agent role, reusable write access, approval of future changes

**Cloud-Native Operations**:
The primary product domain that relates Kubernetes state, metrics, Alerts, logs and traces into an actionable view of system behavior.
_Avoid_: Generic infrastructure portal, GitOps delivery pipeline

**Incident Agent**:
The core product capability that investigates cloud-native operational problems using bounded tools and attributable evidence, then presents findings and proposed actions to the Owner.
_Avoid_: Generic chatbot, autonomous cluster administrator

**Agent Investigation**:
A durable Agent run triggered for an Alert or Incident, with bounded steps, tool calls, Evidence and a diagnostic outcome.
_Avoid_: Chat conversation, background magic, untracked model call

**Agent Consultation**:
A persistent Owner-initiated diagnostic conversation that works from explicit Context Snapshots, can inspect cloud-native facts through authorized bounded read tools and cites Evidence in each answer.
_Avoid_: Generic chat, arbitrary prompt console, direct action executor

**Context Snapshot**:
An immutable attachment of Operational Scope, selected resources, time range, Query Definition versions, Evidence references and Configuration Revision to an Agent Consultation at a known moment.
_Avoid_: Live hidden page state, mutable global selection, copied provider response

**Consultation History**:
The durable messages, Context Snapshots, Evidence references and Agent outputs belonging to one Agent Consultation. It remains available inside that Consultation but is never silently imported into another.
_Avoid_: Global memory, Knowledge Item, hidden prompt context

**Knowledge Item**:
An Owner-confirmed, versioned and scoped piece of reusable guidance derived from an attributable conclusion, preference, known environment fact or operating experience. It may inform future Agent work but never establishes the current state of a system.
_Avoid_: Hidden memory, current Evidence, automatic conversation recall, Runbook Guidance

**Runbook Guidance**:
Versioned Git-managed operational guidance retrieved for a bounded problem context. It may guide an investigation but is neither Evidence nor authority to act.
_Avoid_: Root-cause proof, Operation Plan, Knowledge Item

**GitOps Integration**:
A supporting capability that contributes change context and can deliver an explicitly accepted change through Git and reconciliation. It is not the product's core or a required stage of every investigation.
_Avoid_: Product core, mandatory Incident outcome, general CI/CD platform
