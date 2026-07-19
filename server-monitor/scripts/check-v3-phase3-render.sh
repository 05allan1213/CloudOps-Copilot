#!/usr/bin/env bash
set -Eeuo pipefail

app_manifest="${1:-}"
demo_manifest="${2:-}"
if [[ -z "${app_manifest}" || ! -s "${app_manifest}" || -z "${demo_manifest}" || ! -s "${demo_manifest}" ]]; then
  printf 'usage: %s APP_RENDERED_MANIFEST DEMO_RENDERED_MANIFEST\n' "$0" >&2
  exit 2
fi

for command_name in jq yq; do
  command -v "${command_name}" >/dev/null 2>&1 || {
    printf 'missing command: %s\n' "${command_name}" >&2
    exit 2
  }
done

documents="$(yq -o=json 'select(.kind != null)' "${app_manifest}" "${demo_manifest}" | jq -s '.')"

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
has_resource ServiceMonitor cloudops-api
has_resource PrometheusRule cloudops-api
has_resource Deployment cloudops-demo-workload
has_resource Service cloudops-demo-workload
has_resource Service demo-diagnostics
has_resource PodMonitor cloudops-demo-workload
has_resource PrometheusRule cloudops-demo-workload
has_resource Job cloudops-demo-load-generator

jq -e '
  [.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-api")][0]
  | .spec.template.spec.automountServiceAccountToken == false
  and .spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true
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
  [.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-demo-workload")][0]
  | .metadata.namespace == "cloudops-demo"
  and .spec.replicas == 2
  and .spec.template.spec.automountServiceAccountToken == false
  and .spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true
  and any(.spec.template.spec.containers[0].env[]; .name == "REQUIRED_ENV" and (.value | length > 0))
  and any(.spec.template.spec.containers[0].env[]; .name == "K8S_POD_UID" and .valueFrom.fieldRef.fieldPath == "metadata.uid")
  and any(.spec.template.spec.containers[0].env[]; .name == "TRACE_SAMPLE_RATE" and .value == "1")
  and .spec.template.spec.containers[0].livenessProbe.httpGet.path == "/livez"
  and .spec.template.spec.containers[0].readinessProbe.httpGet.path == "/readyz"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Service" and .metadata.namespace == "cloudops-demo") | .metadata.name] | sort
  == ["cloudops-demo-workload", "demo-diagnostics"]
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Service" and .metadata.name == "cloudops-demo-workload")][0]
  | .spec.type == "ClusterIP"
  and ((.spec.publishNotReadyAddresses // false) == false)
  and (.spec.ports | length == 1)
  and .spec.ports[0].name == "http"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Service" and .metadata.name == "demo-diagnostics")][0]
  | .spec.type == "ClusterIP"
  and .spec.publishNotReadyAddresses == true
  and (.spec.ports | length == 1)
  and .spec.ports[0].name == "http"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "PodMonitor" and .metadata.name == "cloudops-demo-workload")][0]
  | .metadata.namespace == "cloudops-demo"
  and .metadata.labels["cloudops.io/monitoring"] == "enabled"
  and .spec.podMetricsEndpoints[0].path == "/metrics"
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "PrometheusRule" and .metadata.name == "cloudops-demo-workload")][0]
  | .metadata.namespace == "cloudops-demo"
  and .metadata.labels["cloudops.io/monitoring"] == "enabled"
  and any(.spec.groups[].rules[]; .alert == "CloudOpsDemoRequiredEnvMissing" and .labels.signal_target == "cloudops-demo")
  and any(.spec.groups[].rules[]; .alert == "CloudOpsDemoErrorRateHigh" and .labels.signal_target == "cloudops-demo")
' <<<"${documents}" >/dev/null

jq -e '
  [.[] | select(.kind == "Job" and .metadata.name == "cloudops-demo-load-generator")][0]
  | .metadata.namespace == "cloudops-demo"
  and .metadata.annotations["helm.sh/hook"] == "test"
  and .metadata.annotations["cloudops.io/load-rate"] == "5-rps"
  and .metadata.annotations["cloudops.io/request-timeout"] == "2s"
  and .spec.activeDeadlineSeconds == 1800
  and .spec.template.spec.automountServiceAccountToken == false
  and (.spec.template.spec.containers[0].args[0] | contains("http://demo-diagnostics:8080/") and contains("sleep 0.2"))
' <<<"${documents}" >/dev/null

if jq -e 'any(.[]; .metadata.namespace == "cloudops-demo" and (.kind == "Ingress" or .kind == "ServiceAccount" or .kind == "Role" or .kind == "RoleBinding"))' <<<"${documents}" >/dev/null; then
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

printf 'PASS: V3 phase3 profile rendered the API and two-replica Demo with only fixed ClusterIP Services, monitored alerts and a Golden-only load-generator hook.\n'
