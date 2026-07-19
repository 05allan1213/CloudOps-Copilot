#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART_DIR="${ROOT_DIR}/server-monitor/charts/server-monitor"
PLATFORM_CHART_DIR="${ROOT_DIR}/server-monitor/charts/cloudops-kind-platform"
DEMO_CHART_DIR="${ROOT_DIR}/server-monitor/charts/cloudops-demo"
PROFILE_FILE="${CHART_DIR}/values-v3-phase3.yaml"
PLATFORM_VALUES="${PLATFORM_CHART_DIR}/values.yaml"
KIND_CONFIG="${ROOT_DIR}/server-monitor/deploy/kind/kind-config.yaml"
MONITORING_VALUES="${ROOT_DIR}/server-monitor/deploy/kind/kube-prometheus-stack-values.yaml"
VERSIONS_FILE="${ROOT_DIR}/server-monitor/deploy/kind/versions.env"

CLUSTER_NAME="${CLOUDOPS_KIND_CLUSTER:-cloudops-v3-phase3}"
APP_NAMESPACE="${CLOUDOPS_APP_NAMESPACE:-cloudops-system}"
DEMO_NAMESPACE="${CLOUDOPS_DEMO_NAMESPACE:-cloudops-demo}"
MONITORING_NAMESPACE="${CLOUDOPS_MONITORING_NAMESPACE:-cloudops-monitoring}"
MONITORING_RELEASE="${CLOUDOPS_MONITORING_RELEASE:-cloudops-monitoring}"
PLATFORM_RELEASE="${CLOUDOPS_PLATFORM_RELEASE:-cloudops-platform}"
APP_RELEASE="${CLOUDOPS_APP_RELEASE:-cloudops-v3}"
DEMO_RELEASE="${CLOUDOPS_DEMO_RELEASE:-cloudops-demo}"
KPS_CACHE_DIR="${XDG_CACHE_HOME:-${HOME}/.cache}/cloudops-v3/charts"
KPS_PACKAGE=""
RENDERED_FILE="${TMPDIR:-/tmp}/cloudops-v3-phase3-${CLUSTER_NAME}.yaml"
RENDERED_DEMO_FILE="${TMPDIR:-/tmp}/cloudops-v3-phase3-demo-${CLUSTER_NAME}.yaml"
PORT_FORWARD_LOG="${TMPDIR:-/tmp}/cloudops-v3-prometheus-${CLUSTER_NAME}.log"
DEMO_PORT_FORWARD_LOG="${TMPDIR:-/tmp}/cloudops-v3-demo-${CLUSTER_NAME}.log"
port_forward_pid=""
demo_port_forward_pid=""
secret_dir=""
MIN_AVAILABLE_MEMORY_MIB="${CLOUDOPS_MIN_AVAILABLE_MEMORY_MIB:-5120}"

# The chart package checksum is kept in versions.env and checked before any
# cluster mutation. This makes the local demo reproducible without committing
# provider credentials or mutable chart references.
# shellcheck source=../deploy/kind/versions.env
# shellcheck disable=SC1091
source "${VERSIONS_FILE}"
KPS_PACKAGE="${KPS_CACHE_DIR}/kube-prometheus-stack-${KUBE_PROMETHEUS_STACK_VERSION}.tgz"

die() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

cluster_exists() {
  kind get clusters 2>/dev/null | grep -Fxq "${CLUSTER_NAME}"
}

cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${demo_port_forward_pid}" ]]; then
    kill "${demo_port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${secret_dir}" && -d "${secret_dir}" ]]; then
    rm -rf "${secret_dir}"
  fi
}
trap cleanup EXIT

preflight() {
  local available_mib
  for command_name in docker kind kubectl helm jq yq curl sha256sum openssl; do
    require_cmd "${command_name}"
  done
  docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
  [[ -f "${PROFILE_FILE}" ]] || die "missing V3 profile: ${PROFILE_FILE}"
  [[ -f "${KIND_CONFIG}" ]] || die "missing kind config: ${KIND_CONFIG}"
  [[ -f "${MONITORING_VALUES}" ]] || die "missing monitoring values: ${MONITORING_VALUES}"
  [[ -f "${DEMO_CHART_DIR}/Chart.yaml" ]] || die "missing Demo chart: ${DEMO_CHART_DIR}"
  if cluster_exists; then
    die "kind cluster ${CLUSTER_NAME} already exists; choose CLOUDOPS_KIND_CLUSTER or run demo-down explicitly"
  fi
  available_mib="$(awk '/MemAvailable:/ {print int($2/1024)}' /proc/meminfo)"
  [[ "${available_mib}" -ge "${MIN_AVAILABLE_MEMORY_MIB}" ]] || die "at least ${MIN_AVAILABLE_MEMORY_MIB} MiB available memory is required (found ${available_mib} MiB)"
  [[ "$(nproc)" -ge 4 ]] || die "at least four CPU cores are required"
  printf 'PASS: preflight profile=phase3 cluster=%s available_memory_mib=%s cpu=%s\n' "${CLUSTER_NAME}" "${available_mib}" "$(nproc)"
}

render_profile() {
  local platform_render
  platform_render="${RENDERED_FILE}.platform"
  helm lint "${PLATFORM_CHART_DIR}"
  helm lint "${CHART_DIR}" --values "${PROFILE_FILE}"
  helm lint "${DEMO_CHART_DIR}"
  helm template "${PLATFORM_RELEASE}" "${PLATFORM_CHART_DIR}" --namespace "${APP_NAMESPACE}" --values "${PLATFORM_VALUES}" >"${platform_render}"
  helm template "${APP_RELEASE}" "${CHART_DIR}" --namespace "${APP_NAMESPACE}" --values "${PROFILE_FILE}" >"${RENDERED_FILE}"
  helm template "${DEMO_RELEASE}" "${DEMO_CHART_DIR}" --namespace "${DEMO_NAMESPACE}" >"${RENDERED_DEMO_FILE}"
  bash "${ROOT_DIR}/server-monitor/scripts/check-v3-phase3-render.sh" "${RENDERED_FILE}" "${RENDERED_DEMO_FILE}"
  printf 'PASS: Helm profile rendered at %s (platform: %s demo: %s)\n' "${RENDERED_FILE}" "${platform_render}" "${RENDERED_DEMO_FILE}"
}

ensure_monitoring_package() {
  local actual_sha
  mkdir -p "${KPS_CACHE_DIR}"
  if [[ ! -s "${KPS_PACKAGE}" ]]; then
    curl --fail --location --retry 3 --connect-timeout 15 \
      "${KUBE_PROMETHEUS_STACK_URL}" --output "${KPS_PACKAGE}.part"
    mv "${KPS_PACKAGE}.part" "${KPS_PACKAGE}"
  fi
  actual_sha="$(sha256sum "${KPS_PACKAGE}" | awk '{print $1}')"
  [[ "${actual_sha}" == "${KUBE_PROMETHEUS_STACK_SHA256}" ]] || die "kube-prometheus-stack checksum mismatch"
}

create_namespaces() {
  kubectl create namespace "${APP_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create namespace "${DEMO_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create namespace "${MONITORING_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

create_secrets() {
  local database_password root_password webhook_token
  secret_dir="$(mktemp -d "${TMPDIR:-/tmp}/cloudops-v3-secrets.XXXXXX")"
  chmod 700 "${secret_dir}"
  database_password="$(openssl rand -hex 32)"
  root_password="$(openssl rand -hex 32)"
  webhook_token="$(openssl rand -hex 32)"
  printf '%s' "${database_password}" >"${secret_dir}/mysql-password"
  printf '%s' "${root_password}" >"${secret_dir}/mysql-root-password"
  printf '%s' "${webhook_token}" >"${secret_dir}/webhook-token"
  jq -cn \
    --arg cluster "${CLUSTER_NAME}" \
    --arg environment "local-demo" \
    --arg namespace "${DEMO_NAMESPACE}" \
    '[{cluster_id:$cluster,environment:$environment,namespace:$namespace,workload_kind:"Deployment",workload_name:"cloudops-demo-workload",service_name:"cloudops-demo-workload",match_labels:{cluster:$cluster,environment:$environment,namespace:$namespace,deployment:"cloudops-demo-workload"}}]' \
    >"${secret_dir}/signal-target-allowlist.json"
  chmod 600 "${secret_dir}"/*

  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-v3-database \
    --from-file=MYSQL_PASSWORD="${secret_dir}/mysql-password" \
    --from-file=MYSQL_ROOT_PASSWORD="${secret_dir}/mysql-root-password" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-alertmanager-webhook \
    --from-file=token="${secret_dir}/webhook-token" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${MONITORING_NAMESPACE}" create secret generic cloudops-alertmanager-webhook \
    --from-file=token="${secret_dir}/webhook-token" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

build_and_load_images() {
  local revision source_ref api_image migrate_image demo_image
  revision="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
  source_ref="https://github.com/05allan1213/CloudOps-Copilot"
  api_image="cloudops-api:${revision}"
  migrate_image="cloudops-migrate:${revision}"
  demo_image="cloudops-demo:${revision}"
  docker build --target cloudops-api \
    --build-arg VCS_REF="${revision}" --build-arg VCS_SOURCE="${source_ref}" --build-arg VERSION="${revision}" \
    --tag "${api_image}" "${ROOT_DIR}"
  docker build --target cloudops-migrate \
    --build-arg VCS_REF="${revision}" --build-arg VCS_SOURCE="${source_ref}" --build-arg VERSION="${revision}" \
    --tag "${migrate_image}" "${ROOT_DIR}"
  docker build --target cloudops-demo \
    --build-arg VCS_REF="${revision}" --build-arg VCS_SOURCE="${source_ref}" --build-arg VERSION="${revision}" \
    --tag "${demo_image}" "${ROOT_DIR}"
  kind load docker-image "${api_image}" --name "${CLUSTER_NAME}"
  kind load docker-image "${migrate_image}" --name "${CLUSTER_NAME}"
  kind load docker-image "${demo_image}" --name "${CLUSTER_NAME}"
  API_IMAGE_REPOSITORY="cloudops-api" API_IMAGE_TAG="${revision}" MIGRATE_IMAGE_REPOSITORY="cloudops-migrate" MIGRATE_IMAGE_TAG="${revision}" DEMO_IMAGE_REPOSITORY="cloudops-demo" DEMO_IMAGE_TAG="${revision}"
  export API_IMAGE_REPOSITORY API_IMAGE_TAG MIGRATE_IMAGE_REPOSITORY MIGRATE_IMAGE_TAG DEMO_IMAGE_REPOSITORY DEMO_IMAGE_TAG
}

install_monitoring() {
  ensure_monitoring_package
  helm upgrade --install "${MONITORING_RELEASE}" "${KPS_PACKAGE}" \
    --namespace "${MONITORING_NAMESPACE}" --create-namespace \
    --values "${MONITORING_VALUES}" --wait --timeout 10m
  kubectl wait --for=condition=Established --timeout=120s crd/servicemonitors.monitoring.coreos.com
  kubectl wait --for=condition=Established --timeout=120s crd/prometheusrules.monitoring.coreos.com
}

install_platform() {
  helm upgrade --install "${PLATFORM_RELEASE}" "${PLATFORM_CHART_DIR}" \
    --namespace "${APP_NAMESPACE}" --values "${PLATFORM_VALUES}" \
    --wait --timeout 5m
}

install_application() {
  helm upgrade --install "${APP_RELEASE}" "${CHART_DIR}" \
    --namespace "${APP_NAMESPACE}" --values "${PROFILE_FILE}" \
    --set "v3.images.api.tag=${API_IMAGE_TAG}" \
    --set "v3.images.migrate.tag=${MIGRATE_IMAGE_TAG}" \
    --set "v3.images.api.repository=${API_IMAGE_REPOSITORY}" \
    --set "v3.images.migrate.repository=${MIGRATE_IMAGE_REPOSITORY}" \
    --set-file "v3.commonEnv.SIGNAL_TARGET_ALLOWLIST_JSON=${secret_dir}/signal-target-allowlist.json" \
    --wait --wait-for-jobs --timeout 10m
}

install_demo() {
  helm upgrade --install "${DEMO_RELEASE}" "${DEMO_CHART_DIR}" \
    --namespace "${DEMO_NAMESPACE}" \
    --set "image.repository=${DEMO_IMAGE_REPOSITORY}" \
    --set "image.tag=${DEMO_IMAGE_TAG}" \
    --set "sourceRevision=${DEMO_IMAGE_TAG}" \
    --set "clusterName=${CLUSTER_NAME}" \
    --wait --timeout 5m
}

prometheus_service() {
  kubectl -n "${MONITORING_NAMESPACE}" get svc -o json | jq -r --arg instance "${MONITORING_RELEASE}" \
    '[.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "prometheus" and .metadata.labels["app.kubernetes.io/instance"] == $instance) | .metadata.name][0] // empty'
}

check_observability() {
  local service_name query_result revision version_result
  kubectl -n "${APP_NAMESPACE}" wait --for=condition=available --timeout=180s deployment/cloudops-api
  kubectl -n "${APP_NAMESPACE}" get servicemonitor/cloudops-api prometheusrule/cloudops-api >/dev/null
  kubectl -n "${DEMO_NAMESPACE}" wait --for=condition=available --timeout=180s deployment/cloudops-demo-workload
  kubectl -n "${DEMO_NAMESPACE}" get podmonitor/cloudops-demo-workload prometheusrule/cloudops-demo-workload >/dev/null
  kubectl -n "${DEMO_NAMESPACE}" port-forward svc/cloudops-demo-workload 18081:8080 >"${DEMO_PORT_FORWARD_LOG}" 2>&1 &
  demo_port_forward_pid="$!"
  for _ in $(seq 1 30); do
    if curl --fail --silent http://127.0.0.1:18081/readyz >/dev/null 2>&1; then break; fi
    sleep 2
  done
  curl --fail --silent http://127.0.0.1:18081/readyz >/dev/null || die "Demo workload did not become ready"
  revision="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
  version_result="$(curl --fail --silent http://127.0.0.1:18081/version)"
  jq -e --arg revision "${revision}" '.source_revision == $revision and .version == $revision' <<<"${version_result}" >/dev/null || die "Demo /version does not match the built source revision"
  curl --fail --silent http://127.0.0.1:18081/ >/dev/null || die "Demo baseline business request failed"
  service_name="$(prometheus_service)"
  [[ -n "${service_name}" ]] || die "Prometheus service was not found"
  kubectl -n "${MONITORING_NAMESPACE}" port-forward "svc/${service_name}" 19090:9090 >"${PORT_FORWARD_LOG}" 2>&1 &
  port_forward_pid="$!"
  for _ in $(seq 1 30); do
    if curl --fail --silent http://127.0.0.1:19090/-/ready >/dev/null 2>&1; then break; fi
    sleep 2
  done
  curl --fail --silent http://127.0.0.1:19090/-/ready >/dev/null || die "Prometheus did not become ready"
  query_result="$(curl --fail --silent http://127.0.0.1:19090/api/v1/targets)"
  jq -e --arg service "cloudops-api-internal" \
    '[.data.activeTargets[] | select(.labels.service == $service and .health == "up")] | length > 0' \
    <<<"${query_result}" >/dev/null || {
      printf 'Prometheus target labels:\n' >&2
      jq -c '.data.activeTargets[].labels' <<<"${query_result}" >&2
      die "CloudOps API ServiceMonitor target is not healthy"
    }
  jq -e --arg namespace "${DEMO_NAMESPACE}" \
    '[.data.activeTargets[] | select(.labels.namespace == $namespace and .health == "up")] | length >= 2' \
    <<<"${query_result}" >/dev/null || die "both Demo PodMonitor targets are not healthy"
  query_result="$(curl --fail --silent http://127.0.0.1:19090/api/v1/rules)"
  jq -e '[.data.groups[].rules[] | select(.name == "CloudOpsAPIAvailability")] | length == 1' \
    <<<"${query_result}" >/dev/null || die "CloudOps PrometheusRule was not loaded"
  jq -e '[.data.groups[].rules[] | select(.name == "CloudOpsDemoRequiredEnvMissing" or .name == "CloudOpsDemoErrorRateHigh")] | length == 2' \
    <<<"${query_result}" >/dev/null || die "Demo Prometheus rules were not loaded"
  printf 'PASS: API and two Demo targets are healthy, baseline request succeeds, and all Phase 3 rules are loaded\n'
}

up() {
  preflight
  render_profile
  ensure_monitoring_package
  kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}" --wait 120s
  create_namespaces
  create_secrets
  install_monitoring
  install_platform
  build_and_load_images
  install_application
  install_demo
  check_observability
  printf 'PASS: V3 phase3 kind demo is running (cluster=%s namespace=%s)\n' "${CLUSTER_NAME}" "${APP_NAMESPACE}"
}

down() {
  if cluster_exists; then
    kind delete cluster --name "${CLUSTER_NAME}"
    printf 'PASS: deleted kind cluster %s\n' "${CLUSTER_NAME}"
  else
    printf 'PASS: kind cluster %s was already absent\n' "${CLUSTER_NAME}"
  fi
}

check() {
  if ! cluster_exists; then
    die "kind cluster ${CLUSTER_NAME} does not exist; run demo-up first"
  fi
  check_observability
}

case "${1:-}" in
  preflight) preflight ;;
  render) render_profile ;;
  up) up ;;
  check) check ;;
  down) down ;;
  *)
    printf 'usage: %s {preflight|render|up|check|down}\n' "$0" >&2
    exit 2
    ;;
esac
