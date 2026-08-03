#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run_id="${CLOUDOPS_REAL_INTEGRATION_RUN_ID:?CLOUDOPS_REAL_INTEGRATION_RUN_ID is required}"
state_root="${CLOUDOPS_STATE_DIR:-${repo_root}/.cloudops}/integration/${run_id}/scope-environment"
artifact_path="${state_root}.json"
main_context="kind-cloudops-local"
main_namespace="cloudops-system"
release_name="cloudops"
secret_name="cloudops-integration-kubeconfigs"
kubeconfig_filename="secondary.yaml"
kubeconfig_path="${state_root}/${kubeconfig_filename}"
kind_node_image="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"

if [[ ! "${run_id}" =~ ^[A-Za-z0-9._-]+$ ]]; then
  printf 'FAIL: invalid integration run identity\n' >&2
  exit 1
fi

suffix="$(printf '%s' "${run_id}" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9' | tail -c 24)"
[[ -n "${suffix}" ]] || { printf 'FAIL: integration run identity has no usable suffix\n' >&2; exit 1; }
secondary_cluster="cloudops-integration-scope-${suffix}"
secondary_context="kind-${secondary_cluster}"
secondary_cluster_id="${secondary_cluster}"

main_kubectl() {
  kubectl --context "${main_context}" "$@"
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
  local staging_file
  mkdir -p "${state_root}"
  chmod 700 "${state_root}"
  staging_file="$(mktemp "${state_root}/.scope-environment.XXXXXX")"
  jq -n \
    --arg run_id "${run_id}" \
    --arg cluster_name "${secondary_cluster}" \
    --arg cluster_id "${secondary_cluster_id}" \
    --arg context "${secondary_context}" \
    --arg kubeconfig "${kubeconfig_path}" \
    '{run_id:$run_id, cluster_name:$cluster_name, cluster_id:$cluster_id, context:$context, kubeconfig:$kubeconfig}' \
    >"${staging_file}"
  chmod 600 "${staging_file}"
  mv "${staging_file}" "${artifact_path}"
  chmod 600 "${artifact_path}"
}

reader_connections() {
  jq -cn \
    --arg external_id "${secondary_cluster_id}" \
    --arg context "${secondary_context}" \
    --arg kubeconfig "/var/run/secrets/cloudops/worker/${kubeconfig_filename}" \
    '[
      {cluster_id:"cloudops-local",in_cluster:true,allowed_namespaces:["cloudops-system","demo"],default_namespace:"cloudops-system"},
      {cluster_id:$external_id,kubeconfig:$kubeconfig,context:$context,in_cluster:false,allowed_namespaces:["default"],default_namespace:"default",request_timeout_seconds:12}
    ]'
}

wait_worker() {
  main_kubectl -n "${main_namespace}" rollout status deployment/cloudops-worker --timeout=5m
}

up() {
  local staging_file connections
  require_tools
  kubectl --context "${main_context}" cluster-info >/dev/null
  mkdir -p "${state_root}"
  chmod 700 "${state_root}"
  if ! kind get clusters 2>/dev/null | rg -Fxq "${secondary_cluster}"; then
    kind create cluster --name "${secondary_cluster}" --image "${kind_node_image}" --wait 120s
  fi
  staging_file="$(mktemp "${state_root}/.kubeconfig.XXXXXX")"
  kind get kubeconfig --internal --name "${secondary_cluster}" >"${staging_file}"
  chmod 600 "${staging_file}"
  mv "${staging_file}" "${kubeconfig_path}"
  kubectl --kubeconfig "${kubeconfig_path}" --context "${secondary_context}" get namespace default >/dev/null
  main_kubectl -n "${main_namespace}" create secret generic "${secret_name}" \
    --from-file="${kubeconfig_filename}=${kubeconfig_path}" \
    --dry-run=client -o yaml | main_kubectl apply -f - >/dev/null
  main_kubectl -n "${main_namespace}" label secret "${secret_name}" \
    cloudops.io/managed-by=cloudops-real-integration cloudops.io/run-id="${run_id}" --overwrite >/dev/null
  connections="$(reader_connections)"
  helm --kube-context "${main_context}" -n "${main_namespace}" upgrade "${release_name}" "${repo_root}/charts/cloudops" \
    --reuse-values \
    --set-string "worker.credentials.secretName=${secret_name}" \
    --set-string "worker.credentials.files.SECONDARY_KUBECONFIG_FILE=${kubeconfig_filename}" \
    --set-string "worker.env.K8S_CONNECTIONS_JSON=${connections}" \
    --wait --timeout 5m >/dev/null
  wait_worker
  write_artifact
  printf 'PASS independent Scope ready cluster_id=%s\n' "${secondary_cluster_id}"
}

down() {
  require_tools
  if kubectl --context "${main_context}" cluster-info >/dev/null 2>&1 && \
     helm --kube-context "${main_context}" -n "${main_namespace}" status "${release_name}" >/dev/null 2>&1; then
    helm --kube-context "${main_context}" -n "${main_namespace}" upgrade "${release_name}" "${repo_root}/charts/cloudops" \
      --reuse-values \
      --set-string 'worker.credentials.secretName=' \
      --set worker.credentials.files={} \
      --set-string 'worker.env.K8S_CONNECTIONS_JSON=' \
      --wait --timeout 5m >/dev/null
    wait_worker
    main_kubectl -n "${main_namespace}" delete secret "${secret_name}" \
      --ignore-not-found --wait=true >/dev/null
  fi
  if kind get clusters 2>/dev/null | rg -Fxq "${secondary_cluster}"; then
    kind delete cluster --name "${secondary_cluster}"
  fi
  rm -f "${artifact_path}" "${kubeconfig_path}"
  rmdir "${state_root}" 2>/dev/null || true
  printf 'PASS independent Scope removed cluster_id=%s\n' "${secondary_cluster_id}"
}

status() {
  require_tools
  printf 'cluster_id=%s\ncluster_name=%s\ncontext=%s\n' \
    "${secondary_cluster_id}" "${secondary_cluster}" "${secondary_context}"
  if kind get clusters 2>/dev/null | rg -Fxq "${secondary_cluster}"; then
    printf 'cluster_state=present\n'
  else
    printf 'cluster_state=absent\n'
  fi
  if [[ -f "${artifact_path}" ]]; then
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
