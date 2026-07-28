# ADR 0004: Prometheus, ECK/Filebeat and OTel/Tempo

- Status: Accepted component decision; exact version matrix NOT RUN
- Date: 2026-07-18
- Owner: Phase 3

## Context

The current repository deploys hand-written Prometheus/Alertmanager/Grafana, Fluent Bit, hand-written Elasticsearch/Kibana, Jaeger and VictoriaMetrics through parallel Compose/raw/Helm paths. V3 needs four real bounded Evidence paths without owning generic telemetry infrastructure.

## Decision

- Metrics/alerts: Prometheus Operator, Prometheus, Alertmanager and Grafana through kube-prometheus-stack resources.
- Logs: ECK-managed Elasticsearch/Kibana and Filebeat filestream through a Beat CR.
- Traces: application OTLP, an OTel Collector traces-only pipeline and Tempo 3 monolithic.
- Resources/events: typed client-go reads.

Filebeat discovers only CloudOps and Demo namespaces. OTel k8sattributes has namespace-scoped pod/ReplicaSet read access. Logs are read only from Elasticsearch; there is no Pod-log fallback.

Delete VictoriaMetrics, Jaeger, Loki, Fluent Bit, Logstash, Fleet/Elastic Agent, hand-written Elastic workloads and duplicate OTel log/metric collection.

Phase 0 freezes component families and boundaries only. Phase 3 must choose and prove one compatible Chart/image matrix with package checksums and image digests.

## Consequences

- Runtime observations never replace Kubernetes/Registry/GitHub/Argo deployment authority.
- No-data needs healthy, complete and in-retention queries.
- Prometheus/Tempo read-only is a code/config boundary, not per-client IAM.
- Beat v1beta1 and local unauthenticated OTLP remain explicitly tested/documented risks.

## Rejected Alternatives

- Retain the current display stack.
- Add Kafka/Logstash between Filebeat and Elasticsearch.
- Deploy Tempo distributed or use Loki for logs.

## Evidence Required

Clean kind install, real four-path query, same-request correlation, Filebeat namespace canary, OTel spoof negative, no-Kafka Tempo config and measured resource peak.
