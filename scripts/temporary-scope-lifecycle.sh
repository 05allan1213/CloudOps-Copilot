#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_ID="${CLOUDOPS_REAL_INTEGRATION_RUN_ID:?CLOUDOPS_REAL_INTEGRATION_RUN_ID is required}"
STATE_ROOT="${CLOUDOPS_STATE_DIR:-${ROOT_DIR}/.cloudops}/integration/${RUN_ID}/scope-reader"
ARTIFACT_PATH="${STATE_ROOT}.json"
MAIN_CONTEXT="kind-cloudops-local"
MAIN_NAMESPACE="cloudops-system"
RELEASE_NAME="cloudops"
SECRET_NAME="cloudops-kubeconfigs"
KUBECONFIG_FILENAME="secondary.yaml"
KUBECONFIG_PATH="${STATE_ROOT}/${KUBECONFIG_FILENAME}"
KIND_NODE_IMAGE="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"

if [[ ! "${RUN_ID}" =~ ^[A-Za-z0-9._-]+$ ]]; then
  printf 'FAIL: invalid integration run identity\n' >&2
  exit 1
fi

suffix="$(printf '%s' "${RUN_ID}" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9' | tail -c 24)"
[[ -n "${suffix}" ]] || { printf 'FAIL: integration run identity has no usable suffix\n' >&2; exit 1; }
SECONDARY_CLUSTER="scope-reader-${suffix}"
SECONDARY_CONTEXT="kind-${SECONDARY_CLUSTER}"
SECONDARY_CLUSTER_ID="${SECONDARY_CLUSTER}"

main_kubectl() {
  kubectl --context "${MAIN_CONTEXT}" "$@"
}

require_tools() {
  local command_name
  for command_name in kind kubectl helm jq rg; do
    command -v "${command_name}" >/dev/null 2>&1 || {
      printf 'FAIL: missing command %s\n' "${command_name}" >&2
      exit 1
    }
  done
}

write_artifact() {
  local temporary
  mkdir -p "${STATE_ROOT}"
  chmod 700 "${STATE_ROOT}"
  temporary="$(mktemp "${STATE_ROOT}/.scope-reader.XXXXXX")"
  jq -n \
    --arg run_id "${RUN_ID}" \
    --arg cluster_name "${SECONDARY_CLUSTER}" \
    --arg cluster_id "${SECONDARY_CLUSTER_ID}" \
    --arg context "${SECONDARY_CONTEXT}" \
    --arg kubeconfig "${KUBECONFIG_PATH}" \
    '{run_id:$run_id, cluster_name:$cluster_name, cluster_id:$cluster_id, context:$context, kubeconfig:$kubeconfig}' \
    >"${temporary}"
  chmod 600 "${temporary}"
  mv "${temporary}" "${ARTIFACT_PATH}"
  chmod 600 "${ARTIFACT_PATH}"
}

reader_connections() {
  jq -cn \
    --arg external_id "${SECONDARY_CLUSTER_ID}" \
    --arg context "${SECONDARY_CONTEXT}" \
    --arg kubeconfig "/var/run/secrets/cloudops/worker/${KUBECONFIG_FILENAME}" \
    '[
      {cluster_id:"cloudops-local",in_cluster:true,allowed_namespaces:["cloudops-system","demo"],default_namespace:"cloudops-system"},
      {cluster_id:$external_id,kubeconfig:$kubeconfig,context:$context,in_cluster:false,allowed_namespaces:["default"],default_namespace:"default",request_timeout_seconds:12}
    ]'
}

wait_worker() {
  main_kubectl -n "${MAIN_NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m
}

up() {
  local temporary connections
  require_tools
  kubectl --context "${MAIN_CONTEXT}" cluster-info >/dev/null
  mkdir -p "${STATE_ROOT}"
  chmod 700 "${STATE_ROOT}"
  if ! kind get clusters 2>/dev/null | rg -Fxq "${SECONDARY_CLUSTER}"; then
    kind create cluster --name "${SECONDARY_CLUSTER}" --image "${KIND_NODE_IMAGE}" --wait 120s
  fi
  temporary="$(mktemp "${STATE_ROOT}/.kubeconfig.XXXXXX")"
  kind get kubeconfig --internal --name "${SECONDARY_CLUSTER}" >"${temporary}"
  chmod 600 "${temporary}"
  mv "${temporary}" "${KUBECONFIG_PATH}"
  kubectl --kubeconfig "${KUBECONFIG_PATH}" --context "${SECONDARY_CONTEXT}" get namespace default >/dev/null
  main_kubectl -n "${MAIN_NAMESPACE}" create secret generic "${SECRET_NAME}" \
    --from-file="${KUBECONFIG_FILENAME}=${KUBECONFIG_PATH}" \
    --dry-run=client -o yaml | main_kubectl apply -f - >/dev/null
  main_kubectl -n "${MAIN_NAMESPACE}" label secret "${SECRET_NAME}" \
    cloudops.io/managed-by=cloudops-real-integration cloudops.io/run-id="${RUN_ID}" --overwrite >/dev/null
  connections="$(reader_connections)"
  helm --kube-context "${MAIN_CONTEXT}" -n "${MAIN_NAMESPACE}" upgrade "${RELEASE_NAME}" "${ROOT_DIR}/charts/cloudops" \
    --reuse-values \
    --set-string "worker.credentials.secretName=${SECRET_NAME}" \
    --set-string "worker.credentials.files.SECONDARY_KUBECONFIG_FILE=${KUBECONFIG_FILENAME}" \
    --set-string "worker.env.K8S_CONNECTIONS_JSON=${connections}" \
    --wait --timeout 5m >/dev/null
  wait_worker
  write_artifact
  printf 'PASS secondary Scope reader ready cluster_id=%s kubeconfig=%s\n' "${SECONDARY_CLUSTER_ID}" "${KUBECONFIG_PATH}"
}

down() {
  require_tools
  if kubectl --context "${MAIN_CONTEXT}" cluster-info >/dev/null 2>&1 && \
     helm --kube-context "${MAIN_CONTEXT}" -n "${MAIN_NAMESPACE}" status "${RELEASE_NAME}" >/dev/null 2>&1; then
    helm --kube-context "${MAIN_CONTEXT}" -n "${MAIN_NAMESPACE}" upgrade "${RELEASE_NAME}" "${ROOT_DIR}/charts/cloudops" \
      --reuse-values \
      --set-string 'worker.credentials.secretName=' \
      --set worker.credentials.files={} \
      --set-string 'worker.env.K8S_CONNECTIONS_JSON=' \
      --wait --timeout 5m >/dev/null
    wait_worker
    main_kubectl -n "${MAIN_NAMESPACE}" delete secret "${SECRET_NAME}" \
      --ignore-not-found --wait=true >/dev/null
  fi
  if kind get clusters 2>/dev/null | rg -Fxq "${SECONDARY_CLUSTER}"; then
    kind delete cluster --name "${SECONDARY_CLUSTER}"
  fi
  rm -f "${ARTIFACT_PATH}"
  if [[ -d "${STATE_ROOT}" ]]; then
    find "${STATE_ROOT}" -mindepth 1 -maxdepth 1 -type f -delete
    rmdir "${STATE_ROOT}" 2>/dev/null || true
  fi
  printf 'PASS secondary Scope reader removed cluster_id=%s\n' "${SECONDARY_CLUSTER_ID}"
}

status() {
  require_tools
  printf 'cluster_id=%s\ncluster_name=%s\ncontext=%s\n' "${SECONDARY_CLUSTER_ID}" "${SECONDARY_CLUSTER}" "${SECONDARY_CONTEXT}"
  if kind get clusters 2>/dev/null | rg -Fxq "${SECONDARY_CLUSTER}"; then
    printf 'cluster_state=present\n'
  else
    printf 'cluster_state=absent\n'
  fi
  if [[ -f "${ARTIFACT_PATH}" ]]; then
    printf 'artifact_state=present\n'
  else
    printf 'artifact_state=absent\n'
  fi
}

case "${1:-}" in
  up) [[ "$#" == 1 ]] || { printf 'usage: %s {up|down|status}\n' "$0" >&2; exit 2; }; up ;;
  down) [[ "$#" == 1 ]] || { printf 'usage: %s {up|down|status}\n' "$0" >&2; exit 2; }; down ;;
  status) [[ "$#" == 1 ]] || { printf 'usage: %s {up|down|status}\n' "$0" >&2; exit 2; }; status ;;
  *) printf 'usage: %s {up|down|status}\n' "$0" >&2; exit 2 ;;
esac
