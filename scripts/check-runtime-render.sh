#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="${ROOT_DIR}/charts/cloudops"
VALUES_FILE="${CHART_DIR}/values-local.yaml"

for command_name in helm yq jq grep; do
  command -v "${command_name}" >/dev/null 2>&1 || {
    printf 'FAIL: missing command: %s\n' "${command_name}" >&2
    exit 1
  }
done

rendered="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime.XXXXXX.yaml")"
objects="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime.XXXXXX.json")"
multi_values="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-multi.XXXXXX.yaml")"
multi_rendered="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-multi.XXXXXX.rendered.yaml")"
multi_objects="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-multi.XXXXXX.json")"
external_values="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-external.XXXXXX.yaml")"
external_rendered="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-external.XXXXXX.rendered.yaml")"
external_objects="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-external.XXXXXX.json")"
scenario_rendered="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-scenario.XXXXXX.yaml")"
scenario_objects="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-scenario.XXXXXX.json")"
invalid_values="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-invalid.XXXXXX.yaml")"
invalid_output="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-invalid.XXXXXX.log")"
trap 'rm -f "${rendered}" "${objects}" "${multi_values}" "${multi_rendered}" "${multi_objects}" "${external_values}" "${external_rendered}" "${external_objects}" "${scenario_rendered}" "${scenario_objects}" "${invalid_values}" "${invalid_output}"' EXIT

helm template cloudops "${CHART_DIR}" \
  --namespace cloudops-system \
  --values "${VALUES_FILE}" >"${rendered}"
yq -o=json 'select(.kind != null)' "${rendered}" | jq -s '.' >"${objects}"

helm template cloudops "${CHART_DIR}" \
  --namespace cloudops-system \
  --values "${VALUES_FILE}" \
  --set scenario.enabled=true \
  --set-string scenario.id=scenario-render-contract \
  --set-string worker.env.K8S_WRITE_ENABLED=true >"${scenario_rendered}"
yq -o=json 'select(.kind != null)' "${scenario_rendered}" | jq -s '.' >"${scenario_objects}"

count() {
  jq -r --arg kind "$1" --arg name "$2" \
    '[.[] | select(.kind == $kind and .metadata.name == $name)] | length' "${objects}"
}

expect_one() {
  local actual
  actual="$(count "$1" "$2")"
  [[ "${actual}" == "1" ]] || {
    printf 'FAIL: expected one %s/%s, found %s\n' "$1" "$2" "${actual}" >&2
    exit 1
  }
}

expect_one Deployment cloudops-api
expect_one Deployment cloudops-worker
expect_one Deployment prometheus
expect_one Deployment alertmanager
expect_one Deployment grafana
expect_one Deployment otel-collector
expect_one StatefulSet mysql
expect_one StatefulSet elasticsearch
expect_one StatefulSet tempo
expect_one DaemonSet filebeat
expect_one PersistentVolumeClaim cloudops-data
expect_one Service cloudops-api
expect_one Service cloudops-api-management
expect_one Service cloudops-worker-management
expect_one Service prometheus
expect_one Service alertmanager
expect_one Service grafana
expect_one Service elasticsearch
expect_one Service tempo
expect_one Service otel-collector
expect_one ConfigMap cloudops-monitoring
expect_one ConfigMap cloudops-alerting
expect_one ConfigMap cloudops-telemetry
expect_one ServiceAccount filebeat
expect_one ClusterRole cloudops-filebeat-metadata-readonly
expect_one ClusterRoleBinding cloudops-filebeat-metadata-readonly

[[ "$(jq -r '[.[] | select((.kind == "Deployment" or .kind == "Service" or .kind == "ServiceAccount") and (.metadata.name | startswith("cloudops-scenario")))] | length' "${objects}")" == "0" ]] || {
  printf 'FAIL: inactive runtime must not render Scenario resources\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and (.metadata.name == "cloudops-scenario-healthy" or .metadata.name == "cloudops-scenario-fault" or .metadata.name == "cloudops-scenario-traffic")) | select(.metadata.namespace == "demo" and .metadata.labels["cloudops.io/scenario-id"] == "scenario-render-contract")] | length' "${scenario_objects}")" == "3" ]] || {
  printf 'FAIL: active runtime must render the exact 3 bounded Scenario Deployments\n' >&2
  exit 1
}
[[ "$(jq -r '[.[] | select(.kind == "Deployment" and (.metadata.name == "cloudops-api" or .metadata.name == "cloudops-worker")) | .spec.template.spec.containers[0].env | select(any(.[]; .name == "SCENARIO_STATE" and .value == "active")) | select(any(.[]; .name == "SCENARIO_ID" and .value == "scenario-render-contract"))] | length' "${scenario_objects}")" == "2" ]] || {
  printf 'FAIL: API and Worker must receive the exact active Scenario identity\n' >&2
  exit 1
}
[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-worker") | .spec.template.spec.containers[0].env | select(any(.[]; .name == "K8S_WRITE_ENABLED" and .value == "true"))] | length' "${scenario_objects}")" == "1" ]] || {
  printf 'FAIL: active Scenario must enable the bounded Worker write gate\n' >&2
  exit 1
}
[[ "$(jq -r '[.[] | select(.kind == "Role" and .metadata.namespace == "demo" and .metadata.name == "cloudops-worker-readonly") | .rules[] | select(.resources == ["deployments/scale"] and .resourceNames == ["cloudops-scenario-fault"] and (.verbs | sort) == ["get","update"])] | length' "${scenario_objects}")" == "1" ]] || {
  printf 'FAIL: active Scenario scale RBAC must bind one exact Deployment subresource\n' >&2
  exit 1
}
[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-monitoring") | .data["prometheus.yml"] | select(contains("cloudops-scenario-fault.demo.svc:8080") and contains("scenario_id: \"scenario-render-contract\""))] | length' "${scenario_objects}")" == "1" ]] || {
  printf 'FAIL: active Scenario Prometheus targets must carry exact identity\n' >&2
  exit 1
}
[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-alerting") | .data["cloudops-alerts.yml"] | select(contains("alert: CloudOpsScenarioRequiredEnvMissing") and contains("scenario_id: \"scenario-render-contract\""))] | length' "${scenario_objects}")" == "1" ]] || {
  printf 'FAIL: active Scenario Alert rule must carry exact identity\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Job" and (.metadata.name | startswith("cloudops-migrate-")))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: expected one release-revision migration Job\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Service" and .metadata.name == "cloudops-api") | .spec.ports[] | select(.name == "http" and .port == 8080 and .targetPort == "http")] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: cloudops-api must expose the V1 UI/API listener on port 8080\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-worker") | .spec.template.spec | select(.automountServiceAccountToken == true) | select(any(.containers[0].env[]; .name == "K8S_ENABLED" and .value == "true"))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Worker typed Kubernetes reader must mount its read-only ServiceAccount token\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-worker") | .spec.template.spec.containers[0].env | select(any(.[]; .name == "K8S_CONNECTIONS_JSON" and .value == ""))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: empty Kubernetes connection registry must preserve the single-reader bootstrap contract\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and (.metadata.name == "prometheus" or .metadata.name == "grafana")) | .spec.template.spec | select(.automountServiceAccountToken == false) | .containers[0] | select(.securityContext.readOnlyRootFilesystem == true and .imagePullPolicy == "Never")] | length' "${objects}")" == "2" ]] || {
  printf 'FAIL: native Monitoring providers must be non-root, tokenless, read-only, and use preloaded images\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "alertmanager") | .spec.template.spec | select(.automountServiceAccountToken == false and .securityContext.runAsNonRoot == true) | select(any(.volumes[]; .name == "webhook-token" and .secret.secretName == "cloudops-alertmanager-webhook")) | .containers[0] | select(.securityContext.readOnlyRootFilesystem == true and .imagePullPolicy == "Never") | select(any(.volumeMounts[]; .name == "webhook-token" and .mountPath == "/var/run/secrets/cloudops/alertmanager" and .readOnly == true))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Alertmanager must use the private webhook credential, a preloaded image, and a hardened tokenless Pod\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-config") | .data.TRACE_OTLP_ENDPOINT | select(. == "otel-collector.cloudops-system.svc:4317")] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: API and Worker must export traces to the in-cluster OTel Collector\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "StatefulSet" and (.metadata.name == "elasticsearch" or .metadata.name == "tempo")) | select(.spec.persistentVolumeClaimRetentionPolicy.whenDeleted == "Retain" and .spec.persistentVolumeClaimRetentionPolicy.whenScaled == "Retain") | .spec.template.spec | select(.automountServiceAccountToken == false) | .containers[0] | select(.securityContext.readOnlyRootFilesystem == true and .imagePullPolicy == "Never")] | length' "${objects}")" == "2" ]] || {
  printf 'FAIL: Elasticsearch and Tempo must use retained bounded storage, tokenless Pods, and preloaded images\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "StatefulSet" and .metadata.name == "elasticsearch") | select(.spec.volumeClaimTemplates[0].spec.resources.requests.storage == "1Gi") | select(any(.spec.template.spec.initContainers[]; .name == "prepare-config" and (.command | join(" ") | contains("file=/tmp/elasticsearch-gc.log")) and .securityContext.readOnlyRootFilesystem == true and any(.volumeMounts[]; .name == "config" and .mountPath == "/writable-config"))) | select(any(.spec.template.spec.containers[0].volumeMounts[]; .name == "config" and .mountPath == "/usr/share/elasticsearch/config")) | .spec.template.spec.containers[0].env | select(any(.[]; .name == "ES_JAVA_OPTS" and .value == "-Xms384m -Xmx384m"))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Elasticsearch must render fixed JVM/disk ceilings and a bounded writable config volume\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-telemetry") | select(.data["elasticsearch-policy.json"] | contains("\"min_age\": \"7d\"")) | select(.data["filebeat.yml"] | contains("/var/log/containers/*_cloudops-system_*.log") and contains("add_kubernetes_metadata") and contains("deployment: true") and contains("trace_id")) | select(.data["tempo.yaml"] | contains("backend_scheduler:") and contains("backend_worker:") and contains("block_retention: 72h"))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: telemetry configuration must preserve log correlation and bounded raw retention\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "DaemonSet" and .metadata.name == "filebeat") | .spec.template.spec | select(.automountServiceAccountToken == true) | .containers[0] | select(.imagePullPolicy == "Never" and .securityContext.allowPrivilegeEscalation == false and .securityContext.readOnlyRootFilesystem == true and .securityContext.runAsUser == 0) | select(any(.volumeMounts[]; .name == "container-logs" and .readOnly == true)) | select(any(.volumeMounts[]; .name == "pod-logs" and .readOnly == true))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Filebeat must use bounded read-only Kubernetes host-log access and its metadata ServiceAccount\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "otel-collector") | .spec.template.spec | select(.automountServiceAccountToken == false) | .containers[0] | select(.imagePullPolicy == "Never" and .securityContext.readOnlyRootFilesystem == true)] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: OTel Collector must be tokenless, read-only, and use a preloaded image\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-monitoring") | .data["prometheus.yml"] | select(contains("cluster_id: \"cloudops-local\"") and contains("environment: \"local\"") and contains("namespace: \"cloudops-system\"") and contains("workload_kind: Deployment") and contains("workload: cloudops-api") and contains("workload: cloudops-worker") and contains("alertmanager.cloudops-system.svc:9093") and contains("/etc/prometheus/rules/cloudops-alerts.yml"))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Prometheus scrape targets must carry the exact bounded Workload labels\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-alerting") | select(.data["alertmanager.yml"] | contains("cloudops-api-management.cloudops-system.svc:8082/webhooks/alertmanager") and contains("send_resolved: true") and contains("credentials_file: /var/run/secrets/cloudops/alertmanager/token")) | select(.data["cloudops-alerts.yml"] | contains("alert: CloudOpsAlertLifecycleValidation") and contains("expr: vector(0) > 0") and contains("cluster: \"cloudops-local\"") and contains("namespace: demo") and contains("deployment: demo"))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Alertmanager webhook and default-off controlled Alert lifecycle rule are not exact\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "prometheus") | .spec.template.spec.containers[0] | select(any(.args[]; . == "--web.enable-lifecycle")) | select(any(.volumeMounts[]; .mountPath == "/etc/prometheus/rules" and .readOnly == true and (has("subPath") | not)))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Prometheus must mount the controlled Alert rule and expose bounded config reload\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-monitoring") | .data["prometheus-datasource.yaml"] | select(contains("uid: Prometheus") and contains("url: http://prometheus.cloudops-system.svc:9090"))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: Grafana must provision the exact in-cluster Prometheus datasource\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "ConfigMap" and .metadata.name == "cloudops-monitoring") | .data["grafana.ini"] | select(contains("org_role = Editor") and contains("[explore]\nenabled = true"))] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: local Grafana must allow its loopback Owner to open exact Explore links\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "ServiceMonitor" or .kind == "PrometheusRule")] | length' "${objects}")" == "0" ]] || {
  printf 'FAIL: local Monitoring stack must not require unavailable Operator CRDs\n' >&2
  exit 1
}

printf '%s\n' \
  'worker:' \
  '  credentials:' \
  '    secretName: cloudops-kubeconfigs' \
  '    files:' \
  '      CLUSTER_B_KUBECONFIG_FILE: cluster-b.yaml' \
  '  env:' \
  '    K8S_CONNECTIONS_JSON: '\''[{"cluster_id":"cloudops-local","in_cluster":true,"allowed_namespaces":["cloudops-system","demo"],"default_namespace":"cloudops-system"},{"cluster_id":"cluster-b","kubeconfig":"/var/run/secrets/cloudops/worker/cluster-b.yaml","context":"cluster-b","in_cluster":false,"allowed_namespaces":["ops"],"default_namespace":"ops","request_timeout_seconds":12}]'\''' \
  >"${multi_values}"
helm template cloudops "${CHART_DIR}" --namespace cloudops-system --values "${VALUES_FILE}" --values "${multi_values}" >"${multi_rendered}"
yq -o=json 'select(.kind != null)' "${multi_rendered}" | jq -s '.' >"${multi_objects}"

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-worker") | .spec.template.spec | select(.automountServiceAccountToken == true) | .containers[0].env[] | select(.name == "K8S_CONNECTIONS_JSON") | (.value | fromjson | length)] | first' "${multi_objects}")" == "2" ]] || {
  printf 'FAIL: mixed Kubernetes connection registry must render two typed readers and retain the in-cluster token\n' >&2
  exit 1
}
[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-worker") | .spec.template.spec.volumes[] | select(.name == "worker-credentials" and .secret.secretName == "cloudops-kubeconfigs")] | length' "${multi_objects}")" == "1" ]] || {
  printf 'FAIL: external Kubernetes readers must use a mounted credential Secret\n' >&2
  exit 1
}

printf '%s\n' \
  'worker:' \
  '  credentials:' \
  '    secretName: cloudops-kubeconfigs' \
  '    files:' \
  '      CLUSTER_B_KUBECONFIG_FILE: cluster-b.yaml' \
  '  env:' \
  '    K8S_CONNECTIONS_JSON: '\''[{"cluster_id":"cluster-b","kubeconfig":"/var/run/secrets/cloudops/worker/cluster-b.yaml","context":"cluster-b","in_cluster":false,"allowed_namespaces":["ops"],"default_namespace":"ops"}]'\''' \
  >"${external_values}"
helm template cloudops "${CHART_DIR}" --namespace cloudops-system --values "${VALUES_FILE}" --values "${external_values}" >"${external_rendered}"
yq -o=json 'select(.kind != null)' "${external_rendered}" | jq -s '.' >"${external_objects}"

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-worker") | .spec.template.spec | select(.automountServiceAccountToken == false)] | length' "${external_objects}")" == "1" ]] || {
  printf 'FAIL: an external-only Kubernetes registry must not mount the Worker ServiceAccount token\n' >&2
  exit 1
}
[[ "$(jq -r '[.[] | select(.kind == "Role" or .kind == "RoleBinding" or .kind == "ClusterRole" or .kind == "ClusterRoleBinding") | select(.metadata.name | startswith("cloudops-worker"))] | length' "${external_objects}")" == "0" ]] || {
  printf 'FAIL: an external-only Kubernetes registry must not render in-cluster read RBAC\n' >&2
  exit 1
}

printf '%s\n' \
  'worker:' \
  '  credentials:' \
  '    secretName: cloudops-kubeconfigs' \
  '    files:' \
  '      CLUSTER_B_KUBECONFIG_FILE: cluster-b.yaml' \
  '  env:' \
  '    K8S_CONNECTIONS_JSON: '\''[{"cluster_id":"cluster-b","kubeconfig":"/not-mounted/config","in_cluster":false,"allowed_namespaces":["ops"],"default_namespace":"ops"}]'\''' \
  >"${invalid_values}"
if helm template cloudops "${CHART_DIR}" --namespace cloudops-system --values "${VALUES_FILE}" --values "${invalid_values}" >"${invalid_output}" 2>&1; then
  printf 'FAIL: unmounted external kubeconfig path unexpectedly rendered\n' >&2
  exit 1
fi
grep -Eq 'kubeconfig must reference a mounted worker credential file' "${invalid_output}" || {
  printf 'FAIL: invalid external kubeconfig did not fail with the bounded credential contract\n' >&2
  exit 1
}

[[ "$(jq -r '[.[] | select(.kind == "Role" and .metadata.name == "cloudops-worker-readonly")] | length' "${objects}")" == "2" ]] || {
  printf 'FAIL: expected bounded read-only Roles for both configured Provider Namespaces\n' >&2
  exit 1
}

expect_one ClusterRole cloudops-worker-topology-readonly
expect_one ClusterRoleBinding cloudops-worker-topology-readonly

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and (.metadata.name == "cloudops-api" or .metadata.name == "cloudops-worker")) | .spec.template.spec | select(.securityContext.runAsUser == 65532 and .securityContext.runAsGroup == 65532 and .securityContext.fsGroup == 65532) | select(any(.volumes[]; .name == "operational-data" and .persistentVolumeClaim.claimName == "cloudops-data")) | select(any(.containers[0].volumeMounts[]; .name == "operational-data" and .mountPath == "/var/lib/cloudops"))] | length' "${objects}")" == "2" ]] || {
  printf 'FAIL: API and Worker must share the private operational-data PVC as uid/gid 65532\n' >&2
  exit 1
}

if grep -Eni 'oauth|csrf|cloudops\.io/profile|values-phase|V[23]_|/api/v[23]' "${rendered}"; then
  printf 'FAIL: rendered runtime contains an obsolete auth or generation contract\n' >&2
  exit 1
fi

if helm template cloudops "${CHART_DIR}" --namespace cloudops-system --values "${VALUES_FILE}" \
  --set scenario.enabled=true --set-string scenario.id=scenario-invalid >"${invalid_output}" 2>&1; then
  printf 'FAIL: active Scenario rendered without the bounded write gate\n' >&2
  exit 1
fi
grep -Eq 'active Demonstration Scenario requires the bounded Kubernetes write gate' "${invalid_output}" || {
  printf 'FAIL: missing Scenario write gate did not fail with the exact contract\n' >&2
  exit 1
}
if helm template cloudops "${CHART_DIR}" --namespace cloudops-system --values "${VALUES_FILE}" \
  --set-string worker.env.K8S_WRITE_ENABLED=true >"${invalid_output}" 2>&1; then
  printf 'FAIL: inactive runtime rendered with Kubernetes writes enabled\n' >&2
  exit 1
fi
grep -Eq 'Kubernetes write access is permitted only while the bounded Scenario is active' "${invalid_output}" || {
  printf 'FAIL: inactive write gate did not fail with the exact contract\n' >&2
  exit 1
}

install_runtime_contract="$(awk '/^install_runtime\(\) \{/,/^}/' "${ROOT_DIR}/scripts/local-lifecycle.sh")"
local_up_contract="$(awk '/^local_up\(\) \{/,/^}/' "${ROOT_DIR}/scripts/local-lifecycle.sh")"
# shellcheck disable=SC2016
for required in \
  'local active_scenario_id="${1:-}"' \
  '--set scenario.enabled=true' \
  '--set-string "scenario.id=${active_scenario_id}"' \
  '--set-string worker.env.K8S_WRITE_ENABLED=true'; do
  grep -Fq -- "${required}" <<<"${install_runtime_contract}" || {
    printf 'FAIL: install_runtime does not preserve active Scenario contract: %s\n' "${required}" >&2
    exit 1
  }
done
# shellcheck disable=SC2016
for required in \
  'release_values="$(release_scenario_json)"' \
  'active_scenario_fault_replicas="$(scenario_fault_replicas)"' \
  'install_runtime "${active_scenario_id}"' \
  'restore_active_scenario_fault_state "${active_scenario_id}" "${active_scenario_fault_replicas}"' \
  'write_scenario_id "${active_scenario_id}"' \
  'pass "active Scenario preserved across local-up: ${active_scenario_id}"'; do
  grep -Fq -- "${required}" <<<"${local_up_contract}" || {
    printf 'FAIL: local_up does not preserve active Scenario contract: %s\n' "${required}" >&2
    exit 1
  }
done

printf 'PASS: semantic CloudOps runtime render contract\n'
