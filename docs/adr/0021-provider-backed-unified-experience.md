# ADR 0021: Provider-Backed Unified Experience

- Status: Accepted architecture decision; implementation NOT RUN
- Date: 2026-07-26

## Context

The platform already uses specialized cloud-native engines for metrics, alert routing, logs, traces and delivery. Rebuilding those engines would add substantial scope, while requiring users to open and authenticate to several external consoles would preserve the fragmented experience the redesign is intended to remove.

## Decision

CloudOps owns the unified experience and operation control plane. Prometheus and Alertmanager, ECK and Elasticsearch, OpenTelemetry and Tempo, Grafana, GitHub, Argo CD, and Kubernetes remain the engines or authorities for their respective facts and effects.

The CloudOps frontend must provide native, authenticated views and operation entry points for first-class product capabilities. It consumes CloudOps-owned backend contracts that adapt and bound provider access; it does not hold provider credentials, issue arbitrary provider queries, or treat an iframe or external link as the primary capability.

One Local Owner Context covers all native CloudOps views. ADR 0024 defines the local-only trust boundary; provider-console identities remain separate.

## Consequences

- Provider-backed frontend surfaces require explicit CloudOps query or command APIs, response bounds, authorization, availability states and audit behavior.
- Existing provider clients can be reused behind those contracts, but Agent-only fixed observations are not automatically sufficient as user-facing APIs.
- CloudOps stores its domain state, view definitions, permissions and audit records without duplicating provider telemetry stores or reconciliation engines.
- Grafana, Kibana, GitHub and Argo CD may remain optional expert deep links, but daily workflows must be completable without opening them.
- Provider failures must be visible as bounded partial states; the frontend cannot substitute stale or invented data.

## Rejected Alternatives

- Rebuild Prometheus, Elasticsearch, Tempo or GitOps responsibilities inside CloudOps.
- Expose a collection of external links or embedded provider consoles as the main product experience.
- Give the browser provider credentials or unrestricted provider query access.
