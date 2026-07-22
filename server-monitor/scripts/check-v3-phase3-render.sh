#!/usr/bin/env bash
set -Eeuo pipefail

platform_manifest="${1:-}"
app_manifest="${2:-}"
demo_manifest="${3:-}"
if [[ -z "${platform_manifest}" || ! -s "${platform_manifest}" || -z "${app_manifest}" || ! -s "${app_manifest}" || -z "${demo_manifest}" || ! -s "${demo_manifest}" ]]; then
  printf 'usage: %s PLATFORM_RENDERED_MANIFEST APP_RENDERED_MANIFEST DEMO_RENDERED_MANIFEST\n' "$0" >&2
  exit 2
fi

for command_name in jq yq; do
  command -v "${command_name}" >/dev/null 2>&1 || {
    printf 'missing command: %s\n' "${command_name}" >&2
    exit 2
  }
done

platform_documents="$(yq -o=json 'select(.kind != null)' "${platform_manifest}" | jq -s '.')"
application_documents="$(yq -o=json 'select(.kind != null)' "${app_manifest}" "${demo_manifest}" | jq -s '.')"
documents="$(yq -o=json 'select(.kind != null)' "${platform_manifest}" "${app_manifest}" "${demo_manifest}" | jq -s '.')"

has_resource() {
  local kind="$1"
  local name="$2"
  jq -e --arg kind "${kind}" --arg name "${name}" \
    '[.[] | select(.kind == $kind and .metadata.name == $name)] | length == 1' \
    <<<"${documents}" >/dev/null
}

has_resource Deployment cloudops-api
has_resource Service cloudops-api-internal
has_resource Job cloudops-migrate
has_resource StatefulSet mysql
has_resource ConfigMap cloudops-mysql-bootstrap
has_resource ServiceMonitor cloudops-api
has_resource PrometheusRule cloudops-api
has_resource Deployment demo
has_resource Service demo
has_resource Service demo-diagnostics
has_resource PodMonitor demo
has_resource PrometheusRule demo
has_resource Job cloudops-demo-load-generator

for platform_resource in \
  "Elasticsearch cloudops-elasticsearch" \
  "Kibana cloudops-kibana" \
  "Beat cloudops-filebeat" \
  "Deployment cloudops-otel-collector" \
  "Service cloudops-otel-collector" \
  "ServiceMonitor cloudops-otel-collector" \
  "StatefulSet cloudops-tempo" \
  "Service cloudops-tempo" \
  "ServiceMonitor cloudops-tempo"; do
  read -r platform_kind platform_name <<<"${platform_resource}"
  jq -e --arg kind "${platform_kind}" --arg name "${platform_name}" \
    '[.[] | select(.kind == $kind and .metadata.name == $name)] | length == 1' \
    <<<"${platform_documents}" >/dev/null || {
    printf 'missing observability platform resource: %s %s\n' "${platform_kind}" "${platform_name}" >&2
    exit 1
  }
done

jq -e '
  [.[] | select(.kind == "StatefulSet" and .metadata.name == "mysql")][0] as $mysql
  | $mysql.spec.template.spec.containers[0] as $container
  | ($container.env | map(.name) | sort) == [
      "CLOUDOPS_API_PASSWORD", "CLOUDOPS_API_USER",
      "CLOUDOPS_BASELINE_PASSWORD", "CLOUDOPS_BASELINE_USER",
      "CLOUDOPS_MIGRATE_PASSWORD", "CLOUDOPS_MIGRATE_USER",
      "CLOUDOPS_WORKER_PASSWORD", "CLOUDOPS_WORKER_USER",
      "MYSQL_DATABASE", "MYSQL_ROOT_PASSWORD"
    ]
  and ([
    $container.env[]
    | select(.name == "MYSQL_ROOT_PASSWORD" or (.name | endswith("_PASSWORD")))
    | .valueFrom.secretKeyRef.name
  ] | sort) == [
    "cloudops-api-database", "cloudops-baseline-database",
    "cloudops-migrate-database", "cloudops-mysql-root", "cloudops-worker-database"
  ]
  and all($container.env[] | select(.name == "MYSQL_ROOT_PASSWORD" or (.name | endswith("_PASSWORD")));
    .value == null and .valueFrom.secretKeyRef.key != null)
  and ([
    $container.env[]
    | select(.name == "CLOUDOPS_API_USER" or .name == "CLOUDOPS_WORKER_USER" or
             .name == "CLOUDOPS_MIGRATE_USER" or .name == "CLOUDOPS_BASELINE_USER")
    | .value
  ] | sort) == ["cloudops-api", "cloudops-baseline", "cloudops-migrate", "cloudops-worker"]
  and any($container.volumeMounts[];
    .name == "bootstrap-identities" and
    .mountPath == "/docker-entrypoint-initdb.d/10-cloudops-identities.sh" and
    .readOnly == true)
  and any($mysql.spec.template.spec.volumes[];
    .name == "bootstrap-identities" and .configMap.name == "cloudops-mysql-bootstrap")
' <<<"${platform_documents}" >/dev/null || {
  printf 'MySQL must consume four distinct workload/verifier identities plus a separate root Secret\n' >&2
  exit 1
}

jq -e '
  [.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-mysql-bootstrap")][0]
  | .data["10-cloudops-identities.sh"] as $script
  | ($script | contains("GRANT SELECT, INSERT, UPDATE, DELETE ON `cloudops`.* TO ${api_user}@'\''%'\'';"))
  and ($script | contains("GRANT SELECT, INSERT, UPDATE, DELETE ON `cloudops`.* TO ${worker_user}@'\''%'\'';"))
  and ($script | contains("GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, INDEX, REFERENCES ON `cloudops`.* TO ${migrate_user}@'\''%'\'';"))
  and ($script | contains("GRANT SELECT ON `cloudops`.* TO ${baseline_user}@'\''%'\'';"))
  and ($script | contains("GRANT INSERT, UPDATE ON `cloudops`.`deployment_baselines` TO ${baseline_user}@'\''%'\'';"))
  and ($script | contains("GRANT INSERT, UPDATE ON `cloudops`.`baseline_observations` TO ${baseline_user}@'\''%'\'';"))
  and (($script | test("GRANT ALL PRIVILEGES ON|WITH GRANT OPTION")) | not)
' <<<"${platform_documents}" >/dev/null || {
  printf 'MySQL bootstrap privileges are broader than the API/Worker/Migrate/baseline contract\n' >&2
  exit 1
}

jq -e '
  ([.[] | .. | objects | .image? | select(type == "string")]) as $images
  | ($images | length) >= 6
  and all($images[]; test("@sha256:[a-f0-9]{64}$"))
' <<<"${platform_documents}" >/dev/null || {
  printf 'observability platform images must all be immutable digest references\n' >&2
  exit 1
}

jq -e '
  ([.[] | select(.kind == "ClusterRole" or .kind == "ClusterRoleBinding")] | length) == 0
  and ([.[] | select(.kind == "Role" and .metadata.name == "cloudops-filebeat-discovery")] | length) == 2
  and ([.[] | select(.kind == "Role" and .metadata.name == "cloudops-otel-k8sattributes")] | length) == 2
  and all([.[] | select(.kind == "Role" and .metadata.name == "cloudops-filebeat-discovery")][];
    (.rules | length == 1) and .rules[0].resources == ["pods"] and (.rules[0].verbs | sort) == ["get", "list", "watch"]
  )
  and all([.[] | select(.kind == "Role" and .metadata.name == "cloudops-otel-k8sattributes")][];
    (.rules | length == 2) and
    ([.rules[].resources[]] | sort) == ["pods", "replicasets"] and
    all(.rules[]; (.verbs | sort) == ["get", "list", "watch"])
  )
' <<<"${platform_documents}" >/dev/null || {
  printf 'observability RBAC is broader than the namespace-bounded read contract\n' >&2
  exit 1
}

jq -e '
  [.[] | select(.kind == "Role" and (.metadata.name == "cloudops-filebeat-discovery" or .metadata.name == "cloudops-otel-k8sattributes")) | .metadata.namespace] | sort | unique == ["cloudops-system", "demo"]
' <<<"${platform_documents}" >/dev/null || {
  printf 'observability Roles must exist only in the CloudOps and Demo namespaces\n' >&2
  exit 1
}

jq -e '
  [.[] | select(.kind == "Beat" and .metadata.name == "cloudops-filebeat")][0] as $beat
  | $beat.spec.type == "filebeat"
  and ($beat.spec.config["filebeat.autodiscover"].providers | length == 2)
  and (($beat.spec.config["filebeat.autodiscover"].providers | map(.scope) | unique) == ["node"])
  and (($beat.spec.config["filebeat.autodiscover"].providers | map(.namespace) | sort) == ["cloudops-system", "demo"])
  and all($beat.spec.config["filebeat.autodiscover"].providers[];
    (.["hints.enabled"] == false) and
    all(.templates[].config[];
      .type == "filestream" and
      (.id | contains("${data.kubernetes.pod.uid}")) and
      (.id | contains("${data.kubernetes.container.id}")) and
      .["prospector.scanner"].symlinks == true and
      .["prospector.scanner"]["fingerprint.enabled"] == true and
      (.["file_identity.fingerprint"] | type == "object") and
      (.parsers | map(keys) | flatten | index("container") != null)
    )
  )
  and ($beat.spec.config["output.elasticsearch"] | has("index"))
  and ($beat.spec.config["queue.mem"].events > 0)
' <<<"${platform_documents}" >/dev/null || {
  printf 'Filebeat did not render the two bounded filestream providers\n' >&2
  exit 1
}

collector_config="$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-otel-collector")][0].data["collector.yaml"]' <<<"${platform_documents}")"
tempo_config="$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-tempo")][0].data["tempo.yaml"]' <<<"${platform_documents}")"
collector_json="$(yq -o=json '.' <<<"${collector_config}")"
tempo_json="$(yq -o=json '.' <<<"${tempo_config}")"
jq -e '
  (.receivers | keys) == ["otlp"] and
  (.service.pipelines | keys) == ["traces"] and
  .service.pipelines.traces.receivers == ["otlp"] and
  (.service.pipelines.traces.processors | index("k8sattributes/cloudops") != null) and
  (.service.pipelines.traces.processors | index("k8sattributes/demo") != null) and
  (.service.pipelines.traces.processors | index("transform/sanitize_k8s_identity") != null) and
  (.service.pipelines.traces.processors | index("filter/allowed_namespaces") != null) and
  .processors."k8sattributes/cloudops".filter.namespace == "cloudops-system" and
  .processors."k8sattributes/demo".filter.namespace == "demo" and
  (.processors."transform/sanitize_k8s_identity".trace_statements[0].statements | index("delete_key(attributes, \"k8s.namespace.name\")") != null) and
  (.processors."transform/sanitize_k8s_identity".trace_statements[0].statements | index("delete_key(attributes, \"k8s.workload.name\")") != null) and
  (.processors."filter/allowed_namespaces".trace_conditions | length == 1) and
  (.processors."k8sattributes/cloudops".extract.metadata | index("container.image.repo_digests") != null) and
  (.processors."k8sattributes/demo".extract.metadata | index("container.image.repo_digests") != null)
' <<<"${collector_json}" >/dev/null || {
  printf 'OTel Collector must be traces-only with both namespace-filtered k8sattributes processors\n' >&2
  exit 1
}
if grep -Eiq 'filelog|kubeletstats' <<<"${collector_config}"; then
  printf 'OTel Collector rendered a duplicate log/metric receiver\n' >&2
  exit 1
fi
jq -e '
  .target == "all" and
  .storage.trace.backend == "local" and
  (.ingest == null) and
  .usage_report.reporting_enabled == false
' <<<"${tempo_json}" >/dev/null || {
  printf 'Tempo must render the target=all local monolith without Kafka ingest\n' >&2
  exit 1
}
if grep -Eiq 'kafka|tempo-distributed' <<<"${tempo_config}"; then
  printf 'Tempo rendered a forbidden Kafka or distributed dependency\n' >&2
  exit 1
fi

jq -e '
  [.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-api")][0]
  | .spec.template.spec.automountServiceAccountToken == false
  and .spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-config")][0]
  | .data.TRACE_OTLP_ENDPOINT == "cloudops-otel-collector.cloudops-system.svc.cluster.local:4317"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Service" and .metadata.name == "cloudops-api-internal")][0]
  | .spec.type == "ClusterIP"
  and (.spec.ports | any(.[]; .name == "internal" and .port == 8082))
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "ServiceMonitor" and .metadata.name == "cloudops-api")][0]
  | .metadata.labels["cloudops.io/monitoring"] == "enabled"
  and .spec.endpoints[0].path == "/metrics"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "PrometheusRule" and .metadata.name == "cloudops-api")][0]
  | any(.spec.groups[].rules[]; .alert == "CloudOpsAPIAvailability")
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Deployment" and .metadata.name == "demo")][0]
  | .metadata.namespace == "demo"
  and .spec.replicas == 2
  and .spec.selector == {"matchLabels":{"app.kubernetes.io/name":"demo"}}
  and .spec.template.spec.automountServiceAccountToken == false
  and .spec.template.spec.securityContext.runAsNonRoot == true
  and .spec.template.spec.securityContext.seccompProfile == {"type":"RuntimeDefault"}
  and .spec.template.spec.containers[0].name == "demo"
  and .spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true
  and .spec.template.spec.containers[0].securityContext.runAsNonRoot == true
  and .spec.template.spec.containers[0].securityContext.seccompProfile == {"type":"RuntimeDefault"}
  and .spec.template.spec.containers[0].securityContext.capabilities == {"drop":["ALL"]}
  and any(.spec.template.spec.containers[0].env[]; .name == "REQUIRED_ENV" and (.value | length > 0))
  and any(.spec.template.spec.containers[0].env[]; .name == "K8S_NAMESPACE" and .valueFrom.fieldRef == {"apiVersion":"v1","fieldPath":"metadata.namespace"})
  and any(.spec.template.spec.containers[0].env[]; .name == "K8S_POD_NAME" and .valueFrom.fieldRef == {"apiVersion":"v1","fieldPath":"metadata.name"})
  and any(.spec.template.spec.containers[0].env[]; .name == "K8S_POD_UID" and .valueFrom.fieldRef == {"apiVersion":"v1","fieldPath":"metadata.uid"})
  and any(.spec.template.spec.containers[0].env[]; .name == "TRACE_OTLP_ENDPOINT" and .value == "cloudops-otel-collector.cloudops-system.svc.cluster.local:4317")
  and any(.spec.template.spec.containers[0].env[]; .name == "TRACE_SAMPLE_RATE" and .value == "1")
  and .spec.template.spec.containers[0].livenessProbe.httpGet.path == "/livez"
  and .spec.template.spec.containers[0].readinessProbe.httpGet.path == "/readyz"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Service" and .metadata.namespace == "demo") | .metadata.name] | sort
  == ["demo", "demo-diagnostics"]
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Service" and .metadata.name == "demo")][0]
  | .spec.type == "ClusterIP"
  and ((.spec.publishNotReadyAddresses // false) == false)
  and .spec.selector == {"app.kubernetes.io/name":"demo"}
  and (.spec.ports | length == 1)
  and .spec.ports[0].name == "http"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Service" and .metadata.name == "demo-diagnostics")][0]
  | .spec.type == "ClusterIP"
  and .spec.publishNotReadyAddresses == true
  and .spec.selector == {"app.kubernetes.io/name":"demo"}
  and (.spec.ports | length == 1)
  and .spec.ports[0].name == "http"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "PodMonitor" and .metadata.name == "demo")][0]
  | .metadata.namespace == "demo"
  and .metadata.labels["cloudops.io/monitoring"] == "enabled"
  and .spec.selector == {"matchLabels":{"app.kubernetes.io/name":"demo"}}
  and .spec.namespaceSelector == {"matchNames":["demo"]}
  and .spec.podMetricsEndpoints[0].path == "/metrics"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "PrometheusRule" and .metadata.name == "demo")][0]
  | .metadata.namespace == "demo"
  and .metadata.labels["cloudops.io/monitoring"] == "enabled"
  and any(.spec.groups[].rules[]; .alert == "CloudOpsDemoRequiredEnvMissing" and .labels.signal_target == "demo" and .labels.namespace == "demo" and .labels.service == "demo" and .labels.deployment == "demo")
  and any(.spec.groups[].rules[]; .alert == "CloudOpsDemoErrorRateHigh" and .labels.signal_target == "demo" and .labels.namespace == "demo" and .labels.service == "demo" and .labels.deployment == "demo")
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Job" and .metadata.name == "cloudops-demo-load-generator")][0]
  | .metadata.namespace == "demo"
  and .metadata.annotations["helm.sh/hook"] == "test"
  and .metadata.annotations["cloudops.io/load-rate"] == "5-rps"
  and .metadata.annotations["cloudops.io/request-timeout"] == "2s"
  and .spec.activeDeadlineSeconds == 1800
  and .spec.template.spec.automountServiceAccountToken == false
  and (.spec.template.spec.containers[0].args[0] | contains("http://demo-diagnostics:8080/") and contains("sleep 0.2"))
' <<<"${documents}" >/dev/null

if jq -e 'any(.[]; .metadata.namespace == "demo" and (.kind == "Ingress" or .kind == "ServiceAccount" or .kind == "Role" or .kind == "RoleBinding"))' <<<"${application_documents}" >/dev/null; then
  printf 'demo profile rendered a forbidden ingress or RBAC resource\n' >&2
  exit 1
fi

for forbidden_name in server-web prometheus victoriametrics jaeger kafka redis elasticsearch kibana fluent-bit grafana; do
  if jq -e --arg name "${forbidden_name}" 'any(.[]; .metadata.name == $name)' <<<"${documents}" >/dev/null; then
    printf 'forbidden legacy resource rendered: %s\n' "${forbidden_name}" >&2
    exit 1
  fi
done

if jq -e 'any(.[]; .kind == "Secret")' <<<"${documents}" >/dev/null; then
  printf 'v3 profile must reference pre-existing Secrets and render none\n' >&2
  exit 1
fi

printf 'PASS: V3 phase3 rendered pinned ECK/Filebeat and traces-only OTel/Tempo plus the API and two-replica Demo with fixed ClusterIP Services and Golden-only load.\n'
