#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="${ROOT_DIR}/charts/cloudops"
VALUES_FILE="${CHART_DIR}/values-local.yaml"

for command_name in helm yq jq rg; do
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
invalid_values="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-invalid.XXXXXX.yaml")"
invalid_output="$(mktemp "${TMPDIR:-/tmp}/cloudops-runtime-invalid.XXXXXX.log")"
trap 'rm -f "${rendered}" "${objects}" "${multi_values}" "${multi_rendered}" "${multi_objects}" "${external_values}" "${external_rendered}" "${external_objects}" "${invalid_values}" "${invalid_output}"' EXIT

helm template cloudops "${CHART_DIR}" \
  --namespace cloudops-system \
  --values "${VALUES_FILE}" >"${rendered}"
yq -o=json 'select(.kind != null)' "${rendered}" | jq -s '.' >"${objects}"

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
expect_one StatefulSet mysql
expect_one PersistentVolumeClaim cloudops-data
expect_one Service cloudops-api
expect_one Service cloudops-api-management
expect_one Service cloudops-worker-management

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
rg -q 'kubeconfig must reference a mounted worker credential file' "${invalid_output}" || {
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

if rg -n -i 'oauth|csrf|cloudops\.io/profile|values-phase|V[23]_|/api/v[23]' "${rendered}"; then
  printf 'FAIL: rendered runtime contains an obsolete auth or generation contract\n' >&2
  exit 1
fi

printf 'PASS: semantic CloudOps runtime render contract\n'
