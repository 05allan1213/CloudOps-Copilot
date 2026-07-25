# ADR 0031: Single-Cluster Operations Atlas Scope

- Status: Accepted topology-scope decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0029 and ADR 0030

## Context

CloudOps may connect to more than one local or cloud-native cluster, but combining every cluster and low-level Kubernetes object in one animated graph would obscure failures, destabilize layout and make the Overview slower than the operational work it is meant to accelerate. The current backend reads only a bounded subset of Deployment, Pod and Service data for an Incident and has no product-level topology projection.

## Decision

The Operations Atlas renders one active cluster at a time. Operational Configuration may register multiple cluster connections, while a persistent Operational Scope selector chooses the active cluster, environment, namespace set and time range. The default is the configured local kind cluster. Cross-cluster information is limited to health summaries and direct scope switching; resources from multiple clusters are never merged into one topology scene.

The default topology level presents Namespace, Service and Workload structure. Selection or zoom progressively reveals Pods, Nodes and Ingress or Gateway resources. Workload initially covers Deployment, StatefulSet and DaemonSet; additional Kubernetes Kinds require an explicit typed projection rather than a generic unbounded object dump.

Structural edges are derived from attributable Kubernetes facts such as owner references, selectors, EndpointSlices or Endpoints, scheduling and backend references. Metrics, Alerts, logs, traces and Agent activity appear as separately identifiable overlays bound to the same resource identity and time range. They may emphasize traffic or causal evidence but cannot fabricate a structural relationship.

The backend provides a bounded native topology API containing stable resource identities, typed nodes and edges, collection time, source provenance, health state and stale or partial-data markers. Empty, unavailable and partially authorized scopes are explicit states. The visual scene and its keyboard-accessible structured view consume the same projection.

## Consequences

- Infrastructure and provider adapters must add the resource types and relationship projection missing from the current Incident-scoped Kubernetes reader.
- Scene layout remains stable for the same resource identities; live refresh must not randomly reposition the graph.
- Namespace and resource limits, pagination or progressive expansion are required before rendering dense clusters.
- Workspace navigation and Context Links carry Operational Scope so a drill-down opens the same cluster, resource and time range.
- Provider or telemetry failure degrades only its overlay and does not erase still-valid Kubernetes topology.

## Rejected Alternatives

- Merge all configured clusters into one global graph.
- Render every Kubernetes object and telemetry event as an equal first-level node.
- Invent relationships to make the scene appear fuller.
- Build a Three.js-only graph without an equivalent structured resource view.
