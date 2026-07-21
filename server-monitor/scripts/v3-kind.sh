#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART_DIR="${ROOT_DIR}/charts/cloudops"
PLATFORM_CHART_DIR="${ROOT_DIR}/server-monitor/charts/cloudops-kind-platform"
DEMO_CHART_DIR="${ROOT_DIR}/server-monitor/charts/cloudops-demo"
PROFILE_FILE="${CHART_DIR}/values-phase3.yaml"
PLATFORM_VALUES="${PLATFORM_CHART_DIR}/values.yaml"
KIND_CONFIG="${ROOT_DIR}/server-monitor/deploy/kind/kind-config.yaml"
MONITORING_VALUES="${ROOT_DIR}/server-monitor/deploy/kind/kube-prometheus-stack-values.yaml"
VERSIONS_FILE="${ROOT_DIR}/server-monitor/deploy/kind/versions.env"

CLUSTER_NAME="${CLOUDOPS_KIND_CLUSTER:-cloudops-v3-phase3}"
APP_NAMESPACE="${CLOUDOPS_APP_NAMESPACE:-cloudops-system}"
DEMO_NAMESPACE="${CLOUDOPS_DEMO_NAMESPACE:-cloudops-demo}"
MONITORING_NAMESPACE="${CLOUDOPS_MONITORING_NAMESPACE:-cloudops-monitoring}"
ECK_OPERATOR_NAMESPACE="${CLOUDOPS_ECK_OPERATOR_NAMESPACE:-elastic-system}"
MONITORING_RELEASE="${CLOUDOPS_MONITORING_RELEASE:-cloudops-monitoring}"
ECK_OPERATOR_RELEASE="${CLOUDOPS_ECK_OPERATOR_RELEASE:-cloudops-eck}"
PLATFORM_RELEASE="${CLOUDOPS_PLATFORM_RELEASE:-cloudops-platform}"
APP_RELEASE="${CLOUDOPS_APP_RELEASE:-cloudops-v3}"
DEMO_RELEASE="${CLOUDOPS_DEMO_RELEASE:-cloudops-demo}"
CHART_CACHE_DIR="${XDG_CACHE_HOME:-${HOME}/.cache}/cloudops-v3/charts"
KPS_PACKAGE=""
ECK_PACKAGE=""
RENDERED_FILE="${TMPDIR:-/tmp}/cloudops-v3-phase3-${CLUSTER_NAME}.yaml"
RENDERED_DEMO_FILE="${TMPDIR:-/tmp}/cloudops-v3-phase3-demo-${CLUSTER_NAME}.yaml"
PORT_FORWARD_LOG="${TMPDIR:-/tmp}/cloudops-v3-prometheus-${CLUSTER_NAME}.log"
DEMO_PORT_FORWARD_LOG="${TMPDIR:-/tmp}/cloudops-v3-demo-${CLUSTER_NAME}.log"
TEMPO_PORT_FORWARD_LOG="${TMPDIR:-/tmp}/cloudops-v3-tempo-${CLUSTER_NAME}.log"
ELASTICSEARCH_PORT_FORWARD_LOG="${TMPDIR:-/tmp}/cloudops-v3-elasticsearch-${CLUSTER_NAME}.log"
port_forward_pid=""
demo_port_forward_pid=""
tempo_port_forward_pid=""
elasticsearch_port_forward_pid=""
secret_dir=""
MIN_AVAILABLE_MEMORY_MIB="${CLOUDOPS_MIN_AVAILABLE_MEMORY_MIB:-5120}"
MIN_INOTIFY_INSTANCES="${CLOUDOPS_MIN_INOTIFY_INSTANCES:-256}"

# The chart package checksum is kept in versions.env and checked before any
# cluster mutation. This makes the local demo reproducible without committing
# provider credentials or mutable chart references.
# shellcheck source=../deploy/kind/versions.env
# shellcheck disable=SC1091
source "${VERSIONS_FILE}"
KPS_PACKAGE="${CHART_CACHE_DIR}/kube-prometheus-stack-${KUBE_PROMETHEUS_STACK_VERSION}.tgz"
ECK_PACKAGE="${CHART_CACHE_DIR}/eck-operator-${ECK_OPERATOR_VERSION}.tgz"
OTEL_GRPC_ENDPOINT="cloudops-otel-collector.${APP_NAMESPACE}.svc.cluster.local:4317"

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
  if [[ -n "${tempo_port_forward_pid}" ]]; then
    kill "${tempo_port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${elasticsearch_port_forward_pid}" ]]; then
    kill "${elasticsearch_port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${secret_dir}" && -d "${secret_dir}" ]]; then
    rm -rf "${secret_dir}"
  fi
}
trap cleanup EXIT

preflight() {
  local available_mib inotify_instances
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
  inotify_instances="$(cat /proc/sys/fs/inotify/max_user_instances)"
  [[ "${inotify_instances}" -ge "${MIN_INOTIFY_INSTANCES}" ]] || die "fs.inotify.max_user_instances must be at least ${MIN_INOTIFY_INSTANCES} for a disposable kind node (found ${inotify_instances})"
  printf 'PASS: preflight profile=phase3 cluster=%s available_memory_mib=%s cpu=%s inotify_instances=%s\n' "${CLUSTER_NAME}" "${available_mib}" "$(nproc)" "${inotify_instances}"
}

validate_version_lock() {
  local actual
  [[ "${TEMPO_DEPLOYMENT_MODE}" == "monolithic" && "${TEMPO_TARGET}" == "all" ]] || die "Tempo lock must be monolithic target=all"
  [[ "${ECK_OPERATOR_URL}" == *"/eck-operator-${ECK_OPERATOR_VERSION}.tgz" ]] || die "ECK package URL/version mismatch"
  for digest in \
    "${ECK_OPERATOR_IMAGE_DIGEST}" \
    "${ELASTICSEARCH_IMAGE_DIGEST}" \
    "${KIBANA_IMAGE_DIGEST}" \
    "${FILEBEAT_IMAGE_DIGEST}" \
    "${OTEL_COLLECTOR_IMAGE_DIGEST}" \
    "${TEMPO_IMAGE_DIGEST}"; do
    [[ "${digest}" =~ ^sha256:[a-f0-9]{64}$ ]] || die "observability lock contains an invalid image digest"
  done

  actual="$(yq -r '.observability.elastic.version' "${PLATFORM_VALUES}")"
  [[ "${actual}" == "${ELASTIC_STACK_VERSION}" ]] || die "Elastic Stack values/version lock mismatch"
  actual="$(yq -r '.observability.otelCollector.image.tag' "${PLATFORM_VALUES}")"
  [[ "${actual}" == "${OTEL_COLLECTOR_VERSION}" ]] || die "OTel Collector values/version lock mismatch"
  actual="$(yq -r '.observability.tempo.image.tag' "${PLATFORM_VALUES}")"
  [[ "${actual}" == "${TEMPO_VERSION}" ]] || die "Tempo values/version lock mismatch"

  local pair
  # shellcheck disable=SC2153 # TEMPO_IMAGE_REPOSITORY is sourced from the version lock.
  for pair in \
    ".observability.elastic.elasticsearch.image.repository=${ELASTICSEARCH_IMAGE_REPOSITORY}" \
    ".observability.elastic.elasticsearch.image.digest=${ELASTICSEARCH_IMAGE_DIGEST}" \
    ".observability.elastic.kibana.image.repository=${KIBANA_IMAGE_REPOSITORY}" \
    ".observability.elastic.kibana.image.digest=${KIBANA_IMAGE_DIGEST}" \
    ".observability.elastic.filebeat.image.repository=${FILEBEAT_IMAGE_REPOSITORY}" \
    ".observability.elastic.filebeat.image.digest=${FILEBEAT_IMAGE_DIGEST}" \
    ".observability.otelCollector.image.repository=${OTEL_COLLECTOR_IMAGE_REPOSITORY}" \
    ".observability.otelCollector.image.digest=${OTEL_COLLECTOR_IMAGE_DIGEST}" \
    ".observability.tempo.image.repository=${TEMPO_IMAGE_REPOSITORY}" \
    ".observability.tempo.image.digest=${TEMPO_IMAGE_DIGEST}" \
    ".observability.tempo.target=${TEMPO_TARGET}"; do
    actual="$(yq -r "${pair%%=*}" "${PLATFORM_VALUES}")"
    [[ "${actual}" == "${pair#*=}" ]] || die "platform values/version lock mismatch at ${pair%%=*}"
  done
}

render_profile() {
  local platform_render
  platform_render="${RENDERED_FILE}.platform"
  validate_version_lock
  helm lint "${PLATFORM_CHART_DIR}"
  helm lint "${CHART_DIR}" --values "${PROFILE_FILE}"
  helm lint "${DEMO_CHART_DIR}"
  helm template "${PLATFORM_RELEASE}" "${PLATFORM_CHART_DIR}" --namespace "${APP_NAMESPACE}" --values "${PLATFORM_VALUES}" >"${platform_render}"
  helm template "${APP_RELEASE}" "${CHART_DIR}" --namespace "${APP_NAMESPACE}" --values "${PROFILE_FILE}" \
    --set-string "commonEnv.TRACE_OTLP_ENDPOINT=${OTEL_GRPC_ENDPOINT}" >"${RENDERED_FILE}"
  helm template "${DEMO_RELEASE}" "${DEMO_CHART_DIR}" --namespace "${DEMO_NAMESPACE}" \
    --set-string "trace.otlpEndpoint=${OTEL_GRPC_ENDPOINT}" >"${RENDERED_DEMO_FILE}"
  bash "${ROOT_DIR}/server-monitor/scripts/check-v3-phase3-render.sh" "${platform_render}" "${RENDERED_FILE}" "${RENDERED_DEMO_FILE}"
  printf 'PASS: Helm profile rendered at %s (platform: %s demo: %s)\n' "${RENDERED_FILE}" "${platform_render}" "${RENDERED_DEMO_FILE}"
}

ensure_monitoring_package() {
  local actual_sha
  mkdir -p "${CHART_CACHE_DIR}"
  if [[ ! -s "${KPS_PACKAGE}" ]]; then
    curl --fail --location --retry 3 --connect-timeout 15 \
      "${KUBE_PROMETHEUS_STACK_URL}" --output "${KPS_PACKAGE}.part"
    mv "${KPS_PACKAGE}.part" "${KPS_PACKAGE}"
  fi
  actual_sha="$(sha256sum "${KPS_PACKAGE}" | awk '{print $1}')"
  [[ "${actual_sha}" == "${KUBE_PROMETHEUS_STACK_SHA256}" ]] || die "kube-prometheus-stack checksum mismatch"
}

ensure_eck_package() {
  local actual_sha
  mkdir -p "${CHART_CACHE_DIR}"
  if [[ ! -s "${ECK_PACKAGE}" ]]; then
    curl --fail --location --retry 3 --connect-timeout 15 \
      "${ECK_OPERATOR_URL}" --output "${ECK_PACKAGE}.part"
    mv "${ECK_PACKAGE}.part" "${ECK_PACKAGE}"
  fi
  actual_sha="$(sha256sum "${ECK_PACKAGE}" | awk '{print $1}')"
  [[ "${actual_sha}" == "${ECK_OPERATOR_SHA256}" ]] || die "ECK operator checksum mismatch"
}

monitoring_images() {
  helm template "${MONITORING_RELEASE}" "${KPS_PACKAGE}" \
    --namespace "${MONITORING_NAMESPACE}" --values "${MONITORING_VALUES}" 2>/dev/null |
    yq eval-all '.. | select(tag == "!!map") | .image // ""' - |
    sed '/^$/d; /^---$/d'
  printf '%s\n' "quay.io/prometheus-operator/prometheus-config-reloader:${KUBE_PROMETHEUS_OPERATOR_VERSION}"
}

pinned_platform_images() {
  printf '%s\n' \
    "mysql:${MYSQL_VERSION}@${MYSQL_IMAGE_DIGEST}" \
    "${ECK_OPERATOR_IMAGE_REPOSITORY}:${ECK_OPERATOR_VERSION}@${ECK_OPERATOR_IMAGE_DIGEST}" \
    "${ELASTICSEARCH_IMAGE_REPOSITORY}:${ELASTIC_STACK_VERSION}@${ELASTICSEARCH_IMAGE_DIGEST}" \
    "${KIBANA_IMAGE_REPOSITORY}:${ELASTIC_STACK_VERSION}@${KIBANA_IMAGE_DIGEST}" \
    "${FILEBEAT_IMAGE_REPOSITORY}:${ELASTIC_STACK_VERSION}@${FILEBEAT_IMAGE_DIGEST}" \
    "${OTEL_COLLECTOR_IMAGE_REPOSITORY}:${OTEL_COLLECTOR_VERSION}@${OTEL_COLLECTOR_IMAGE_DIGEST}" \
    "${TEMPO_IMAGE_REPOSITORY}:${TEMPO_VERSION}@${TEMPO_IMAGE_DIGEST}"
}

normalized_node_image_ref() {
  local first_segment reference="$1"
  if [[ "${reference}" != */* ]]; then
    printf 'docker.io/library/%s\n' "${reference}"
    return
  fi
  first_segment="${reference%%/*}"
  if [[ "${first_segment}" != *.* && "${first_segment}" != *:* && "${first_segment}" != "localhost" ]]; then
    printf 'docker.io/%s\n' "${reference}"
    return
  fi
  printf '%s\n' "${reference}"
}

preload_external_images() {
  local digest_ref image node node_ref save_ref
  node="${CLUSTER_NAME}-control-plane"
  {
    monitoring_images
    pinned_platform_images
  } | sort -u | while IFS= read -r image; do
    [[ -n "${image}" ]] || continue
    printf 'Preloading pinned image through host Docker: %s\n' "${image}"
    docker pull --platform linux/amd64 "${image}" >/dev/null
    save_ref="${image%@*}"
    if [[ "${image}" == *@sha256:* ]]; then
      docker image tag "${image}" "${save_ref}"
    fi
    # kind load currently imports every manifest-list platform and can fail on
    # Docker's locally pruned attestations. Import the host-resolved amd64
    # archive directly into the disposable node instead. Docker can inspect a
    # tag@digest reference but docker save accepts its local tag reference.
    docker save "${save_ref}" |
      docker exec -i "${node}" ctr --namespace=k8s.io images import --digests --snapshotter=overlayfs - >/dev/null
    node_ref="$(normalized_node_image_ref "${save_ref}")"
    if [[ "${image}" == *@sha256:* ]]; then
      digest_ref="${node_ref%:*}@${image##*@}"
      docker exec "${node}" ctr --namespace=k8s.io images tag "${node_ref}" "${digest_ref}" >/dev/null
    else
      digest_ref="${node_ref}"
    fi
    docker exec "${node}" crictl inspecti "${digest_ref}" >/dev/null
  done
  printf 'PASS: external monitoring/platform images are present in the kind node before Helm creates Pods\n'
}

create_namespaces() {
  kubectl create namespace "${APP_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create namespace "${DEMO_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create namespace "${MONITORING_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create namespace "${ECK_OPERATOR_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl label namespace "${APP_NAMESPACE}" "cloudops.io/monitoring=enabled" --overwrite >/dev/null
  kubectl label namespace "${DEMO_NAMESPACE}" "cloudops.io/monitoring=enabled" --overwrite >/dev/null
}

create_secrets() {
  local api_database_password worker_database_password migrate_database_password baseline_database_password root_password webhook_token
  secret_dir="$(mktemp -d "${TMPDIR:-/tmp}/cloudops-v3-secrets.XXXXXX")"
  chmod 700 "${secret_dir}"
  api_database_password="$(openssl rand -hex 32)"
  worker_database_password="$(openssl rand -hex 32)"
  migrate_database_password="$(openssl rand -hex 32)"
  baseline_database_password="$(openssl rand -hex 32)"
  root_password="$(openssl rand -hex 32)"
  webhook_token="$(openssl rand -hex 32)"
  printf '%s' "${api_database_password}" >"${secret_dir}/mysql-api-password"
  printf '%s' "${worker_database_password}" >"${secret_dir}/mysql-worker-password"
  printf '%s' "${migrate_database_password}" >"${secret_dir}/mysql-migrate-password"
  printf '%s' "${baseline_database_password}" >"${secret_dir}/mysql-baseline-password"
  printf '%s' "${root_password}" >"${secret_dir}/mysql-root-password"
  printf '%s' "${webhook_token}" >"${secret_dir}/webhook-token"
  printf '%s\n' \
    'cloudops_reader:' \
    '  cluster: [ "monitor" ]' \
    '  indices:' \
    '    - names: [ "logs-cloudops-*" ]' \
    '      privileges: [ "read", "view_index_metadata" ]' \
    >"${secret_dir}/elasticsearch-roles.yml"
  jq -cn \
    --arg cluster "${CLUSTER_NAME}" \
    --arg environment "local-demo" \
    --arg namespace "${DEMO_NAMESPACE}" \
    '[{cluster_id:$cluster,environment:$environment,namespace:$namespace,workload_kind:"Deployment",workload_name:"cloudops-demo-workload",service_name:"cloudops-demo-workload",match_labels:{cluster:$cluster,environment:$environment,namespace:$namespace,deployment:"cloudops-demo-workload"}}]' \
    >"${secret_dir}/signal-target-allowlist.json"
  chmod 600 "${secret_dir}"/*

  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-mysql-root \
    --from-file=MYSQL_ROOT_PASSWORD="${secret_dir}/mysql-root-password" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-api-database \
    --from-file=MYSQL_PASSWORD="${secret_dir}/mysql-api-password" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-worker-database \
    --from-file=MYSQL_PASSWORD="${secret_dir}/mysql-worker-password" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-migrate-database \
    --from-file=MYSQL_PASSWORD="${secret_dir}/mysql-migrate-password" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-baseline-database \
    --from-file=MYSQL_PASSWORD="${secret_dir}/mysql-baseline-password" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-alertmanager-webhook \
    --from-file=token="${secret_dir}/webhook-token" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${MONITORING_NAMESPACE}" create secret generic cloudops-alertmanager-webhook \
    --from-file=token="${secret_dir}/webhook-token" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "${APP_NAMESPACE}" create secret generic cloudops-elasticsearch-roles \
    --from-file=roles.yml="${secret_dir}/elasticsearch-roles.yml" \
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

install_eck() {
  ensure_eck_package
  helm upgrade --install "${ECK_OPERATOR_RELEASE}" "${ECK_PACKAGE}" \
    --namespace "${ECK_OPERATOR_NAMESPACE}" --create-namespace \
    --set installCRDs=true \
    --set createClusterScopedResources=false \
    --set webhook.enabled=false \
    --set config.validateStorageClass=false \
    --set telemetry.disabled=true \
    --set-string "image.repository=${ECK_OPERATOR_IMAGE_REPOSITORY}" \
    --set-string "image.tag=${ECK_OPERATOR_VERSION}" \
    --set-string "image.digest=${ECK_OPERATOR_IMAGE_DIGEST}" \
    --set-string "managedNamespaces[0]=${APP_NAMESPACE}" \
    --wait --timeout 10m
  for crd in \
    elasticsearches.elasticsearch.k8s.elastic.co \
    kibanas.kibana.k8s.elastic.co \
    beats.beat.k8s.elastic.co; do
    kubectl wait --for=condition=Established --timeout=120s "crd/${crd}"
  done
  kubectl -n "${ECK_OPERATOR_NAMESPACE}" rollout status statefulset/elastic-operator --timeout=180s
}

install_platform() {
  helm upgrade --install "${PLATFORM_RELEASE}" "${PLATFORM_CHART_DIR}" \
    --namespace "${APP_NAMESPACE}" --values "${PLATFORM_VALUES}" \
    --wait --timeout 15m
}

install_application() {
  helm upgrade --install "${APP_RELEASE}" "${CHART_DIR}" \
    --namespace "${APP_NAMESPACE}" --values "${PROFILE_FILE}" \
    --set-string "images.api.tag=${API_IMAGE_TAG}" \
    --set-string "images.migrate.tag=${MIGRATE_IMAGE_TAG}" \
    --set-string "images.api.repository=${API_IMAGE_REPOSITORY}" \
    --set-string "images.migrate.repository=${MIGRATE_IMAGE_REPOSITORY}" \
    --set-string "commonEnv.TRACE_OTLP_ENDPOINT=${OTEL_GRPC_ENDPOINT}" \
    --set-file "api.env.SIGNAL_TARGET_ALLOWLIST_JSON=${secret_dir}/signal-target-allowlist.json" \
    --wait --wait-for-jobs --timeout 10m
}

install_demo() {
  helm upgrade --install "${DEMO_RELEASE}" "${DEMO_CHART_DIR}" \
    --namespace "${DEMO_NAMESPACE}" \
    --set "image.repository=${DEMO_IMAGE_REPOSITORY}" \
    --set "image.tag=${DEMO_IMAGE_TAG}" \
    --set "sourceRevision=${DEMO_IMAGE_TAG}" \
    --set "clusterName=${CLUSTER_NAME}" \
    --set-string "trace.otlpEndpoint=${OTEL_GRPC_ENDPOINT}" \
    --set-string "trace.sampleRate=1" \
    --wait --timeout 5m
}

prometheus_service() {
  kubectl -n "${MONITORING_NAMESPACE}" get svc -o json | jq -r --arg instance "${MONITORING_RELEASE}" \
    '[.items[] | select(.metadata.labels["app.kubernetes.io/name"] == "prometheus" and .metadata.labels["app.kubernetes.io/instance"] == $instance) | .metadata.name][0] // empty'
}

auth_can_i() {
  kubectl auth can-i "$@" 2>/dev/null || true
}

check_namespace_rbac() {
  local filebeat_identity otel_identity operator_identity
  filebeat_identity="system:serviceaccount:${APP_NAMESPACE}:cloudops-filebeat"
  otel_identity="system:serviceaccount:${APP_NAMESPACE}:cloudops-otel-collector"
  operator_identity="system:serviceaccount:${ECK_OPERATOR_NAMESPACE}:elastic-operator"

  for namespace in "${APP_NAMESPACE}" "${DEMO_NAMESPACE}"; do
    [[ "$(auth_can_i --as="${filebeat_identity}" list pods --namespace "${namespace}")" == "yes" ]] || die "Filebeat cannot discover pods in ${namespace}"
    [[ "$(auth_can_i --as="${otel_identity}" list pods --namespace "${namespace}")" == "yes" ]] || die "OTel Collector cannot discover pods in ${namespace}"
    [[ "$(auth_can_i --as="${otel_identity}" list replicasets.apps --namespace "${namespace}")" == "yes" ]] || die "OTel Collector cannot discover ReplicaSets in ${namespace}"
  done
  [[ "$(auth_can_i --as="${filebeat_identity}" list pods --all-namespaces)" == "no" ]] || die "Filebeat unexpectedly has cluster-wide pod discovery"
  [[ "$(auth_can_i --as="${otel_identity}" list pods --all-namespaces)" == "no" ]] || die "OTel Collector unexpectedly has cluster-wide pod discovery"
  [[ "$(auth_can_i --as="${filebeat_identity}" get secrets --namespace "${APP_NAMESPACE}")" == "no" ]] || die "Filebeat unexpectedly has Secret access"
  [[ "$(auth_can_i --as="${otel_identity}" get secrets --namespace "${APP_NAMESPACE}")" == "no" ]] || die "OTel Collector unexpectedly has Secret access"
  [[ "$(auth_can_i --as="${otel_identity}" create deployments.apps --namespace "${APP_NAMESPACE}")" == "no" ]] || die "OTel Collector unexpectedly has workload write access"
  [[ "$(auth_can_i --as="${operator_identity}" list secrets --namespace "${APP_NAMESPACE}")" == "yes" ]] || die "ECK operator cannot reconcile the managed namespace"
  [[ "$(auth_can_i --as="${operator_identity}" list secrets --namespace "${DEMO_NAMESPACE}")" == "no" ]] || die "ECK operator unexpectedly manages the Demo namespace"
  [[ "$(auth_can_i --as="${operator_identity}" list secrets --namespace default)" == "no" ]] || die "ECK operator unexpectedly manages namespaces outside the allowlist"
}

check_platform_observability() {
  local elasticsearch_name kibana_name filebeat_name otel_name tempo_name
  local collector_config tempo_config collector_json tempo_json elastic_password elastic_result trace_result
  elasticsearch_name="$(yq -r '.observability.elastic.elasticsearch.name' "${PLATFORM_VALUES}")"
  kibana_name="$(yq -r '.observability.elastic.kibana.name' "${PLATFORM_VALUES}")"
  filebeat_name="$(yq -r '.observability.elastic.filebeat.name' "${PLATFORM_VALUES}")"
  otel_name="$(yq -r '.observability.otelCollector.name' "${PLATFORM_VALUES}")"
  tempo_name="$(yq -r '.observability.tempo.name' "${PLATFORM_VALUES}")"

  kubectl -n "${APP_NAMESPACE}" wait --for="jsonpath={.status.health}=green" --timeout=15m "elasticsearch/${elasticsearch_name}"
  kubectl -n "${APP_NAMESPACE}" wait --for="jsonpath={.status.health}=green" --timeout=15m "kibana/${kibana_name}"
  kubectl -n "${APP_NAMESPACE}" wait --for="jsonpath={.status.health}=green" --timeout=15m "beat/${filebeat_name}"
  kubectl -n "${APP_NAMESPACE}" rollout status "deployment/${otel_name}" --timeout=180s
  kubectl -n "${APP_NAMESPACE}" rollout status "statefulset/${tempo_name}" --timeout=180s

  collector_config="$(kubectl -n "${APP_NAMESPACE}" get configmap "${otel_name}" -o jsonpath='{.data.collector\.yaml}')"
  collector_json="$(yq -o=json '.' <<<"${collector_config}")"
  jq -e '.receivers | keys == ["otlp"]' <<<"${collector_json}" >/dev/null || die "OTel Collector enabled a non-OTLP receiver"
  jq -e '.service.pipelines | keys == ["traces"]' <<<"${collector_json}" >/dev/null || die "OTel Collector enabled a non-trace pipeline"
  jq -e --arg cloudops "${APP_NAMESPACE}" --arg demo "${DEMO_NAMESPACE}" '
    .processors."k8sattributes/cloudops".filter.namespace == $cloudops and
    .processors."k8sattributes/demo".filter.namespace == $demo
  ' <<<"${collector_json}" >/dev/null || die "OTel k8sattributes namespace filters drifted"

  tempo_config="$(kubectl -n "${APP_NAMESPACE}" get configmap "${tempo_name}" -o jsonpath='{.data.tempo\.yaml}')"
  tempo_json="$(yq -o=json '.' <<<"${tempo_config}")"
  jq -e '.target == "all" and .storage.trace.backend == "local"' <<<"${tempo_json}" >/dev/null || die "Tempo is not the local target=all monolith"
  if grep -Eiq 'kafka|tempo-distributed' <<<"${tempo_config}"; then
    die "Tempo rendered a forbidden Kafka or distributed dependency"
  fi
  check_namespace_rbac

  if [[ -z "${secret_dir}" || ! -d "${secret_dir}" ]]; then
    secret_dir="$(mktemp -d "${TMPDIR:-/tmp}/cloudops-v3-check.XXXXXX")"
    chmod 700 "${secret_dir}"
  fi
  kubectl -n "${APP_NAMESPACE}" get secret "${elasticsearch_name}-es-http-certs-public" \
    -o go-template='{{ index .data "tls.crt" | base64decode }}' >"${secret_dir}/elasticsearch-ca.crt"
  elastic_password="$(kubectl -n "${APP_NAMESPACE}" get secret "${elasticsearch_name}-es-elastic-user" -o go-template='{{ index .data "elastic" | base64decode }}')"
  chmod 600 "${secret_dir}/elasticsearch-ca.crt"
  kubectl -n "${APP_NAMESPACE}" port-forward "svc/${elasticsearch_name}-es-http" 19200:9200 >"${ELASTICSEARCH_PORT_FORWARD_LOG}" 2>&1 &
  elasticsearch_port_forward_pid="$!"
  for _ in $(seq 1 60); do
    elastic_result="$(curl --fail --silent --cacert "${secret_dir}/elasticsearch-ca.crt" --user "elastic:${elastic_password}" \
      "https://127.0.0.1:19200/_cluster/health" 2>/dev/null || true)"
    if jq -e '.status == "green"' <<<"${elastic_result}" >/dev/null 2>&1; then break; fi
    sleep 2
  done
  jq -e '.status == "green"' <<<"${elastic_result}" >/dev/null || die "Elasticsearch query path is not healthy"
  elastic_result="$(curl --fail --silent --cacert "${secret_dir}/elasticsearch-ca.crt" --user "elastic:${elastic_password}" \
    "https://127.0.0.1:19200/_security/role/cloudops_reader")"
  jq -e '.cloudops_reader.cluster == ["monitor"] and (.cloudops_reader.indices | length == 1) and .cloudops_reader.indices[0].names == ["logs-cloudops-*"] and (.cloudops_reader.indices[0].privileges | sort) == ["read", "view_index_metadata"]' \
    <<<"${elastic_result}" >/dev/null || die "CloudOps Elasticsearch read role drifted"
  for _ in $(seq 1 60); do
    elastic_result="$(curl --fail --silent --cacert "${secret_dir}/elasticsearch-ca.crt" --user "elastic:${elastic_password}" \
      "https://127.0.0.1:19200/_data_stream/logs-cloudops-*" 2>/dev/null || true)"
    if jq -e '.data_streams | length > 0' <<<"${elastic_result}" >/dev/null 2>&1; then break; fi
    sleep 2
  done
  jq -e '.data_streams | length > 0' <<<"${elastic_result}" >/dev/null || die "Filebeat did not create the bounded CloudOps data stream"
  elastic_password=""

  kubectl -n "${APP_NAMESPACE}" port-forward "svc/${tempo_name}" 13200:3200 >"${TEMPO_PORT_FORWARD_LOG}" 2>&1 &
  tempo_port_forward_pid="$!"
  for _ in $(seq 1 60); do
    if curl --fail --silent http://127.0.0.1:13200/ready >/dev/null 2>&1; then break; fi
    sleep 2
  done
  curl --fail --silent http://127.0.0.1:13200/ready >/dev/null || die "Tempo query path is not healthy"
  for _ in $(seq 1 60); do
    trace_result="$(curl --fail --silent --get \
      --data-urlencode 'q={ resource.service.name = "cloudops-demo-workload" }' \
      --data-urlencode 'limit=20' \
      http://127.0.0.1:13200/api/search 2>/dev/null || true)"
    if jq -e '.traces | length > 0' <<<"${trace_result}" >/dev/null 2>&1; then break; fi
    sleep 2
  done
  jq -e '.traces | length > 0' <<<"${trace_result}" >/dev/null || die "Demo trace did not traverse OTel Collector into Tempo"
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
  check_platform_observability
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
  # Proxy variables pointing at host loopback are valid for host-side Docker
  # pulls but invalid inside the kind node. Do not copy them into the node;
  # preload_external_images imports the host-resolved pinned images instead.
  env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
    kind create cluster --name "${CLUSTER_NAME}" --config "${KIND_CONFIG}" --wait 120s
  preload_external_images
  create_namespaces
  create_secrets
  install_monitoring
  install_eck
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
