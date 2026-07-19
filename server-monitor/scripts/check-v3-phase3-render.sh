#!/usr/bin/env bash
set -Eeuo pipefail

rendered_manifest="${1:-}"
if [[ -z "${rendered_manifest}" || ! -s "${rendered_manifest}" ]]; then
  printf 'usage: %s RENDERED_MANIFEST\n' "$0" >&2
  exit 2
fi

for command_name in jq yq; do
  command -v "${command_name}" >/dev/null 2>&1 || {
    printf 'missing command: %s\n' "${command_name}" >&2
    exit 2
  }
done

documents="$(yq -o=json 'select(.kind != null)' "${rendered_manifest}" | jq -s '.')"

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

printf 'PASS: V3 phase3 profile rendered API, migration, internal metrics Service, ServiceMonitor and PrometheusRule with legacy resources disabled.\n'
