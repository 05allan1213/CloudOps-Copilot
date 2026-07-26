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
trap 'rm -f "${rendered}" "${objects}"' EXIT

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

[[ "$(jq -r '[.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-worker") | .spec.template.spec | select(.automountServiceAccountToken == false)] | length' "${objects}")" == "1" ]] || {
  printf 'FAIL: standby Worker must not mount a Kubernetes token\n' >&2
  exit 1
}

if rg -n -i 'oauth|csrf|cloudops\.io/profile|values-phase|V[23]_|/api/v[23]' "${rendered}"; then
  printf 'FAIL: rendered runtime contains an obsolete auth or generation contract\n' >&2
  exit 1
fi

printf 'PASS: semantic CloudOps runtime render contract\n'
