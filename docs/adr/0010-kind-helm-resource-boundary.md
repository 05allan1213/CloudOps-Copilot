# ADR 0010: kind + Helm Only and Resource Boundary

- Status: Accepted target decision; implementation NOT RUN
- Date: 2026-07-18
- Owner: Phase 3 and Phase 7
- Refined by: ADR 0040

## Context

The repository currently has Compose, raw Kubernetes and a monolithic Helm chart with duplicate ownership. That makes configuration drift and shortcut demos likely.

## Decision

The only complete deployment path is a local kind cluster bootstrapped by Make targets and Helm.

Platform bootstrap owns kind, namespaces/storage, ECK, kube-prometheus-stack, Tempo/OTel, Argo and MySQL. charts/cloudops owns API, Worker, Migrate, oauth2-proxy, Services, minimal RBAC, ServiceMonitor/PrometheusRule and references to pre-created Secrets. Argo owns only the Golden Demo manifests. The Golden harness owns the temporary load generator.

Use ClusterIP and kubectl port-forward, not Ingress/NodePort. All third-party Chart versions, packages and images are pinned and verified. The target budget is about 5 GiB requests and 10 GiB limits, but exact minima and peak remain NOT RUN until a clean install.

No HA, production SLO, backup/DR or NetworkPolicy isolation is claimed.

## Consequences

- Delete Compose, raw manifests, deploy-k8s and fast-demo paths only after replacement proof.
- Preflight reports existing cluster/port/resource conflicts and never auto-deletes user state.
- MySQL is a single-replica local Demo dependency outside the CloudOps product chart.

## Rejected Alternatives

- Keep Compose as a fast parallel architecture.
- Let CI deploy the application directly with Helm/kubeconfig.
- Add Calico/Cilium, Ingress, HPA or production data topology to the first release.

## Evidence Required

Empty-cluster repeatability, rendered ownership/RBAC/Secret checks, pinned-version data paths, measured resource peak and final cleanup state.
