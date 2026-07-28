#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${CLOUDOPS_STATE_DIR:-${ROOT_DIR}/.cloudops}"
BACKUP_ROOT="${CLOUDOPS_BACKUP_DIR:-${STATE_DIR}/backups}"
BOOTSTRAP_SECRET_DIR="${STATE_DIR}/bootstrap-secrets"
SECRET_DIR="${STATE_DIR}/secrets"
RUNTIME_DIR="${STATE_DIR}/runtime"
CHART_DIR="${ROOT_DIR}/charts/cloudops"
VALUES_FILE="${CHART_DIR}/values-local.yaml"

CLUSTER_NAME="cloudops-local"
KUBE_CONTEXT="kind-cloudops-local"
NAMESPACE="cloudops-system"
PROVIDER_NAMESPACE="demo"
RELEASE_NAME="cloudops"
DATABASE_NAME="cloudops"
LOCAL_PORT="${CLOUDOPS_LOCAL_PORT:-18080}"
LOCAL_URL="http://127.0.0.1:${LOCAL_PORT}"
GRAFANA_LOCAL_PORT="${CLOUDOPS_GRAFANA_PORT:-18081}"
GRAFANA_LOCAL_URL="http://127.0.0.1:${GRAFANA_LOCAL_PORT}"
TEMPO_LOCAL_PORT="${CLOUDOPS_TEMPO_PORT:-18084}"
TEMPO_LOCAL_URL="http://127.0.0.1:${TEMPO_LOCAL_PORT}"
PORT_FORWARD_PID_FILE="${RUNTIME_DIR}/api-port-forward.pid"
PORT_FORWARD_LOG="${RUNTIME_DIR}/api-port-forward.log"
GRAFANA_PORT_FORWARD_PID_FILE="${RUNTIME_DIR}/grafana-port-forward.pid"
GRAFANA_PORT_FORWARD_LOG="${RUNTIME_DIR}/grafana-port-forward.log"
TEMPO_PORT_FORWARD_PID_FILE="${RUNTIME_DIR}/tempo-port-forward.pid"
TEMPO_PORT_FORWARD_LOG="${RUNTIME_DIR}/tempo-port-forward.log"
SCENARIO_ID_FILE="${RUNTIME_DIR}/scenario-id"
KIND_NODE_IMAGE="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"
MYSQL_IMAGE="mysql:8.0.46"
MYSQL_REPOSITORY="mysql"
MYSQL_PLATFORM="linux/amd64"
MYSQL_DIGEST="sha256:62fb722c78b24245ddff1796a0fcee4a49cc5b87e0aaaf20c92d1da9e0a2497b"
PROMETHEUS_IMAGE="quay.io/prometheus/prometheus:v3.13.1-distroless"
PROMETHEUS_REPOSITORY="quay.io/prometheus/prometheus"
PROMETHEUS_DIGEST="sha256:214f8427c8fba80c327bb94a75feb802ae12f2d6ca30812aa6e7d22f09bbea80"
PROMETHEUS_AMD64_DIGEST="sha256:335b5796a6e4355530475575253f84de20b8ad07bf899f65ed218451ce4c60b4"
PROMETHEUS_PRELOAD_IMAGE="cloudops-preload/prometheus-amd64:v3.13.1-distroless"
GRAFANA_IMAGE="grafana/grafana:12.3.1"
GRAFANA_REPOSITORY="grafana/grafana"
GRAFANA_DIGEST="sha256:2175aaa91c96733d86d31cf270d5310b278654b03f5718c59de12a865380a31f"
GRAFANA_AMD64_DIGEST="sha256:7c064e627d9cb50c3485c9ded5ca0222de89a08e41403322a0c3ca6f1777a8d1"
GRAFANA_PRELOAD_IMAGE="cloudops-preload/grafana-amd64:12.3.1"
ELASTICSEARCH_IMAGE="docker.elastic.co/elasticsearch/elasticsearch:9.4.3"
ELASTICSEARCH_REPOSITORY="docker.elastic.co/elasticsearch/elasticsearch"
ELASTICSEARCH_DIGEST="sha256:851ff5f9615ab7d2f00931114f6db32850f0208ec9fe7e841f135ac78f5f13d5"
ELASTICSEARCH_PRELOAD_IMAGE="cloudops-preload/elasticsearch-amd64:9.4.3"
FILEBEAT_IMAGE="docker.elastic.co/beats/filebeat:9.4.3"
FILEBEAT_REPOSITORY="docker.elastic.co/beats/filebeat"
FILEBEAT_DIGEST="sha256:61bf789ac881c88c0edee6d490434e7a6d464d5f46a2bb1f5540a2301d4bdec3"
FILEBEAT_PRELOAD_IMAGE="cloudops-preload/filebeat-amd64:9.4.3"
TEMPO_IMAGE="grafana/tempo:3.0.2"
TEMPO_REPOSITORY="grafana/tempo"
TEMPO_DIGEST="sha256:cda87c212d8c584dc0b89e337e7ed648a5100feb657e5d528480ee4fa03dbbe3"
TEMPO_PRELOAD_IMAGE="cloudops-preload/tempo-amd64:3.0.2"
OTEL_COLLECTOR_IMAGE="otel/opentelemetry-collector-contrib:0.156.0"
OTEL_COLLECTOR_REPOSITORY="otel/opentelemetry-collector-contrib"
OTEL_COLLECTOR_DIGEST="sha256:125bdbeb7590cc1952c5b3430ecf14063568980c2c93d5b38676cc0446ed8108"
OTEL_COLLECTOR_PRELOAD_IMAGE="cloudops-preload/otel-collector-contrib-amd64:0.156.0"
ALERTMANAGER_IMAGE="quay.io/prometheus/alertmanager:v0.33.1"
ALERTMANAGER_REPOSITORY="quay.io/prometheus/alertmanager"
ALERTMANAGER_DIGEST="sha256:9e082985f56f4c8c9f724e18f2288c6708f472e56a5286b8863d080434ea065d"
ALERTMANAGER_PRELOAD_IMAGE="cloudops-preload/alertmanager-amd64:v0.33.1"
MIN_INOTIFY_INSTANCES=512
BACKUP_FORMAT_VERSION=2
BACKUP_CONTRACT="cloudops-semantic"
RESTORE_STAGING_DATABASE="cloudops_restore_staging"
LATEST_SCHEMA_VERSION=11
DATA_CLAIM="cloudops-data"
DATA_MOUNT_PATH="/var/lib/cloudops"
DATA_DIRECTORY="${DATA_MOUNT_PATH}/data"
DATA_TRANSFER_POD="cloudops-data-transfer"
DATA_TRANSFER_ACTIVE=0

RESTORE_CLEANUP_POD=""
RESTORE_CLEANUP_STAGING=0
RESTORE_RECOVER_RUNTIME=0
RESTORE_API_REPLICAS=0
RESTORE_WORKER_REPLICAS=0

die() {
  if [[ "${DATA_TRANSFER_ACTIVE:-0}" == "1" ]] && context_exists; then
    kube -n "${NAMESPACE}" delete pod "${DATA_TRANSFER_POD}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    DATA_TRANSFER_ACTIVE=0
  fi
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'PASS: %s\n' "$*"
}

note() {
  printf 'INFO: %s\n' "$*"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

validate_identifier() {
  [[ "$1" =~ ^[a-zA-Z][a-zA-Z0-9_]{0,63}$ ]] || die "invalid MySQL identifier: $1"
}

validate_local_port() {
  if [[ ! "${LOCAL_PORT}" =~ ^[0-9]+$ ]] || ((LOCAL_PORT < 1024 || LOCAL_PORT > 65535)); then
    die "CLOUDOPS_LOCAL_PORT must be an unprivileged TCP port"
  fi
  if [[ ! "${GRAFANA_LOCAL_PORT}" =~ ^[0-9]+$ ]] || ((GRAFANA_LOCAL_PORT < 1024 || GRAFANA_LOCAL_PORT > 65535)); then
    die "CLOUDOPS_GRAFANA_PORT must be an unprivileged TCP port"
  fi
  if [[ ! "${TEMPO_LOCAL_PORT}" =~ ^[0-9]+$ ]] || ((TEMPO_LOCAL_PORT < 1024 || TEMPO_LOCAL_PORT > 65535)); then
    die "CLOUDOPS_TEMPO_PORT must be an unprivileged TCP port"
  fi
  [[ "${LOCAL_PORT}" != "${GRAFANA_LOCAL_PORT}" &&
     "${LOCAL_PORT}" != "${TEMPO_LOCAL_PORT}" &&
     "${GRAFANA_LOCAL_PORT}" != "${TEMPO_LOCAL_PORT}" ]] ||
    die "CloudOps, Grafana, and Tempo loopback ports must be different"
}

validate_fixed_boundaries() {
  [[ "${CLOUDOPS_CLUSTER_NAME:-${CLUSTER_NAME}}" == "${CLUSTER_NAME}" ]] ||
    die "CLOUDOPS_CLUSTER_NAME cannot override the fixed cloudops-local boundary"
  [[ "${CLOUDOPS_KUBE_CONTEXT:-${KUBE_CONTEXT}}" == "${KUBE_CONTEXT}" ]] ||
    die "CLOUDOPS_KUBE_CONTEXT cannot override the fixed kind-cloudops-local boundary"
  [[ "${CLOUDOPS_NAMESPACE:-${NAMESPACE}}" == "${NAMESPACE}" ]] ||
    die "CLOUDOPS_NAMESPACE cannot override the fixed cloudops-system boundary"
  [[ "${CLOUDOPS_RELEASE_NAME:-${RELEASE_NAME}}" == "${RELEASE_NAME}" ]] ||
    die "CLOUDOPS_RELEASE_NAME cannot override the fixed cloudops release"
}

validate_state_directory() {
  local resolved
  resolved="$(realpath -m "${STATE_DIR}")"
  [[ "${resolved}" != "/" && "${resolved}" != "${HOME}" && "${resolved}" != "${ROOT_DIR}" ]] ||
    die "refusing broad state directory: ${resolved}"
}

ensure_private_directories() {
  validate_state_directory
  umask 077
  mkdir -p \
    "${STATE_DIR}" "${BACKUP_ROOT}" "${BOOTSTRAP_SECRET_DIR}" \
    "${SECRET_DIR}" "${RUNTIME_DIR}"
  chmod 700 \
    "${STATE_DIR}" "${BACKUP_ROOT}" "${BOOTSTRAP_SECRET_DIR}" \
    "${SECRET_DIR}" "${RUNTIME_DIR}"
}

kube() {
  kubectl --context "${KUBE_CONTEXT}" "$@"
}

cluster_exists() {
  kind get clusters 2>/dev/null | rg -Fxq "${CLUSTER_NAME}"
}

context_exists() {
  kubectl config get-contexts -o name 2>/dev/null | rg -Fxq "${KUBE_CONTEXT}"
}

release_exists() {
  context_exists && helm --kube-context "${KUBE_CONTEXT}" -n "${NAMESPACE}" status "${RELEASE_NAME}" >/dev/null 2>&1
}

release_status() {
  release_exists || return 1
  helm --kube-context "${KUBE_CONTEXT}" -n "${NAMESPACE}" status "${RELEASE_NAME}" -o json |
    jq -r '.info.status'
}

mysql_pod_if_running() {
  local pod
  pod="$(kube -n "${NAMESPACE}" get pods \
    -l app.kubernetes.io/component=database,app.kubernetes.io/instance="${RELEASE_NAME}" \
    --field-selector=status.phase=Running \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "${pod}" ]] && printf '%s\n' "${pod}"
}

mysql_pod() {
  local pod
  pod="$(mysql_pod_if_running || true)"
  [[ -n "${pod}" ]] || die "running MySQL Pod not found in ${KUBE_CONTEXT}/${NAMESPACE}"
  printf '%s\n' "${pod}"
}

mysql_exec() {
  local pod="$1"
  shift
  # Expansion belongs to the shell inside the MySQL Pod.
  # shellcheck disable=SC2016
  kube -n "${NAMESPACE}" exec "${pod}" -- sh -ec \
    'export MYSQL_PWD="${MYSQL_ROOT_PASSWORD:?MYSQL_ROOT_PASSWORD is required}"; exec mysql --protocol=socket --batch --skip-column-names -uroot "$@"' \
    cloudops-mysql "$@"
}

mysql_import() {
  local pod="$1" database="$2" input="$3"
  # Expansion belongs to the shell inside the MySQL Pod.
  # shellcheck disable=SC2016
  kube -n "${NAMESPACE}" exec -i "${pod}" -- sh -ec \
    'export MYSQL_PWD="${MYSQL_ROOT_PASSWORD:?MYSQL_ROOT_PASSWORD is required}"; exec mysql --protocol=socket -uroot "$1"' \
    cloudops-restore "${database}" <"${input}"
}

database_exists() {
  local pod="$1" database="$2" count
  count="$(mysql_exec "${pod}" -e "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='${database}'")"
  [[ "${count}" == "1" ]]
}

schema_version() {
  local pod="$1" database="$2" has_table
  has_table="$(mysql_exec "${pod}" -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='${database}' AND table_name='goose_db_version'")"
  if [[ "${has_table}" != "1" ]]; then
    printf '0\n'
    return
  fi
  mysql_exec "${pod}" "${database}" -e 'SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied=1'
}

write_row_counts() {
  local pod="$1" database="$2" output="$3" table count
  : >"${output}"
  while IFS= read -r table; do
    [[ -n "${table}" ]] || continue
    count="$(mysql_exec "${pod}" "${database}" -e "SELECT COUNT(*) FROM \`${table}\`")"
    printf '%s\t%s\n' "${table}" "${count}" >>"${output}"
  done < <(mysql_exec "${pod}" -e "SELECT table_name FROM information_schema.tables WHERE table_schema='${database}' ORDER BY table_name")
}

preflight() {
  local available_mib failed=0
  validate_fixed_boundaries
  validate_local_port
  validate_state_directory
  for command_name in docker kind kubectl helm jq openssl sha256sum realpath git curl rg; do
    if command -v "${command_name}" >/dev/null 2>&1; then
      printf 'PASS prerequisite %s\n' "${command_name}"
    else
      printf 'FAIL prerequisite %s missing\n' "${command_name}" >&2
      failed=1
    fi
  done
  [[ "${failed}" == "0" ]] || die "required local tools are missing"
  docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
  [[ -f "${CHART_DIR}/Chart.yaml" && -f "${VALUES_FILE}" ]] || die "canonical CloudOps Chart is incomplete"
  available_mib="$(awk '/MemAvailable:/ {print int($2/1024)}' /proc/meminfo)"
  [[ "${available_mib}" -ge 2048 ]] || die "at least 2048 MiB available memory is required (found ${available_mib} MiB)"
  [[ "$(nproc)" -ge 2 ]] || die "at least two CPU cores are required"
  if cluster_exists && ! context_exists; then
    die "kind cluster ${CLUSTER_NAME} exists but context ${KUBE_CONTEXT} is unavailable"
  fi
  pass "local preflight cluster=${CLUSTER_NAME} available_memory_mib=${available_mib} cpu=$(nproc)"
}

ensure_cluster() {
  local node current_inotify
  if cluster_exists; then
    note "reusing kind cluster ${CLUSTER_NAME}"
  else
    note "creating kind cluster ${CLUSTER_NAME} with pinned node image"
    kind create cluster --name "${CLUSTER_NAME}" --image "${KIND_NODE_IMAGE}" --wait 120s
  fi
  context_exists || die "kind did not create ${KUBE_CONTEXT}"
  kube cluster-info >/dev/null
  node="${CLUSTER_NAME}-control-plane"
  current_inotify="$(docker exec "${node}" cat /proc/sys/fs/inotify/max_user_instances)"
  [[ "${current_inotify}" =~ ^[0-9]+$ ]] || die "could not read kind inotify capacity"
  if ((current_inotify < MIN_INOTIFY_INSTANCES)); then
    note "raising shared inotify instance capacity for kind system controllers"
    docker exec "${node}" sysctl -w "fs.inotify.max_user_instances=${MIN_INOTIFY_INSTANCES}" >/dev/null ||
      die "kind requires fs.inotify.max_user_instances>=${MIN_INOTIFY_INSTANCES}"
  fi
  kube -n kube-system rollout status daemonset/kube-proxy --timeout=2m
  kube -n local-path-storage rollout status deployment/local-path-provisioner --timeout=2m
}

ensure_namespaces() {
  local namespace
  for namespace in "${NAMESPACE}" "${PROVIDER_NAMESPACE}"; do
    kube create namespace "${namespace}" --dry-run=client -o yaml | kube apply -f - >/dev/null
    kube label namespace "${namespace}" cloudops.io/managed-by=cloudops-local --overwrite >/dev/null
  done
}

decode_cluster_secret() {
  local secret="$1" key="$2" output="$3" encoded
  context_exists || return 1
  encoded="$(kube -n "${NAMESPACE}" get secret "${secret}" -o "jsonpath={.data.${key}}" 2>/dev/null || true)"
  [[ -n "${encoded}" ]] || return 1
  printf '%s' "${encoded}" | base64 --decode >"${output}"
  [[ -s "${output}" ]]
}

ensure_secret_file() {
  local filename="$1" secret="$2" key="$3" path tmp
  path="${BOOTSTRAP_SECRET_DIR}/${filename}"
  if [[ -s "${path}" ]]; then
    chmod 600 "${path}"
    return
  fi
  tmp="$(mktemp "${BOOTSTRAP_SECRET_DIR}/.${filename}.XXXXXX")"
  if decode_cluster_secret "${secret}" "${key}" "${tmp}"; then
    note "recovered local secret reference ${filename} from ${KUBE_CONTEXT}"
  else
    openssl rand -hex 32 | tr -d '\n' >"${tmp}"
  fi
  chmod 600 "${tmp}"
  mv "${tmp}" "${path}"
}

apply_secret_file() {
  local secret="$1" key="$2" path="$3"
  kube -n "${NAMESPACE}" create secret generic "${secret}" \
    --from-file="${key}=${path}" --dry-run=client -o yaml |
    kube apply -f - >/dev/null
  kube -n "${NAMESPACE}" label secret "${secret}" \
    app.kubernetes.io/instance="${RELEASE_NAME}" cloudops.io/managed-by=cloudops-local --overwrite >/dev/null
}

ensure_runtime_secrets() {
  ensure_private_directories
  ensure_secret_file mysql-root-password cloudops-mysql-root MYSQL_ROOT_PASSWORD
  ensure_secret_file mysql-api-password cloudops-api-database MYSQL_PASSWORD
  ensure_secret_file mysql-worker-password cloudops-worker-database MYSQL_PASSWORD
  ensure_secret_file mysql-migrate-password cloudops-migrate-database MYSQL_PASSWORD
  ensure_secret_file webhook-token cloudops-alertmanager-webhook token

  apply_secret_file cloudops-mysql-root MYSQL_ROOT_PASSWORD "${BOOTSTRAP_SECRET_DIR}/mysql-root-password"
  apply_secret_file cloudops-api-database MYSQL_PASSWORD "${BOOTSTRAP_SECRET_DIR}/mysql-api-password"
  apply_secret_file cloudops-worker-database MYSQL_PASSWORD "${BOOTSTRAP_SECRET_DIR}/mysql-worker-password"
  apply_secret_file cloudops-migrate-database MYSQL_PASSWORD "${BOOTSTRAP_SECRET_DIR}/mysql-migrate-password"
  apply_secret_file cloudops-alertmanager-webhook token "${BOOTSTRAP_SECRET_DIR}/webhook-token"
}

verify_mysql_image() {
  local image_id platform
  if ! docker image inspect "${MYSQL_REPOSITORY}@${MYSQL_DIGEST}" >/dev/null 2>&1; then
    docker pull --platform "${MYSQL_PLATFORM}" "${MYSQL_REPOSITORY}@${MYSQL_DIGEST}" >/dev/null
  fi
  docker tag "${MYSQL_REPOSITORY}@${MYSQL_DIGEST}" "${MYSQL_IMAGE}"
  image_id="$(docker image inspect "${MYSQL_IMAGE}" --format '{{.Id}}')"
  platform="$(docker image inspect "${MYSQL_IMAGE}" --format '{{.Os}}/{{.Architecture}}')"
  [[ "${image_id}" == "${MYSQL_DIGEST}" ]] ||
    die "local ${MYSQL_IMAGE} does not match the pinned platform digest"
  [[ "${platform}" == "${MYSQL_PLATFORM}" ]] ||
    die "local ${MYSQL_IMAGE} platform=${platform}; expected ${MYSQL_PLATFORM}"
}

verify_observability_image() {
  local image="$1" repository="$2" digest="$3" platform_digest="$4" preload_image="$5" image_id platform
  if ! docker image inspect "${image}" >/dev/null 2>&1; then
    docker pull --platform "${MYSQL_PLATFORM}" "${repository}@${digest}" >/dev/null
    docker tag "${repository}@${digest}" "${image}"
  fi
  image_id="$(docker image inspect "${image}" --format '{{.Id}}')"
  platform="$(docker image inspect "${image}" --format '{{.Os}}/{{.Architecture}}')"
  [[ "${image_id}" == "${digest}" ]] ||
    die "local ${image} does not match pinned image digest ${digest}"
  docker pull --platform "${MYSQL_PLATFORM}" "${repository}@${platform_digest}" >/dev/null
  docker tag "${repository}@${platform_digest}" "${preload_image}"
  image_id="$(docker image inspect "${preload_image}" --format '{{.Id}}')"
  [[ "${platform}" == "${MYSQL_PLATFORM}" ]] ||
    die "local ${image} platform=${platform}; expected ${MYSQL_PLATFORM}"
  [[ "${image_id}" == "${platform_digest}" ]] ||
    die "local ${preload_image} does not match pinned platform digest ${platform_digest}"
}

verify_observability_images() {
  verify_observability_image \
    "${PROMETHEUS_IMAGE}" "${PROMETHEUS_REPOSITORY}" "${PROMETHEUS_DIGEST}" \
    "${PROMETHEUS_AMD64_DIGEST}" "${PROMETHEUS_PRELOAD_IMAGE}"
  verify_observability_image \
    "${GRAFANA_IMAGE}" "${GRAFANA_REPOSITORY}" "${GRAFANA_DIGEST}" \
    "${GRAFANA_AMD64_DIGEST}" "${GRAFANA_PRELOAD_IMAGE}"
  verify_pinned_provider_image \
    "${ELASTICSEARCH_IMAGE}" "${ELASTICSEARCH_REPOSITORY}" "${ELASTICSEARCH_DIGEST}" "${ELASTICSEARCH_PRELOAD_IMAGE}"
  verify_pinned_provider_image \
    "${FILEBEAT_IMAGE}" "${FILEBEAT_REPOSITORY}" "${FILEBEAT_DIGEST}" "${FILEBEAT_PRELOAD_IMAGE}"
  verify_pinned_provider_image \
    "${TEMPO_IMAGE}" "${TEMPO_REPOSITORY}" "${TEMPO_DIGEST}" "${TEMPO_PRELOAD_IMAGE}"
  verify_pinned_provider_image \
    "${OTEL_COLLECTOR_IMAGE}" "${OTEL_COLLECTOR_REPOSITORY}" "${OTEL_COLLECTOR_DIGEST}" "${OTEL_COLLECTOR_PRELOAD_IMAGE}"
  verify_pinned_provider_image \
    "${ALERTMANAGER_IMAGE}" "${ALERTMANAGER_REPOSITORY}" "${ALERTMANAGER_DIGEST}" "${ALERTMANAGER_PRELOAD_IMAGE}"
}

verify_pinned_provider_image() {
  local image="$1" repository="$2" digest="$3" preload_image="$4" image_id platform
  if ! docker image inspect "${repository}@${digest}" >/dev/null 2>&1; then
    docker pull --platform "${MYSQL_PLATFORM}" "${repository}@${digest}" >/dev/null
  fi
  docker tag "${repository}@${digest}" "${image}"
  docker tag "${repository}@${digest}" "${preload_image}"
  image_id="$(docker image inspect "${preload_image}" --format '{{.Id}}')"
  platform="$(docker image inspect "${preload_image}" --format '{{.Os}}/{{.Architecture}}')"
  [[ "${image_id}" == "${digest}" ]] ||
    die "local ${preload_image} does not match pinned image digest ${digest}"
  [[ "${platform}" == "${MYSQL_PLATFORM}" ]] ||
    die "local ${preload_image} platform=${platform}; expected ${MYSQL_PLATFORM}"
}

load_observability_image() {
  local preload_image="$1" runtime_image="$2" node source_ref target_ref
  source_ref="docker.io/${preload_image}"
  target_ref="${runtime_image}"
  [[ "${target_ref}" == */*/* ]] || target_ref="docker.io/${target_ref}"
  while IFS= read -r node; do
    note "loading ${runtime_image} into ${node} for ${MYSQL_PLATFORM}"
    docker save "${preload_image}" |
      docker exec --privileged -i "${node}" ctr --namespace=k8s.io images import \
        --platform "${MYSQL_PLATFORM}" --snapshotter=overlayfs - >/dev/null
    docker exec "${node}" ctr --namespace=k8s.io images tag --force "${source_ref}" "${target_ref}" >/dev/null
    [[ "$(docker exec "${node}" ctr --namespace=k8s.io images list --quiet "name==${target_ref}")" == "${target_ref}" ]] ||
      die "runtime image ${target_ref} was not loaded into ${node}"
  done < <(kind get nodes --name "${CLUSTER_NAME}")
}

build_application_images() {
  local exact_sha go_proxy source_url target
  exact_sha="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
  go_proxy="${CLOUDOPS_GO_PROXY:-https://goproxy.cn,direct}"
  [[ "${go_proxy}" =~ ^[A-Za-z0-9:/.,_|-]+$ && "${go_proxy}" != *"@"* ]] ||
    die "CLOUDOPS_GO_PROXY must be a credential-free Go proxy list"
  source_url="$(git -C "${ROOT_DIR}" remote get-url origin 2>/dev/null || printf 'local')"
  for target in api worker migrate demo; do
    note "building cloudops-${target}:local"
    docker build \
      --network host \
      --build-arg "GO_PROXY=${go_proxy}" \
      --build-arg "VCS_REF=${exact_sha}" \
      --build-arg "VCS_SOURCE=${source_url}" \
      --build-arg "VERSION=local" \
      --target "cloudops-${target}" \
      --tag "cloudops-${target}:local" \
      "${ROOT_DIR}"
  done
}

load_runtime_images() {
  local image
  verify_mysql_image
  verify_observability_images
  for image in \
    cloudops-api:local cloudops-worker:local cloudops-migrate:local cloudops-demo:local "${MYSQL_IMAGE}"; do
    kind load docker-image "${image}" --name "${CLUSTER_NAME}"
  done
  load_observability_image "${PROMETHEUS_PRELOAD_IMAGE}" "${PROMETHEUS_IMAGE}"
  load_observability_image "${GRAFANA_PRELOAD_IMAGE}" "${GRAFANA_IMAGE}"
  load_observability_image "${ELASTICSEARCH_PRELOAD_IMAGE}" "${ELASTICSEARCH_IMAGE}"
  load_observability_image "${FILEBEAT_PRELOAD_IMAGE}" "${FILEBEAT_IMAGE}"
  load_observability_image "${TEMPO_PRELOAD_IMAGE}" "${TEMPO_IMAGE}"
  load_observability_image "${OTEL_COLLECTOR_PRELOAD_IMAGE}" "${OTEL_COLLECTOR_IMAGE}"
  load_observability_image "${ALERTMANAGER_PRELOAD_IMAGE}" "${ALERTMANAGER_IMAGE}"
}

reconcile_mysql_identities() {
  local pod
  kube -n "${NAMESPACE}" scale statefulset/mysql --replicas=1 >/dev/null
  kube -n "${NAMESPACE}" rollout status statefulset/mysql --timeout=5m
  pod="$(mysql_pod)"
  mysql_exec "${pod}" -e "CREATE DATABASE IF NOT EXISTS \`${DATABASE_NAME}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
  kube -n "${NAMESPACE}" exec "${pod}" -- bash /docker-entrypoint-initdb.d/10-cloudops-identities.sh
  pass "MySQL database and workload identities reconciled"
}

install_runtime() {
  local active_scenario_id="${1:-}" helm_args current_status
  helm_args=(
    --kube-context "${KUBE_CONTEXT}"
    --namespace "${NAMESPACE}"
    --values "${VALUES_FILE}"
    --timeout 12m
  )
  if [[ -n "${active_scenario_id}" ]]; then
    valid_scenario_id "${active_scenario_id}" || die "active release has an invalid Scenario identity"
    helm_args+=(
      --set scenario.enabled=true
      --set-string "scenario.id=${active_scenario_id}"
      --set-string worker.env.K8S_WRITE_ENABLED=true
    )
  else
    helm_args+=(
      --set scenario.enabled=false
      --set-string worker.env.K8S_WRITE_ENABLED=false
    )
  fi
  current_status="$(release_status 2>/dev/null || true)"
  if [[ -n "${active_scenario_id}" && "${current_status}" != "deployed" ]]; then
    die "refusing to replace active Scenario ${active_scenario_id} while release status is ${current_status:-unavailable}"
  fi
  if [[ "${current_status}" != "deployed" ]]; then
    note "bootstrapping persistent MySQL in the canonical release"
    helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" "${helm_args[@]}" \
      --set bootstrapOnly=true \
      --set api.enabled=false \
      --set worker.enabled=false \
      --set migrate.enabled=false \
      --wait
    kube -n "${NAMESPACE}" rollout status statefulset/mysql --timeout=5m
  fi

  reconcile_mysql_identities

  # Recreate existing local-tag Pods before Helm waits. A migration can make the
  # previous binaries unready, so deferring this restart until after --wait
  # creates a schema/readiness deadlock on upgrades.
  if kube -n "${NAMESPACE}" get deployment/cloudops-api deployment/cloudops-worker >/dev/null 2>&1; then
    note "restarting API and Worker to consume freshly loaded local images"
    kube -n "${NAMESPACE}" rollout restart deployment/cloudops-api deployment/cloudops-worker >/dev/null
  fi

  note "reconciling API, Worker, Migrate, MySQL, and bounded telemetry Providers"
  helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" "${helm_args[@]}" \
    --wait --wait-for-jobs
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-api --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/prometheus --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/alertmanager --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/grafana --timeout=5m
  kube -n "${NAMESPACE}" rollout status statefulset/elasticsearch --timeout=8m
  kube -n "${NAMESPACE}" rollout status daemonset/filebeat --timeout=5m
  kube -n "${NAMESPACE}" rollout status statefulset/tempo --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/otel-collector --timeout=5m
}

valid_scenario_id() {
  [[ "$1" =~ ^scenario-[a-z0-9]([-a-z0-9]*[a-z0-9])?$ && ${#1} -le 63 ]]
}

scenario_id_from_file() {
  local scenario_id
  [[ -f "${SCENARIO_ID_FILE}" && ! -L "${SCENARIO_ID_FILE}" ]] || return 1
  [[ "$(stat -c '%a' "${SCENARIO_ID_FILE}")" == "600" ]] || return 1
  scenario_id="$(tr -d '[:space:]' <"${SCENARIO_ID_FILE}")"
  valid_scenario_id "${scenario_id}" || return 1
  printf '%s\n' "${scenario_id}"
}

write_scenario_id() {
  local scenario_id="$1" staged
  valid_scenario_id "${scenario_id}" || die "invalid generated Scenario identity"
  ensure_private_directories
  staged="$(mktemp "${RUNTIME_DIR}/.scenario-id.XXXXXX")"
  printf '%s\n' "${scenario_id}" >"${staged}"
  chmod 600 "${staged}"
  mv "${staged}" "${SCENARIO_ID_FILE}"
}

release_scenario_json() {
  release_exists || return 1
  helm --kube-context "${KUBE_CONTEXT}" -n "${NAMESPACE}" get values "${RELEASE_NAME}" -a -o json
}

release_scenario_id() {
  release_scenario_json 2>/dev/null | jq -r 'select(.scenario.enabled == true) | .scenario.id // empty'
}

release_scenario_active() {
  [[ "$(release_scenario_json 2>/dev/null | jq -r '.scenario.enabled // false' || true)" == "true" ]]
}

urlencode() {
  jq -rn --arg value "$1" '$value | @uri'
}

service_proxy_get() {
  local namespace="$1" service="$2" port="$3" path="$4"
  kube get --raw "/api/v1/namespaces/${namespace}/services/http:${service}:${port}/proxy${path}"
}

scenario_metric_samples() {
  local scenario_id="$1" query encoded response value
  query="count(cloudops_demo_workload_ready{scenario_id=\"${scenario_id}\"})"
  encoded="$(urlencode "${query}")"
  response="$(service_proxy_get "${NAMESPACE}" prometheus 9090 "/api/v1/query?query=${encoded}" 2>/dev/null || true)"
  value="$(jq -r '.data.result[0].value[1] // "0"' <<<"${response}" 2>/dev/null || true)"
  [[ "${value}" =~ ^[0-9]+([.][0-9]+)?$ ]] || value=0
  awk -v value="${value}" 'BEGIN {printf "%d\n", value}'
}

scenario_firing_alerts() {
  local scenario_id="$1" response value
  response="$(service_proxy_get "${NAMESPACE}" alertmanager 9093 '/api/v2/alerts?active=true&silenced=false&inhibited=false' 2>/dev/null || true)"
  value="$(jq -r --arg scenario_id "${scenario_id}" '[.[]? | select(.labels.scenario_id == $scenario_id and .labels.alertname == "CloudOpsScenarioRequiredEnvMissing")] | length' \
    <<<"${response}" 2>/dev/null || true)"
  [[ "${value}" =~ ^[0-9]+$ ]] || value=0
  printf '%s\n' "${value}"
}

scenario_firing_alerts_strict() {
  local scenario_id="$1" response value
  response="$(service_proxy_get "${NAMESPACE}" alertmanager 9093 '/api/v2/alerts?active=true&silenced=false&inhibited=false')" || return 1
  jq -e 'type == "array"' <<<"${response}" >/dev/null 2>&1 || return 1
  value="$(jq -r --arg scenario_id "${scenario_id}" '[.[]? | select(.labels.scenario_id == $scenario_id and .labels.alertname == "CloudOpsScenarioRequiredEnvMissing")] | length' \
    <<<"${response}")" || return 1
  [[ "${value}" =~ ^[0-9]+$ ]] || return 1
  printf '%s\n' "${value}"
}

scenario_log_documents() {
  local scenario_id="$1" query encoded response value
  query="scenario_id:\"${scenario_id}\""
  encoded="$(urlencode "${query}")"
  response="$(service_proxy_get "${NAMESPACE}" elasticsearch 9200 "/logs-cloudops/_count?q=${encoded}" 2>/dev/null || true)"
  value="$(jq -r '.count // 0' <<<"${response}" 2>/dev/null || true)"
  [[ "${value}" =~ ^[0-9]+$ ]] || value=0
  printf '%s\n' "${value}"
}

scenario_traces() {
  local scenario_id="$1" query encoded response value
  query="{ resource.cloudops.scenario.id = \"${scenario_id}\" }"
  encoded="$(urlencode "${query}")"
  response="$(service_proxy_get "${NAMESPACE}" tempo 3200 "/api/search?q=${encoded}&limit=20" 2>/dev/null || true)"
  value="$(jq -r '(.traces // []) | length' <<<"${response}" 2>/dev/null || true)"
  [[ "${value}" =~ ^[0-9]+$ ]] || value=0
  printf '%s\n' "${value}"
}

scenario_history_count() {
  local scenario_id="$1" pod
  valid_scenario_id "${scenario_id}" || return 1
  pod="$(mysql_pod_if_running || true)"
  [[ -n "${pod}" ]] || return 1
  mysql_exec "${pod}" "${DATABASE_NAME}" -e "SELECT
    (SELECT COUNT(*) FROM alerts WHERE JSON_UNQUOTE(JSON_EXTRACT(labels_json,'$.scenario_id'))='${scenario_id}') +
    (SELECT COUNT(*) FROM context_snapshots WHERE JSON_UNQUOTE(JSON_EXTRACT(filters_json,'$.scenario_id'))='${scenario_id}') +
    (SELECT COUNT(*) FROM agent_operation_plans WHERE JSON_UNQUOTE(JSON_EXTRACT(target_json,'$.scenario_id'))='${scenario_id}')"
}

scenario_persisted_firing_alerts() {
  local scenario_id="$1" pod
  valid_scenario_id "${scenario_id}" || return 1
  pod="$(mysql_pod_if_running)" || return 1
  mysql_exec "${pod}" "${DATABASE_NAME}" -e "SELECT COUNT(*) FROM alerts
    WHERE status='firing'
      AND JSON_UNQUOTE(JSON_EXTRACT(labels_json,'$.scenario_id'))='${scenario_id}'"
}

scenario_stale_firing_alerts() {
  local pod
  pod="$(mysql_pod_if_running)" || return 1
  mysql_exec "${pod}" "${DATABASE_NAME}" -e "SELECT COUNT(*) FROM alerts
    WHERE status='firing' AND target_name LIKE 'cloudops-scenario-%'"
}

scenario_agent_runs() {
  local scenario_id="$1" pod
  valid_scenario_id "${scenario_id}" || return 1
  pod="$(mysql_pod_if_running || true)"
  [[ -n "${pod}" ]] || return 1
  mysql_exec "${pod}" "${DATABASE_NAME}" -e "SELECT COUNT(*)
    FROM agent_runs AS run
    JOIN context_snapshots AS snapshot ON snapshot.id=run.context_snapshot_id
    WHERE JSON_UNQUOTE(JSON_EXTRACT(snapshot.filters_json,'$.scenario_id'))='${scenario_id}'"
}

scenario_fault_replicas() {
  kube -n "${PROVIDER_NAMESPACE}" get deployment cloudops-scenario-fault \
    -o jsonpath='{.spec.replicas}' 2>/dev/null || true
}

scenario_fault_running_pods() {
  kube -n "${PROVIDER_NAMESPACE}" get pods \
    -l 'app.kubernetes.io/name=cloudops-scenario-fault' -o json 2>/dev/null |
    jq -r '[.items[]? | select(.status.phase == "Running")] | length' 2>/dev/null || printf '0\n'
}

inject_scenario_fault() {
  local desired ready generation observed running
  [[ "$(scenario_fault_replicas)" == "1" ]] || return
  note "injecting bounded Scenario fault by removing REQUIRED_ENV from demo/cloudops-scenario-fault"
  kube -n "${PROVIDER_NAMESPACE}" set env deployment/cloudops-scenario-fault REQUIRED_ENV- >/dev/null
  for attempt in {1..120}; do
    desired="$(scenario_fault_replicas)"
    ready="$(kube -n "${PROVIDER_NAMESPACE}" get deployment cloudops-scenario-fault -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
    generation="$(kube -n "${PROVIDER_NAMESPACE}" get deployment cloudops-scenario-fault -o jsonpath='{.metadata.generation}' 2>/dev/null || true)"
    observed="$(kube -n "${PROVIDER_NAMESPACE}" get deployment cloudops-scenario-fault -o jsonpath='{.status.observedGeneration}' 2>/dev/null || true)"
    running="$(scenario_fault_running_pods)"
    if [[ "${desired}" == "1" && ("${ready}" == "" || "${ready}" == "0") && "${generation}" =~ ^[0-9]+$ && "${observed}" =~ ^[0-9]+$ ]] &&
      ((observed >= generation && running >= 1)); then
      pass "Scenario Kubernetes fault active desired=1 ready=0 running_pods=${running}"
      return
    fi
    if ((attempt % 10 == 0)); then
      note "waiting for Scenario Kubernetes degradation desired=${desired:-unavailable} ready=${ready:-0} running_pods=${running}"
    fi
    sleep 1
  done
  die "Scenario Kubernetes fault did not become observable"
}

recover_scenario_fault_for_shutdown() {
  local desired running
  kube -n "${PROVIDER_NAMESPACE}" get deployment cloudops-scenario-fault >/dev/null 2>&1 ||
    die "Scenario fault Deployment is unavailable before Scenario shutdown"
  desired="$(scenario_fault_replicas)"
  if [[ "${desired}" != "0" ]]; then
    note "ending the bounded Scenario fault before runtime removal"
    kube -n "${PROVIDER_NAMESPACE}" scale deployment/cloudops-scenario-fault --replicas=0 >/dev/null
  fi
  for attempt in {1..120}; do
    desired="$(scenario_fault_replicas)"
    running="$(scenario_fault_running_pods)"
    if [[ "${desired}" == "0" && "${running}" == "0" ]]; then
      pass "Scenario Kubernetes fault ended desired=0 running_pods=0"
      return
    fi
    if ((attempt % 10 == 0)); then
      note "waiting for Scenario fault shutdown desired=${desired:-unavailable} running_pods=${running:-unavailable}"
    fi
    sleep 1
  done
  die "Scenario Kubernetes fault did not end before runtime removal"
}

restore_active_scenario_fault_state() {
  local scenario_id="$1" expected_replicas="$2"
  valid_scenario_id "${scenario_id}" || die "invalid active Scenario identity"
  case "${expected_replicas}" in
    1)
      inject_scenario_fault
      wait_for_scenario_evidence "${scenario_id}"
      ;;
    0)
      note "restoring the recovered Scenario fault state after runtime reconciliation"
      kube -n "${PROVIDER_NAMESPACE}" scale deployment/cloudops-scenario-fault --replicas=0 >/dev/null
      for attempt in {1..120}; do
        if [[ "$(scenario_fault_replicas)" == "0" && "$(scenario_fault_running_pods)" == "0" ]]; then
          break
        fi
        if [[ "${attempt}" == "120" ]]; then
          die "recovered Scenario fault state was not restored after local-up"
        fi
        sleep 1
      done
      ;;
    *)
      die "active Scenario fault state is invalid: replicas=${expected_replicas:-unavailable}"
      ;;
  esac
  scenario_core_resources_ready "${scenario_id}" || die "active Scenario Kubernetes state was not restored after local-up"
}

wait_for_scenario_alert_resolution() {
  local scenario_id="$1" provider_firing persisted_firing
  for attempt in {1..180}; do
    provider_firing="$(scenario_firing_alerts_strict "${scenario_id}" 2>/dev/null || printf 'unavailable')"
    persisted_firing="$(scenario_persisted_firing_alerts "${scenario_id}" 2>/dev/null || printf 'unavailable')"
    if [[ "${provider_firing}" == "0" && "${persisted_firing}" == "0" ]]; then
      pass "Scenario Alert resolution delivered by Alertmanager and persisted by CloudOps"
      return
    fi
    if ((attempt % 10 == 0)); then
      note "waiting for Scenario Alert resolution provider=${provider_firing} persisted=${persisted_firing}"
    fi
    sleep 1
  done
  die "Scenario Alert did not resolve before runtime removal provider=${provider_firing} persisted=${persisted_firing}"
}

scenario_core_resources_ready() {
  local scenario_id="$1" desired ready running item
  for item in cloudops-scenario-healthy cloudops-scenario-traffic; do
    desired="$(kube -n "${PROVIDER_NAMESPACE}" get deployment "${item}" -o jsonpath='{.spec.replicas}' 2>/dev/null || true)"
    ready="$(kube -n "${PROVIDER_NAMESPACE}" get deployment "${item}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
    [[ "${desired}" == "1" && "${ready}" == "1" ]] || return 1
    [[ "$(kube -n "${PROVIDER_NAMESPACE}" get deployment "${item}" -o jsonpath='{.metadata.labels.cloudops\.io/scenario-id}' 2>/dev/null || true)" == "${scenario_id}" ]] || return 1
  done
  desired="$(scenario_fault_replicas)"
  ready="$(kube -n "${PROVIDER_NAMESPACE}" get deployment cloudops-scenario-fault -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
  running="$(scenario_fault_running_pods)"
  [[ "$(kube -n "${PROVIDER_NAMESPACE}" get deployment cloudops-scenario-fault -o jsonpath='{.metadata.labels.cloudops\.io/scenario-id}' 2>/dev/null || true)" == "${scenario_id}" ]] || return 1
  [[ "${desired}" == "0" || ("${desired}" == "1" && ("${ready}" == "" || "${ready}" == "0") && "${running}" =~ ^[0-9]+$ && "${running}" -ge 1) ]]
}

wait_for_scenario_evidence() {
  local scenario_id="$1" metric_samples=0 firing_alerts=0 log_documents=0 traces=0
  for attempt in {1..120}; do
    metric_samples="$(scenario_metric_samples "${scenario_id}")"
    firing_alerts="$(scenario_firing_alerts "${scenario_id}")"
    log_documents="$(scenario_log_documents "${scenario_id}")"
    traces="$(scenario_traces "${scenario_id}")"
    if ((metric_samples >= 2 && firing_alerts >= 1 && log_documents >= 1 && traces >= 1)); then
      pass "Scenario Evidence Plane ready metrics=${metric_samples} alerts=${firing_alerts} logs=${log_documents} traces=${traces}"
      return
    fi
    if ((attempt % 10 == 0)); then
      note "waiting for Scenario Evidence Plane metrics=${metric_samples} alerts=${firing_alerts} logs=${log_documents} traces=${traces}"
    fi
    sleep 1
  done
  die "Scenario Evidence Plane did not become ready metrics=${metric_samples} alerts=${firing_alerts} logs=${log_documents} traces=${traces}"
}

scenario_up() {
  local scenario_id current_id helm_args
  preflight
  ensure_private_directories
  ensure_cluster
  ensure_namespaces
  ensure_runtime_secrets
  if release_scenario_active; then
    current_id="$(release_scenario_id)"
    valid_scenario_id "${current_id}" || die "active release has an invalid Scenario identity"
    write_scenario_id "${current_id}"
    note "Scenario is already active: ${current_id}"
    start_port_forward
    start_grafana_port_forward
    start_tempo_port_forward
    inject_scenario_fault
    wait_for_scenario_evidence "${current_id}"
    scenario_status
    return
  fi

  build_application_images
  load_runtime_images
  if ! release_exists; then
    install_runtime
  fi
  scenario_id="scenario-$(date -u +%Y%m%d%H%M%S)-$(openssl rand -hex 4)"
  write_scenario_id "${scenario_id}"
  helm_args=(
    --kube-context "${KUBE_CONTEXT}"
    --namespace "${NAMESPACE}"
    --values "${VALUES_FILE}"
    --timeout 12m
  )
  note "activating bounded Scenario ${scenario_id} in the canonical CloudOps release"
  helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" "${helm_args[@]}" \
    --set scenario.enabled=true \
    --set-string "scenario.id=${scenario_id}" \
    --set-string worker.env.K8S_WRITE_ENABLED=true \
    --wait --wait-for-jobs
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-api --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/prometheus --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/alertmanager --timeout=5m
  kube -n "${PROVIDER_NAMESPACE}" rollout status deployment/cloudops-scenario-healthy --timeout=5m
  kube -n "${PROVIDER_NAMESPACE}" rollout status deployment/cloudops-scenario-fault --timeout=5m
  kube -n "${PROVIDER_NAMESPACE}" rollout status deployment/cloudops-scenario-traffic --timeout=5m
  inject_scenario_fault
  scenario_core_resources_ready "${scenario_id}" || die "Scenario Kubernetes resources are not ready"
  start_port_forward
  start_grafana_port_forward
  start_tempo_port_forward
  wait_for_scenario_evidence "${scenario_id}"
  scenario_status
}

scenario_status() {
  local scenario_id file_id write_gate resource_count fault_replicas fault_state stale_firing
  local metric_samples firing_alerts log_documents traces agent_runs history failed=0
  validate_fixed_boundaries
  if ! context_exists || ! release_exists; then
    printf 'scenario_state=unavailable\nreason=runtime_not_available\n'
    return 1
  fi
  if ! release_scenario_active; then
    printf 'scenario_state=inactive\nscenario_id=\nscenario_write_gate=false\n'
    resource_count="$(kube -n "${PROVIDER_NAMESPACE}" get deployments,services,serviceaccounts -o json 2>/dev/null | jq -r '[.items[]? | select(.metadata.name | startswith("cloudops-scenario"))] | length')"
    printf 'scenario_runtime_resources=%s\n' "${resource_count}"
    stale_firing="$(scenario_stale_firing_alerts 2>/dev/null || printf 'unavailable')"
    printf 'scenario_stale_firing_alerts=%s\n' "${stale_firing}"
    [[ "${resource_count}" == "0" && "${stale_firing}" == "0" ]] || return 1
    return
  fi
  scenario_id="$(release_scenario_id)"
  valid_scenario_id "${scenario_id}" || die "active release has an invalid Scenario identity"
  file_id="$(scenario_id_from_file 2>/dev/null || true)"
  write_gate="$(kube -n "${NAMESPACE}" get deployment cloudops-worker -o json 2>/dev/null | jq -r '[.spec.template.spec.containers[0].env[] | select(.name=="K8S_WRITE_ENABLED") | .value][0] // "unavailable"')"
  fault_replicas="$(scenario_fault_replicas)"
  case "${fault_replicas}" in
    1) fault_state=degraded ;;
    0) fault_state=recovered ;;
    *) fault_state=invalid; failed=1 ;;
  esac
  resource_count="$(kube -n "${PROVIDER_NAMESPACE}" get deployments,services,serviceaccounts -l "cloudops.io/scenario-id=${scenario_id}" --no-headers 2>/dev/null | wc -l)"
  metric_samples="$(scenario_metric_samples "${scenario_id}")"
  firing_alerts="$(scenario_firing_alerts "${scenario_id}")"
  log_documents="$(scenario_log_documents "${scenario_id}")"
  traces="$(scenario_traces "${scenario_id}")"
  agent_runs="$(scenario_agent_runs "${scenario_id}" 2>/dev/null || printf '0')"
  history="$(scenario_history_count "${scenario_id}" 2>/dev/null || printf '0')"
  printf 'scenario_state=active\nscenario_id=%s\nscenario_fault_state=%s\nscenario_fault_replicas=%s\nscenario_write_gate=%s\nscenario_runtime_resources=%s\n' \
    "${scenario_id}" "${fault_state}" "${fault_replicas:-unavailable}" "${write_gate}" "${resource_count}"
  printf 'scenario_metrics_samples=%s\nscenario_firing_alerts=%s\nscenario_log_documents=%s\nscenario_traces=%s\nscenario_agent_runs=%s\nscenario_history_records=%s\n' \
    "${metric_samples}" "${firing_alerts}" "${log_documents}" "${traces}" "${agent_runs}" "${history}"
  if [[ "${file_id}" == "${scenario_id}" ]]; then
    printf 'scenario_identity_file=PASS\n'
  else
    printf 'scenario_identity_file=FAIL\n'
    failed=1
  fi
  if [[ "${write_gate}" == "true" ]]; then
    printf 'scenario_write_boundary=PASS\n'
  else
    printf 'scenario_write_boundary=FAIL\n'
    failed=1
  fi
  if scenario_core_resources_ready "${scenario_id}"; then
    printf 'scenario_kubernetes=PASS\n'
  else
    printf 'scenario_kubernetes=FAIL\n'
    failed=1
  fi
  if [[ "${fault_state}" == "degraded" ]]; then
    if ((metric_samples >= 2)); then printf 'scenario_metrics=PASS\n'; else printf 'scenario_metrics=FAIL\n'; failed=1; fi
    if ((firing_alerts >= 1)); then printf 'scenario_alert=PASS\n'; else printf 'scenario_alert=FAIL\n'; failed=1; fi
  else
    if ((metric_samples >= 1)); then printf 'scenario_metrics=PASS\n'; else printf 'scenario_metrics=FAIL\n'; failed=1; fi
    if ((firing_alerts == 0)); then printf 'scenario_alert=PASS_RESOLVED\n'; else printf 'scenario_alert=PENDING_RESOLUTION\n'; fi
  fi
  if ((log_documents >= 1)); then printf 'scenario_logs=PASS\n'; else printf 'scenario_logs=FAIL\n'; failed=1; fi
  if ((traces >= 1)); then printf 'scenario_traces=PASS\n'; else printf 'scenario_traces=FAIL\n'; failed=1; fi
  if ((agent_runs >= 1)); then printf 'scenario_agent=PASS\n'; else printf 'scenario_agent=NOT_RUN\n'; fi
  [[ "${failed}" == "0" ]] || return 1
}

scenario_down() {
  local scenario_id history_before history_after write_gate remaining helm_args can_write
  validate_fixed_boundaries
  ensure_private_directories
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  release_exists || die "CloudOps release is unavailable; run make local-up"
  if ! release_scenario_active; then
    rm -f "${SCENARIO_ID_FILE}"
    note "Scenario runtime is already inactive"
    scenario_status
    return
  fi
  scenario_id="$(release_scenario_id)"
  valid_scenario_id "${scenario_id}" || die "active release has an invalid Scenario identity"
  history_before="$(scenario_history_count "${scenario_id}" 2>/dev/null || printf '0')"
  recover_scenario_fault_for_shutdown
  wait_for_scenario_alert_resolution "${scenario_id}"
  helm_args=(
    --kube-context "${KUBE_CONTEXT}"
    --namespace "${NAMESPACE}"
    --values "${VALUES_FILE}"
    --timeout 12m
  )
  note "deactivating bounded Scenario ${scenario_id}; retained history will not be deleted"
  helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" "${helm_args[@]}" \
    --set scenario.enabled=false \
    --set-string worker.env.K8S_WRITE_ENABLED=false \
    --wait --wait-for-jobs
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-api --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m
  remaining="$(kube -n "${PROVIDER_NAMESPACE}" get deployments,services,serviceaccounts -l "cloudops.io/scenario-id=${scenario_id}" --no-headers 2>/dev/null | wc -l)"
  [[ "${remaining}" == "0" ]] || die "Scenario runtime resources remain after scenario-down: ${remaining}"
  write_gate="$(kube -n "${NAMESPACE}" get deployment cloudops-worker -o json | jq -r '[.spec.template.spec.containers[0].env[] | select(.name=="K8S_WRITE_ENABLED") | .value][0] // "unavailable"')"
  [[ "${write_gate}" == "false" ]] || die "Scenario write gate remained enabled after scenario-down"
  can_write="$(kube auth can-i update deployments.apps/cloudops-scenario-fault --subresource=scale --as=system:serviceaccount:${NAMESPACE}:cloudops-worker -n "${PROVIDER_NAMESPACE}" 2>/dev/null || true)"
  [[ "${can_write}" == "no" ]] || die "Scenario scale RBAC remained authorized after scenario-down"
  history_after="$(scenario_history_count "${scenario_id}" 2>/dev/null || printf '0')"
  ((history_after >= history_before)) || die "Scenario history was not retained after scenario-down"
  rm -f "${SCENARIO_ID_FILE}"
  pass "Scenario runtime removed write_gate=false retained_history_before=${history_before} retained_history_after=${history_after}"
  scenario_status
}

port_forward_pid() {
  [[ -f "${PORT_FORWARD_PID_FILE}" ]] || return 1
  tr -d '[:space:]' <"${PORT_FORWARD_PID_FILE}"
}

port_forward_process_matches() {
  local pid="$1" command_line
  [[ "${pid}" =~ ^[0-9]+$ && -r "/proc/${pid}/cmdline" ]] || return 1
  command_line="$(tr '\0' ' ' <"/proc/${pid}/cmdline")"
  [[ "${command_line}" == *"kubectl"* &&
     "${command_line}" == *"${KUBE_CONTEXT}"* &&
     "${command_line}" == *"${NAMESPACE}"* &&
     "${command_line}" == *"service/cloudops-api"* &&
     "${command_line}" == *"${LOCAL_PORT}:8080"* ]]
}

stop_port_forward() {
  local pid
  pid="$(port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && port_forward_process_matches "${pid}"; then
    kill "${pid}"
    for _attempt in {1..20}; do
      kill -0 "${pid}" 2>/dev/null || break
      sleep 0.1
    done
  fi
  rm -f "${PORT_FORWARD_PID_FILE}"
}

start_port_forward() {
  local pid
  ensure_private_directories
  pid="$(port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && port_forward_process_matches "${pid}" && curl --noproxy '*' --fail --silent --max-time 2 "${LOCAL_URL}/livez" >/dev/null 2>&1; then
    note "reusing loopback access process pid=${pid}"
    return
  fi
  stop_port_forward
  : >"${PORT_FORWARD_LOG}"
  chmod 600 "${PORT_FORWARD_LOG}"
  nohup kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    port-forward --address=127.0.0.1 service/cloudops-api "${LOCAL_PORT}:8080" \
    </dev/null >"${PORT_FORWARD_LOG}" 2>&1 &
  pid="$!"
  printf '%s\n' "${pid}" >"${PORT_FORWARD_PID_FILE}"
  chmod 600 "${PORT_FORWARD_PID_FILE}"
  for _attempt in {1..60}; do
    if curl --noproxy '*' --fail --silent --max-time 2 "${LOCAL_URL}/livez" >/dev/null 2>&1; then
      pass "loopback_url=${LOCAL_URL}"
      return
    fi
    kill -0 "${pid}" 2>/dev/null || break
    sleep 0.5
  done
  stop_port_forward
  tail -n 40 "${PORT_FORWARD_LOG}" >&2 || true
  die "CloudOps loopback access did not become ready"
}

grafana_port_forward_pid() {
  [[ -f "${GRAFANA_PORT_FORWARD_PID_FILE}" ]] || return 1
  tr -d '[:space:]' <"${GRAFANA_PORT_FORWARD_PID_FILE}"
}

grafana_port_forward_process_matches() {
  local pid="$1" command_line
  [[ "${pid}" =~ ^[0-9]+$ && -r "/proc/${pid}/cmdline" ]] || return 1
  command_line="$(tr '\0' ' ' <"/proc/${pid}/cmdline")"
  [[ "${command_line}" == *"kubectl"* &&
     "${command_line}" == *"${KUBE_CONTEXT}"* &&
     "${command_line}" == *"${NAMESPACE}"* &&
     "${command_line}" == *"service/grafana"* &&
     "${command_line}" == *"${GRAFANA_LOCAL_PORT}:3000"* ]]
}

stop_grafana_port_forward() {
  local pid
  pid="$(grafana_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && grafana_port_forward_process_matches "${pid}"; then
    kill "${pid}"
    for _attempt in {1..20}; do
      kill -0 "${pid}" 2>/dev/null || break
      sleep 0.1
    done
  fi
  rm -f "${GRAFANA_PORT_FORWARD_PID_FILE}"
}

start_grafana_port_forward() {
  local pid
  ensure_private_directories
  pid="$(grafana_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && grafana_port_forward_process_matches "${pid}" &&
     curl --noproxy '*' --fail --silent --max-time 2 "${GRAFANA_LOCAL_URL}/api/health" >/dev/null 2>&1; then
    note "reusing Grafana loopback process pid=${pid}"
    return
  fi
  stop_grafana_port_forward
  : >"${GRAFANA_PORT_FORWARD_LOG}"
  chmod 600 "${GRAFANA_PORT_FORWARD_LOG}"
  nohup kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    port-forward --address=127.0.0.1 service/grafana "${GRAFANA_LOCAL_PORT}:3000" \
    </dev/null >"${GRAFANA_PORT_FORWARD_LOG}" 2>&1 &
  pid="$!"
  printf '%s\n' "${pid}" >"${GRAFANA_PORT_FORWARD_PID_FILE}"
  chmod 600 "${GRAFANA_PORT_FORWARD_PID_FILE}"
  for _attempt in {1..60}; do
    if curl --noproxy '*' --fail --silent --max-time 2 "${GRAFANA_LOCAL_URL}/api/health" >/dev/null 2>&1; then
      pass "grafana_url=${GRAFANA_LOCAL_URL}"
      return
    fi
    kill -0 "${pid}" 2>/dev/null || break
    sleep 0.5
  done
  stop_grafana_port_forward
  tail -n 40 "${GRAFANA_PORT_FORWARD_LOG}" >&2 || true
  die "Grafana loopback access did not become ready"
}

tempo_port_forward_pid() {
  [[ -f "${TEMPO_PORT_FORWARD_PID_FILE}" ]] || return 1
  tr -d '[:space:]' <"${TEMPO_PORT_FORWARD_PID_FILE}"
}

tempo_port_forward_process_matches() {
  local pid="$1" command_line
  [[ "${pid}" =~ ^[0-9]+$ && -r "/proc/${pid}/cmdline" ]] || return 1
  command_line="$(tr '\0' ' ' <"/proc/${pid}/cmdline")"
  [[ "${command_line}" == *"kubectl"* &&
     "${command_line}" == *"${KUBE_CONTEXT}"* &&
     "${command_line}" == *"${NAMESPACE}"* &&
     "${command_line}" == *"service/tempo"* &&
     "${command_line}" == *"${TEMPO_LOCAL_PORT}:3200"* ]]
}

stop_tempo_port_forward() {
  local pid
  pid="$(tempo_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && tempo_port_forward_process_matches "${pid}"; then
    kill "${pid}"
    for _attempt in {1..20}; do
      kill -0 "${pid}" 2>/dev/null || break
      sleep 0.1
    done
  fi
  rm -f "${TEMPO_PORT_FORWARD_PID_FILE}"
}

start_tempo_port_forward() {
  local pid
  ensure_private_directories
  pid="$(tempo_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && tempo_port_forward_process_matches "${pid}" &&
     curl --noproxy '*' --fail --silent --max-time 2 "${TEMPO_LOCAL_URL}/ready" >/dev/null 2>&1; then
    note "reusing Tempo loopback process pid=${pid}"
    return
  fi
  stop_tempo_port_forward
  : >"${TEMPO_PORT_FORWARD_LOG}"
  chmod 600 "${TEMPO_PORT_FORWARD_LOG}"
  nohup kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    port-forward --address=127.0.0.1 service/tempo "${TEMPO_LOCAL_PORT}:3200" \
    </dev/null >"${TEMPO_PORT_FORWARD_LOG}" 2>&1 &
  pid="$!"
  printf '%s\n' "${pid}" >"${TEMPO_PORT_FORWARD_PID_FILE}"
  chmod 600 "${TEMPO_PORT_FORWARD_PID_FILE}"
  for _attempt in {1..60}; do
    if curl --noproxy '*' --fail --silent --max-time 2 "${TEMPO_LOCAL_URL}/ready" >/dev/null 2>&1; then
      pass "tempo_url=${TEMPO_LOCAL_URL}"
      return
    fi
    kill -0 "${pid}" 2>/dev/null || break
    sleep 0.5
  done
  stop_tempo_port_forward
  tail -n 40 "${TEMPO_PORT_FORWARD_LOG}" >&2 || true
  die "Tempo loopback access did not become ready"
}

local_up() {
  local active_scenario_id="" active_scenario_fault_replicas="" release_values scenario_enabled
  preflight
  ensure_private_directories
  ensure_cluster
  ensure_namespaces
  ensure_runtime_secrets
  if release_exists; then
    release_values="$(release_scenario_json)" || die "failed to read the current release Scenario state"
    scenario_enabled="$(jq -er '(.scenario.enabled // false) | if type == "boolean" then tostring else error("scenario.enabled must be boolean") end' <<<"${release_values}")" ||
      die "current release has an invalid Scenario enabled state"
    if [[ "${scenario_enabled}" == "true" ]]; then
      active_scenario_id="$(jq -er '.scenario.id | select(type == "string" and length > 0)' <<<"${release_values}")" ||
        die "active release is missing its Scenario identity"
      valid_scenario_id "${active_scenario_id}" || die "active release has an invalid Scenario identity"
      active_scenario_fault_replicas="$(scenario_fault_replicas)"
      [[ "${active_scenario_fault_replicas}" == "0" || "${active_scenario_fault_replicas}" == "1" ]] ||
        die "active release has an invalid Scenario fault state"
    fi
  fi
  build_application_images
  load_runtime_images
  install_runtime "${active_scenario_id}"
  if [[ -n "${active_scenario_id}" ]]; then
    [[ "$(release_scenario_id)" == "${active_scenario_id}" ]] ||
      die "local-up changed the active Scenario identity"
    restore_active_scenario_fault_state "${active_scenario_id}" "${active_scenario_fault_replicas}"
    write_scenario_id "${active_scenario_id}"
    pass "active Scenario preserved across local-up: ${active_scenario_id}"
  else
    rm -f "${SCENARIO_ID_FILE}"
  fi
  start_port_forward
  start_grafana_port_forward
  start_tempo_port_forward
  print_status
}

local_open() {
  validate_fixed_boundaries
  validate_local_port
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  release_exists || die "CloudOps release is unavailable; run make local-up"
  start_port_forward
  start_grafana_port_forward
  start_tempo_port_forward
  printf '%s\n' "${LOCAL_URL}"
  if [[ "${CLOUDOPS_NO_OPEN:-0}" != "1" ]] && command -v xdg-open >/dev/null 2>&1; then
    nohup xdg-open "${LOCAL_URL}" </dev/null >/dev/null 2>&1 &
  fi
}

validate_secret_relative_path() {
  local path="$1"
  [[ "${path}" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,254}$ ]] ||
    die "invalid secret backup path: ${path}"
  [[ "/${path}/" != *"/../"* && "${path}" != */.. && "${path}" != ./* ]] ||
    die "unsafe secret backup path: ${path}"
}

stop_data_transfer_pod() {
  if context_exists; then
    kube -n "${NAMESPACE}" delete pod "${DATA_TRANSFER_POD}" --ignore-not-found --wait=true >/dev/null 2>&1 || true
  fi
  DATA_TRANSFER_ACTIVE=0
}

start_data_transfer_pod() {
  local image existing_component
  [[ "$(kube -n "${NAMESPACE}" get pvc "${DATA_CLAIM}" -o jsonpath='{.status.phase}' 2>/dev/null || true)" == "Bound" ]] ||
    die "operational data PVC is not Bound: ${DATA_CLAIM}"
  image="$(kube -n "${NAMESPACE}" get deployment cloudops-api -o jsonpath='{.spec.template.spec.containers[?(@.name=="cloudops-api")].image}' 2>/dev/null || true)"
  [[ "${image}" =~ ^[A-Za-z0-9._/:@-]+$ ]] || die "could not resolve the exact CloudOps API image for data transfer"
  existing_component="$(kube -n "${NAMESPACE}" get pod "${DATA_TRANSFER_POD}" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/component}' 2>/dev/null || true)"
  if [[ -n "${existing_component}" ]]; then
    [[ "${existing_component}" == "operational-data-transfer" ]] ||
      die "refusing to replace unmanaged Pod ${DATA_TRANSFER_POD}"
    stop_data_transfer_pod
  fi
  kube -n "${NAMESPACE}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${DATA_TRANSFER_POD}
  labels:
    app.kubernetes.io/instance: ${RELEASE_NAME}
    app.kubernetes.io/component: operational-data-transfer
    cloudops.io/managed-by: cloudops-local
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  enableServiceLinks: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    fsGroup: 65532
    fsGroupChangePolicy: OnRootMismatch
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: transfer
      image: "${image}"
      imagePullPolicy: IfNotPresent
      command: ["/bin/sh", "-ec", "trap 'exit 0' TERM INT; sleep 600 & wait"]
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      volumeMounts:
        - name: operational-data
          mountPath: ${DATA_MOUNT_PATH}
        - name: tmp
          mountPath: /tmp
  volumes:
    - name: operational-data
      persistentVolumeClaim:
        claimName: ${DATA_CLAIM}
    - name: tmp
      emptyDir: {}
EOF
  DATA_TRANSFER_ACTIVE=1
  if ! kube -n "${NAMESPACE}" wait --for=condition=Ready "pod/${DATA_TRANSFER_POD}" --timeout=120s >/dev/null; then
    kube -n "${NAMESPACE}" describe pod "${DATA_TRANSFER_POD}" >&2 || true
    die "operational data transfer Pod did not become ready"
  fi
}

sync_operational_secrets_from_runtime() {
  local staged previous
  staged="$(mktemp -d "${STATE_DIR}/.secrets.sync.XXXXXX")"
  previous="${STATE_DIR}/.secrets.previous.$$"
  [[ ! -e "${previous}" ]] || die "stale operational secret sync directory exists: ${previous}"
  start_data_transfer_pod
  # Expansion belongs to the shell inside the data-transfer Pod.
  # shellcheck disable=SC2016
  if ! kube -n "${NAMESPACE}" exec "${DATA_TRANSFER_POD}" -- sh -ec \
    'root="$1"; mkdir -p "${root}"; test ! -L "${root}"; cd "${root}"; exec tar -cf - .' \
    cloudops-data "${DATA_DIRECTORY}/secrets" | tar -xf - -C "${staged}"; then
    stop_data_transfer_pod
    rm -rf "${staged}"
    die "could not read operational secret files from ${DATA_CLAIM}"
  fi
  stop_data_transfer_pod
  [[ -z "$(find "${staged}" -type l -print -quit)" ]] || die "runtime operational secret directory contains a symbolic link"
  [[ -z "$(find "${staged}" ! -type d ! -type f -print -quit)" ]] || die "runtime operational secret directory contains a special file"
  find "${staged}" -type d -exec chmod 700 {} +
  find "${staged}" -type f -exec chmod 600 {} +
  mv "${SECRET_DIR}" "${previous}"
  mv "${staged}" "${SECRET_DIR}"
  rm -rf "${previous}"
}

sync_operational_secrets_to_runtime() {
  local source_dir="$1"
  [[ -d "${source_dir}" && ! -L "${source_dir}" ]] || die "operational secret restore source is unavailable"
  start_data_transfer_pod
  # Expansion belongs to the shell inside the data-transfer Pod.
  # shellcheck disable=SC2016
  if ! tar -C "${source_dir}" -cf - . | kube -n "${NAMESPACE}" exec -i "${DATA_TRANSFER_POD}" -- sh -ec \
    'root="$1"; target="${root}/secrets"; staged="${root}/.secrets.restore.$$"; previous="${root}/.secrets.previous.$$";
     test ! -L "${root}"; mkdir -p "${root}"; rm -rf "${staged}" "${previous}"; mkdir "${staged}";
     tar -xf - -C "${staged}"; test -z "$(find "${staged}" -type l -print -quit)";
     test -z "$(find "${staged}" ! -type d ! -type f -print -quit)";
     find "${staged}" -type d -exec chmod 700 {} +; find "${staged}" -type f -exec chmod 600 {} +;
     if test -e "${target}"; then test ! -L "${target}"; mv "${target}" "${previous}"; fi;
     mv "${staged}" "${target}"; rm -rf "${previous}"' \
    cloudops-data "${DATA_DIRECTORY}"; then
    stop_data_transfer_pod
    die "could not restore operational secret files to ${DATA_CLAIM}"
  fi
  stop_data_transfer_pod
}

write_secret_manifest() {
  local source_dir="$1" output="$2" relative hash
  [[ -z "$(find "${source_dir}" -type l -print -quit)" ]] ||
    die "operational secret directory contains a symbolic link"
  [[ -z "$(find "${source_dir}" ! -type d ! -type f -print -quit)" ]] ||
    die "operational secret directory contains a special file"
  : >"${output}"
  while IFS= read -r relative; do
    [[ -n "${relative}" ]] || continue
    validate_secret_relative_path "${relative}"
    [[ -f "${source_dir}/${relative}" && ! -L "${source_dir}/${relative}" ]] ||
      die "secret backup source is not a regular file: ${relative}"
    hash="$(sha256sum "${source_dir}/${relative}" | awk '{print $1}')"
    printf '%s\t%s\n' "${hash}" "${relative}" >>"${output}"
  done < <(find "${source_dir}" -type f -printf '%P\n' | LC_ALL=C sort)
}

validate_secret_manifest() {
  local backup="$1" hash relative actual listed_count=0 actual_count
  local -A listed=()
  [[ -z "$(find "${backup}/secret-files" -type l -print -quit)" ]] ||
    die "secret backup contains a symbolic link"
  [[ -z "$(find "${backup}/secret-files" ! -type d ! -type f -print -quit)" ]] ||
    die "secret backup contains a special file"
  while IFS=$'\t' read -r hash relative; do
    [[ -n "${hash}" || -n "${relative}" ]] || continue
    [[ "${hash}" =~ ^[0-9a-f]{64}$ ]] || die "invalid secret manifest hash"
    validate_secret_relative_path "${relative}"
    [[ -z "${listed[${relative}]+present}" ]] || die "duplicate secret manifest path: ${relative}"
    [[ -f "${backup}/secret-files/${relative}" &&
       ! -L "${backup}/secret-files/${relative}" ]] ||
      die "secret manifest file is unavailable: ${relative}"
    actual="$(sha256sum "${backup}/secret-files/${relative}" | awk '{print $1}')"
    [[ "${actual}" == "${hash}" ]] || die "secret manifest hash mismatch: ${relative}"
    listed["${relative}"]=1
    listed_count=$((listed_count + 1))
  done <"${backup}/secret-manifest.tsv"

  actual_count="$(find "${backup}/secret-files" -type f | wc -l)"
  [[ "${actual_count}" == "${listed_count}" ]] ||
    die "secret manifest omits files"
  while IFS= read -r relative; do
    [[ -n "${listed[${relative}]+present}" ]] ||
      die "unlisted secret backup file: ${relative}"
  done < <(find "${backup}/secret-files" -type f -printf '%P\n' | LC_ALL=C sort)
}

verify_row_counts() {
  local pod="$1" database="$2" row_counts="$3" table expected actual lines=0 actual_tables
  local -A seen=()
  while IFS=$'\t' read -r table expected; do
    [[ -n "${table}" ]] || continue
    validate_identifier "${table}"
    [[ "${expected}" =~ ^[0-9]+$ ]] || die "invalid row count for table ${table}"
    [[ -z "${seen[${table}]+present}" ]] || die "duplicate row-count table: ${table}"
    seen["${table}"]=1
    actual="$(mysql_exec "${pod}" "${database}" -e "SELECT COUNT(*) FROM \`${table}\`")"
    [[ "${actual}" == "${expected}" ]] ||
      die "row-count mismatch table=${table} expected=${expected} actual=${actual}"
    lines=$((lines + 1))
  done <"${row_counts}"
  [[ "${lines}" -gt 0 ]] || die "backup row-count manifest is empty"
  actual_tables="$(mysql_exec "${pod}" -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='${database}'")"
  [[ "${actual_tables}" == "${lines}" ]] ||
    die "restored table-count mismatch expected=${lines} actual=${actual_tables}"
}

restore_operational_secrets() {
  local backup="$1" verified_manifest
  sync_operational_secrets_to_runtime "${backup}/secret-files"
  sync_operational_secrets_from_runtime
  verified_manifest="$(mktemp "${STATE_DIR}/.secret-manifest.XXXXXX")"
  write_secret_manifest "${SECRET_DIR}" "${verified_manifest}"
  if ! cmp -s "${backup}/secret-manifest.tsv" "${verified_manifest}"; then
    rm -f "${verified_manifest}"
    die "restored operational secret manifest does not match the backup"
  fi
  rm -f "${verified_manifest}"
}

wait_for_deployment_scale_zero() {
  local deployment="$1" replicas
  for _attempt in {1..120}; do
    replicas="$(kube -n "${NAMESPACE}" get deployment "${deployment}" -o jsonpath='{.status.replicas}' 2>/dev/null || true)"
    if [[ -z "${replicas}" || "${replicas}" == "0" ]]; then
      return
    fi
    sleep 0.5
  done
  die "deployment did not scale to zero: ${deployment}"
}

restore_runtime_replicas() {
  local api_replicas="$1" worker_replicas="$2"
  kube -n "${NAMESPACE}" scale deployment/cloudops-api --replicas="${api_replicas}" >/dev/null || return
  kube -n "${NAMESPACE}" scale deployment/cloudops-worker --replicas="${worker_replicas}" >/dev/null || return
  if ((api_replicas > 0)); then
    kube -n "${NAMESPACE}" rollout status deployment/cloudops-api --timeout=5m || return
  fi
  if ((worker_replicas > 0)); then
    kube -n "${NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m || return
  fi
}

restore_exit_cleanup() {
  local exit_status="$?" cleanup_failed=0
  trap - EXIT
  set +e
  if [[ "${RESTORE_CLEANUP_STAGING}" == "1" && -n "${RESTORE_CLEANUP_POD}" ]]; then
    mysql_exec "${RESTORE_CLEANUP_POD}" -e "DROP DATABASE IF EXISTS \`${RESTORE_STAGING_DATABASE}\`" >/dev/null || {
      printf 'FAIL: could not remove restore staging database %s\n' "${RESTORE_STAGING_DATABASE}" >&2
      cleanup_failed=1
    }
  fi
  if [[ "${RESTORE_RECOVER_RUNTIME}" == "1" ]]; then
    restore_runtime_replicas "${RESTORE_API_REPLICAS}" "${RESTORE_WORKER_REPLICAS}" || {
      printf 'FAIL: could not restore API/Worker replica counts after restore failure\n' >&2
      cleanup_failed=1
    }
  fi
  if [[ "${exit_status}" == "0" && "${cleanup_failed}" == "1" ]]; then
    exit_status=1
  fi
  exit "${exit_status}"
}

create_backup() {
  local pod timestamp exact_sha tmp_dir target schema_id version checksum_tmp
  local row_counts_hash secret_manifest_hash table_count
  for command_name in kubectl jq sha256sum realpath git; do
    require_command "${command_name}"
  done
  validate_fixed_boundaries
  ensure_private_directories
  validate_identifier "${DATABASE_NAME}"
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  pod="$(mysql_pod)"
  database_exists "${pod}" "${DATABASE_NAME}" || die "database does not exist: ${DATABASE_NAME}"
  version="$(schema_version "${pod}" "${DATABASE_NAME}")"
  [[ "${version}" == "${LATEST_SCHEMA_VERSION}" ]] ||
    die "refusing unsupported backup schema_version=${version}; expected ${LATEST_SCHEMA_VERSION}"

  umask 077
  tmp_dir="$(mktemp -d "${BACKUP_ROOT}/.incomplete.XXXXXX")"
  trap 'rm -rf "${tmp_dir:-}"' RETURN
  chmod 700 "${tmp_dir}"

  note "creating a consistent MySQL dump from ${KUBE_CONTEXT}/${NAMESPACE}"
  # Expansion belongs to the shell inside the MySQL Pod.
  # shellcheck disable=SC2016
  kube -n "${NAMESPACE}" exec "${pod}" -- sh -ec \
    'export MYSQL_PWD="${MYSQL_ROOT_PASSWORD:?MYSQL_ROOT_PASSWORD is required}"; exec mysqldump --protocol=socket -uroot --single-transaction --quick --hex-blob --routines --triggers --events --set-gtid-purged=OFF --no-tablespaces --skip-dump-date "$1"' \
    cloudops-dump "${DATABASE_NAME}" >"${tmp_dir}/database.sql"
  # Expansion belongs to the shell inside the MySQL Pod.
  # shellcheck disable=SC2016
  kube -n "${NAMESPACE}" exec "${pod}" -- sh -ec \
    'export MYSQL_PWD="${MYSQL_ROOT_PASSWORD:?MYSQL_ROOT_PASSWORD is required}"; exec mysqldump --protocol=socket -uroot --no-data --routines --triggers --events --set-gtid-purged=OFF --no-tablespaces --skip-dump-date "$1"' \
    cloudops-schema "${DATABASE_NAME}" |
    sed -E 's/ AUTO_INCREMENT=[0-9]+//' >"${tmp_dir}/schema.sql"
  chmod 600 "${tmp_dir}/database.sql" "${tmp_dir}/schema.sql"

  write_row_counts "${pod}" "${DATABASE_NAME}" "${tmp_dir}/row-counts.tsv"
  sync_operational_secrets_from_runtime
  mkdir "${tmp_dir}/secret-files"
  cp -a "${SECRET_DIR}/." "${tmp_dir}/secret-files/"
  chmod -R go-rwx "${tmp_dir}/secret-files"
  write_secret_manifest "${tmp_dir}/secret-files" "${tmp_dir}/secret-manifest.tsv"

  schema_id="$(sha256sum "${tmp_dir}/schema.sql" | awk '{print $1}')"
  row_counts_hash="$(sha256sum "${tmp_dir}/row-counts.tsv" | awk '{print $1}')"
  secret_manifest_hash="$(sha256sum "${tmp_dir}/secret-manifest.tsv" | awk '{print $1}')"
  table_count="$(wc -l <"${tmp_dir}/row-counts.tsv")"
  exact_sha="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  jq -n \
    --argjson format_version "${BACKUP_FORMAT_VERSION}" \
    --arg contract "${BACKUP_CONTRACT}" \
    --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg cluster_name "${CLUSTER_NAME}" \
    --arg kube_context "${KUBE_CONTEXT}" \
    --arg namespace "${NAMESPACE}" \
    --arg database "${DATABASE_NAME}" \
    --arg schema_version "${version}" \
    --arg schema_identity "sha256:${schema_id}" \
    --arg row_counts_hash "${row_counts_hash}" \
    --argjson table_count "${table_count}" \
    --arg secret_manifest_hash "${secret_manifest_hash}" \
    --arg source_commit "${exact_sha}" \
    '{format_version:$format_version,contract:$contract,created_at:$created_at,cluster_name:$cluster_name,kube_context:$kube_context,namespace:$namespace,database:$database,schema_version:$schema_version,schema_identity:$schema_identity,row_counts_hash:$row_counts_hash,table_count:$table_count,secret_manifest_hash:$secret_manifest_hash,source_commit:$source_commit}' \
    >"${tmp_dir}/metadata.json"
  chmod 600 \
    "${tmp_dir}/metadata.json" "${tmp_dir}/row-counts.tsv" \
    "${tmp_dir}/secret-manifest.tsv"
  checksum_tmp="$(mktemp "${BACKUP_ROOT}/.checksums.XXXXXX")"
  (
    cd "${tmp_dir}"
    find . -type f ! -path './SHA256SUMS' -printf '%P\0' | LC_ALL=C sort -z | xargs -0 sha256sum
  ) >"${checksum_tmp}"
  mv "${checksum_tmp}" "${tmp_dir}/SHA256SUMS"
  (cd "${tmp_dir}" && sha256sum --check --quiet SHA256SUMS)
  chmod 600 "${tmp_dir}/SHA256SUMS"

  target="${BACKUP_ROOT}/${timestamp}-${exact_sha:0:12}"
  [[ ! -e "${target}" ]] || die "backup already exists: ${target}"
  mv "${tmp_dir}" "${target}"
  trap - RETURN
  mysql_exec "${pod}" "${DATABASE_NAME}" -e "INSERT INTO backup_records
    (public_id, backup_name, schema_version, schema_identity, source_commit, created_at)
    VALUES (UUID(), '${target##*/}', ${version}, 'sha256:${schema_id}', '${exact_sha}',
            STR_TO_DATE('$(jq -r '.created_at' "${target}/metadata.json")', '%Y-%m-%dT%H:%i:%sZ'))
    ON DUPLICATE KEY UPDATE backup_name = VALUES(backup_name)" >/dev/null
  pass "backup=${target} schema_version=${version} schema_identity=sha256:${schema_id}"
}

validate_backup() {
  local backup="$1" format contract version source_database
  local expected actual checksum path table_count checksummed_count actual_file_count
  local -A checksummed=()
  [[ -n "${backup}" ]] || die "BACKUP is required"
  [[ -d "${backup}" ]] || die "backup directory does not exist: ${backup}"
  for file in metadata.json database.sql schema.sql row-counts.tsv secret-manifest.tsv SHA256SUMS; do
    [[ -f "${backup}/${file}" ]] || die "backup is missing ${file}"
  done
  [[ -d "${backup}/secret-files" && ! -L "${backup}/secret-files" ]] ||
    die "backup is missing the operational secret directory"
  [[ -z "$(find "${backup}" -type l -print -quit)" ]] ||
    die "backup contains a symbolic link"

  format="$(jq -r '.format_version' "${backup}/metadata.json")"
  [[ "${format}" == "${BACKUP_FORMAT_VERSION}" ]] || die "unsupported backup format: ${format}"
  contract="$(jq -r '.contract' "${backup}/metadata.json")"
  [[ "${contract}" == "${BACKUP_CONTRACT}" ]] || die "unsupported backup contract: ${contract}"
  version="$(jq -r '.schema_version' "${backup}/metadata.json")"
  [[ "${version}" == "${LATEST_SCHEMA_VERSION}" ]] || die "backup schema is not current: ${version}"
  source_database="$(jq -r '.database' "${backup}/metadata.json")"
  [[ "${source_database}" == "${DATABASE_NAME}" ]] ||
    die "backup database identity is not ${DATABASE_NAME}"

  while read -r checksum path; do
    path="${path#\*}"
    [[ "${checksum}" =~ ^[0-9a-f]{64}$ ]] || die "invalid backup checksum entry"
    validate_secret_relative_path "${path}"
    [[ -z "${checksummed[${path}]+present}" ]] || die "duplicate backup checksum path: ${path}"
    [[ -f "${backup}/${path}" && ! -L "${backup}/${path}" ]] ||
      die "backup checksum target is unavailable: ${path}"
    checksummed["${path}"]=1
  done <"${backup}/SHA256SUMS"
  checksummed_count="${#checksummed[@]}"
  actual_file_count="$(find "${backup}" -type f ! -path "${backup}/SHA256SUMS" | wc -l)"
  [[ "${checksummed_count}" == "${actual_file_count}" ]] ||
    die "backup checksum manifest omits files"
  while IFS= read -r path; do
    [[ -n "${checksummed[${path}]+present}" ]] ||
      die "backup checksum manifest omits ${path}"
  done < <(find "${backup}" -type f ! -path "${backup}/SHA256SUMS" -printf '%P\n' | LC_ALL=C sort)
  (cd "${backup}" && sha256sum --check --quiet SHA256SUMS) || die "backup checksum verification failed"

  expected="$(jq -r '.schema_identity' "${backup}/metadata.json")"
  actual="sha256:$(sha256sum "${backup}/schema.sql" | awk '{print $1}')"
  [[ "${actual}" == "${expected}" ]] || die "backup schema identity mismatch"
  expected="$(jq -r '.row_counts_hash' "${backup}/metadata.json")"
  actual="$(sha256sum "${backup}/row-counts.tsv" | awk '{print $1}')"
  [[ "${actual}" == "${expected}" ]] || die "backup row-count manifest hash mismatch"
  table_count="$(wc -l <"${backup}/row-counts.tsv")"
  [[ "${table_count}" == "$(jq -r '.table_count' "${backup}/metadata.json")" ]] ||
    die "backup table count mismatch"
  expected="$(jq -r '.secret_manifest_hash' "${backup}/metadata.json")"
  actual="$(sha256sum "${backup}/secret-manifest.tsv" | awk '{print $1}')"
  [[ "${actual}" == "${expected}" ]] || die "backup secret manifest hash mismatch"
  validate_secret_manifest "${backup}"

  pass "backup contract verified: ${backup}"
}

restore_backup() {
  local backup="${1:-}" pod source_database target_database active_restore=0
  local restored_version api_replicas worker_replicas
  for command_name in kubectl jq sha256sum realpath cmp; do
    require_command "${command_name}"
  done
  validate_fixed_boundaries
  ensure_private_directories
  [[ -n "${backup}" ]] || die "BACKUP is required"
  backup="$(realpath -e "${backup}")"
  validate_backup "${backup}"
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  pod="$(mysql_pod)"
  source_database="$(jq -r '.database' "${backup}/metadata.json")"
  target_database="${CLOUDOPS_RESTORE_DATABASE:-${source_database}}"
  validate_identifier "${target_database}"
  if [[ "${target_database}" != "${DATABASE_NAME}" ]]; then
    [[ "${target_database}" =~ ^cloudops_restore_[a-zA-Z0-9_]+$ ]] ||
      die "non-active restore database must use the cloudops_restore_* boundary"
    [[ "${target_database}" != "${RESTORE_STAGING_DATABASE}" ]] ||
      die "restore target is reserved for staging validation: ${RESTORE_STAGING_DATABASE}"
  fi
  [[ "${CONFIRM_RESTORE:-}" == "RESTORE:${target_database}" ]] ||
    die "set CONFIRM_RESTORE=RESTORE:${target_database} to replace the exact target database"

  RESTORE_CLEANUP_POD="${pod}"
  RESTORE_CLEANUP_STAGING=1
  trap restore_exit_cleanup EXIT

  note "validating backup in isolated database ${RESTORE_STAGING_DATABASE}"
  mysql_exec "${pod}" -e "DROP DATABASE IF EXISTS \`${RESTORE_STAGING_DATABASE}\`; CREATE DATABASE \`${RESTORE_STAGING_DATABASE}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
  mysql_import "${pod}" "${RESTORE_STAGING_DATABASE}" "${backup}/database.sql"
  restored_version="$(schema_version "${pod}" "${RESTORE_STAGING_DATABASE}")"
	[[ "${restored_version}" == "${LATEST_SCHEMA_VERSION}" ]] ||
		die "staging restore schema_version=${restored_version}; expected ${LATEST_SCHEMA_VERSION}"
  verify_row_counts "${pod}" "${RESTORE_STAGING_DATABASE}" "${backup}/row-counts.tsv"

  if [[ "${target_database}" == "${DATABASE_NAME}" ]]; then
    active_restore=1
    api_replicas="$(kube -n "${NAMESPACE}" get deployment cloudops-api -o jsonpath='{.spec.replicas}')"
    worker_replicas="$(kube -n "${NAMESPACE}" get deployment cloudops-worker -o jsonpath='{.spec.replicas}')"
    [[ "${api_replicas}" =~ ^[0-9]+$ && "${worker_replicas}" =~ ^[0-9]+$ ]] ||
      die "could not determine current API/Worker replica counts"
    RESTORE_API_REPLICAS="${api_replicas}"
    RESTORE_WORKER_REPLICAS="${worker_replicas}"
    RESTORE_RECOVER_RUNTIME=1
    kube -n "${NAMESPACE}" scale deployment/cloudops-api deployment/cloudops-worker --replicas=0 >/dev/null
    wait_for_deployment_scale_zero cloudops-api
    wait_for_deployment_scale_zero cloudops-worker
    note "creating rollback backup from the quiesced active database"
    create_backup
  fi

  note "replacing MySQL database ${target_database} from ${backup}"
  mysql_exec "${pod}" -e "DROP DATABASE IF EXISTS \`${target_database}\`; CREATE DATABASE \`${target_database}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
  mysql_import "${pod}" "${target_database}" "${backup}/database.sql"
  restored_version="$(schema_version "${pod}" "${target_database}")"
	[[ "${restored_version}" == "${LATEST_SCHEMA_VERSION}" ]] ||
		die "target restore schema_version=${restored_version}; expected ${LATEST_SCHEMA_VERSION}"
  verify_row_counts "${pod}" "${target_database}" "${backup}/row-counts.tsv"

  if [[ "${active_restore}" == "1" ]]; then
    restore_operational_secrets "${backup}"
    restore_runtime_replicas "${api_replicas}" "${worker_replicas}"
    RESTORE_RECOVER_RUNTIME=0
  fi
  mysql_exec "${pod}" -e "DROP DATABASE IF EXISTS \`${RESTORE_STAGING_DATABASE}\`" >/dev/null
  RESTORE_CLEANUP_STAGING=0
  trap - EXIT
  pass "restore=${target_database} row_counts=verified schema_version=${restored_version}"
}

backup_summary() {
  local backup_count=0 latest_backup=none latest_path
  local format=unavailable contract=unavailable schema=unavailable
  if [[ -d "${BACKUP_ROOT}" ]]; then
    backup_count="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d ! -name '.incomplete.*' | wc -l)"
    latest_backup="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d ! -name '.incomplete.*' -printf '%f\n' | sort | tail -1)"
    [[ -n "${latest_backup}" ]] || latest_backup=none
  fi
  if [[ "${latest_backup}" != "none" ]]; then
    latest_path="${BACKUP_ROOT}/${latest_backup}"
    if [[ -f "${latest_path}/metadata.json" ]] && jq -e . "${latest_path}/metadata.json" >/dev/null 2>&1; then
      format="$(jq -r '.format_version // "unknown"' "${latest_path}/metadata.json")"
      contract="$(jq -r '.contract // "unknown"' "${latest_path}/metadata.json")"
      schema="$(jq -r '.schema_version // "unknown"' "${latest_path}/metadata.json")"
    else
      format=invalid
      contract=invalid
      schema=invalid
    fi
  fi
  printf 'backup_count=%s\nlatest_backup=%s\nlatest_backup_format=%s\nlatest_backup_contract=%s\nlatest_backup_schema_version=%s\n' \
    "${backup_count}" "${latest_backup}" "${format}" "${contract}" "${schema}"
}

secret_directory_summary() {
  local bootstrap_count=0 operational_count=0 bootstrap_state=missing operational_state=missing
  if [[ -d "${BOOTSTRAP_SECRET_DIR}" && ! -L "${BOOTSTRAP_SECRET_DIR}" ]]; then
    bootstrap_state=available
    bootstrap_count="$(find "${BOOTSTRAP_SECRET_DIR}" -type f | wc -l)"
  fi
  if [[ -d "${SECRET_DIR}" && ! -L "${SECRET_DIR}" ]]; then
    operational_state=available
    operational_count="$(find "${SECRET_DIR}" -type f | wc -l)"
  fi
  printf 'bootstrap_secrets=%s\nbootstrap_secret_files=%s\noperational_secrets=%s\noperational_secret_files=%s\n' \
    "${bootstrap_state}" "${bootstrap_count}" "${operational_state}" "${operational_count}"
}

validate_private_directory() {
  local directory="$1"
  [[ -d "${directory}" && ! -L "${directory}" ]] || return 1
  [[ "$(stat -c '%a' "${directory}")" == "700" ]] || return 1
  [[ -z "$(find "${directory}" -type l -print -quit)" ]] || return 1
  [[ -z "$(find "${directory}" -mindepth 1 -perm /077 -print -quit)" ]]
}

validate_bootstrap_secret_directory() {
  local filename actual_count
  validate_private_directory "${BOOTSTRAP_SECRET_DIR}" || return 1
  for filename in \
    mysql-root-password mysql-api-password mysql-worker-password \
    mysql-migrate-password webhook-token; do
    [[ -f "${BOOTSTRAP_SECRET_DIR}/${filename}" &&
       ! -L "${BOOTSTRAP_SECRET_DIR}/${filename}" &&
       "$(stat -c '%a' "${BOOTSTRAP_SECRET_DIR}/${filename}")" == "600" ]] || return 1
  done
  actual_count="$(find "${BOOTSTRAP_SECRET_DIR}" -type f | wc -l)"
  [[ "${actual_count}" == "5" ]]
}

print_status() {
  local pod version pid grafana_pid tempo_pid gateway
  require_command kubectl
  require_command jq
  validate_fixed_boundaries
  validate_local_port
  validate_identifier "${DATABASE_NAME}"
  printf 'cluster_name=%s\nkube_context=%s\nnamespace=%s\nrelease=%s\nurl=%s\ngrafana_url=%s\ntempo_url=%s\n' \
    "${CLUSTER_NAME}" "${KUBE_CONTEXT}" "${NAMESPACE}" "${RELEASE_NAME}" "${LOCAL_URL}" "${GRAFANA_LOCAL_URL}" "${TEMPO_LOCAL_URL}"
  secret_directory_summary
  if ! context_exists; then
    printf 'runtime=unavailable\nreason=context_not_found\n'
    backup_summary
    return 1
  fi
  if ! release_exists; then
    printf 'runtime=unavailable\nreason=release_not_found\n'
    backup_summary
    return 1
  fi
  printf 'runtime=available\n'
  helm --kube-context "${KUBE_CONTEXT}" -n "${NAMESPACE}" status "${RELEASE_NAME}" \
    -o json | jq -r '"release_status=" + .info.status + "\nrelease_revision=" + (.version|tostring)'
  kube -n "${NAMESPACE}" get deployments,statefulsets,daemonsets,jobs \
    -l app.kubernetes.io/instance="${RELEASE_NAME}" \
    -o custom-columns='KIND:.kind,NAME:.metadata.name,READY:.status.readyReplicas,SUCCEEDED:.status.succeeded' --no-headers 2>/dev/null || true
  pod="$(mysql_pod_if_running || true)"
  if [[ -n "${pod}" ]] && database_exists "${pod}" "${DATABASE_NAME}"; then
    version="$(schema_version "${pod}" "${DATABASE_NAME}")"
    printf 'mysql=available\nmysql_pod=%s\nschema_version=%s\n' "${pod}" "${version}"
  else
    printf 'mysql=stopped_or_unavailable\n'
  fi
  gateway="$(kube -n "${NAMESPACE}" get deployment cloudops-worker -o json 2>/dev/null |
    jq -r '[.spec.template.spec.containers[]?.env[]? | select(.name=="PROVIDER_GATEWAY_ENABLED") | .value][0] // "unavailable"' || true)"
  printf 'provider_gateway=%s\n' "${gateway:-unavailable}"
  pid="$(port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && port_forward_process_matches "${pid}"; then
    printf 'loopback=available\nloopback_pid=%s\n' "${pid}"
  else
    printf 'loopback=stopped\n'
  fi
  grafana_pid="$(grafana_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${grafana_pid}" ]] && grafana_port_forward_process_matches "${grafana_pid}"; then
    printf 'grafana_loopback=available\ngrafana_loopback_pid=%s\n' "${grafana_pid}"
  else
    printf 'grafana_loopback=stopped\n'
  fi
  tempo_pid="$(tempo_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${tempo_pid}" ]] && tempo_port_forward_process_matches "${tempo_pid}"; then
    printf 'tempo_loopback=available\ntempo_loopback_pid=%s\n' "${tempo_pid}"
  else
    printf 'tempo_loopback=stopped\n'
  fi
  kube -n "${NAMESPACE}" get pvc -l app.kubernetes.io/instance="${RELEASE_NAME}" \
    -o custom-columns='PVC:.metadata.name,STATUS:.status.phase,CAPACITY:.status.capacity.storage' --no-headers 2>/dev/null || true
  scenario_status || true
  backup_summary
}

local_logs() {
  local component="${1:-}" lines="${LINES:-200}" job
  validate_fixed_boundaries
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  [[ "${lines}" =~ ^[1-9][0-9]{0,3}$ ]] || die "LINES must be between 1 and 9999"
  case "${component:-all}" in
    api) kube -n "${NAMESPACE}" logs deployment/cloudops-api --all-containers --tail="${lines}" ;;
    worker) kube -n "${NAMESPACE}" logs deployment/cloudops-worker --all-containers --tail="${lines}" ;;
    prometheus) kube -n "${NAMESPACE}" logs deployment/prometheus --all-containers --tail="${lines}" ;;
    alertmanager) kube -n "${NAMESPACE}" logs deployment/alertmanager --all-containers --tail="${lines}" ;;
    grafana) kube -n "${NAMESPACE}" logs deployment/grafana --all-containers --tail="${lines}" ;;
    elasticsearch) kube -n "${NAMESPACE}" logs statefulset/elasticsearch --all-containers --tail="${lines}" ;;
    filebeat) kube -n "${NAMESPACE}" logs daemonset/filebeat --all-containers --tail="${lines}" ;;
    tempo) kube -n "${NAMESPACE}" logs statefulset/tempo --all-containers --tail="${lines}" ;;
    otel-collector) kube -n "${NAMESPACE}" logs deployment/otel-collector --all-containers --tail="${lines}" ;;
    mysql) kube -n "${NAMESPACE}" logs statefulset/mysql --all-containers --tail="${lines}" ;;
    migrate)
      job="$(kube -n "${NAMESPACE}" get jobs -l app.kubernetes.io/component=migrate \
        --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null || true)"
      [[ -n "${job}" ]] || die "no migration Job is retained"
      kube -n "${NAMESPACE}" logs "job/${job}" --all-containers --tail="${lines}"
      ;;
    all)
      for component in api worker prometheus alertmanager grafana elasticsearch filebeat tempo otel-collector migrate mysql; do
        printf '== %s ==\n' "${component}"
        local_logs "${component}" || true
      done
      ;;
    *) die "COMPONENT must be api, worker, prometheus, alertmanager, grafana, elasticsearch, filebeat, tempo, otel-collector, migrate, mysql, or empty" ;;
  esac
}

resume_filebeat() {
  local suspended
  kube -n "${NAMESPACE}" get daemonset/filebeat >/dev/null 2>&1 || return 0
  suspended="$(kube -n "${NAMESPACE}" get daemonset/filebeat -o jsonpath='{.spec.template.spec.nodeSelector.cloudops\.io/runtime-stopped}' 2>/dev/null || true)"
  if [[ "${suspended}" == "true" ]]; then
    kube -n "${NAMESPACE}" patch daemonset/filebeat --type=json \
      -p='[{"op":"remove","path":"/spec/template/spec/nodeSelector/cloudops.io~1runtime-stopped"}]' >/dev/null
  fi
}

suspend_filebeat() {
  local remaining
  kube -n "${NAMESPACE}" get daemonset/filebeat >/dev/null 2>&1 || return 0
  kube -n "${NAMESPACE}" patch daemonset/filebeat --type=merge \
    -p='{"spec":{"template":{"spec":{"nodeSelector":{"cloudops.io/runtime-stopped":"true"}}}}}' >/dev/null
  for _attempt in {1..60}; do
    remaining="$(kube -n "${NAMESPACE}" get pods -l app.kubernetes.io/name=filebeat --no-headers 2>/dev/null | wc -l)"
    [[ "${remaining}" == "0" ]] && return 0
    sleep 0.5
  done
  die "Filebeat did not stop within the bounded shutdown window"
}

local_restart() {
  validate_fixed_boundaries
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  release_exists || die "CloudOps release is unavailable; run make local-up"
  kube -n "${NAMESPACE}" scale statefulset/mysql statefulset/elasticsearch statefulset/tempo --replicas=1 >/dev/null
  kube -n "${NAMESPACE}" rollout status statefulset/mysql --timeout=5m
  kube -n "${NAMESPACE}" rollout status statefulset/elasticsearch --timeout=8m
  kube -n "${NAMESPACE}" rollout status statefulset/tempo --timeout=5m
  kube -n "${NAMESPACE}" scale \
    deployment/cloudops-api deployment/cloudops-worker deployment/prometheus deployment/alertmanager deployment/grafana deployment/otel-collector \
    --replicas=1 >/dev/null
  resume_filebeat
  kube -n "${NAMESPACE}" rollout restart \
    deployment/cloudops-api deployment/cloudops-worker deployment/prometheus deployment/alertmanager deployment/grafana deployment/otel-collector \
    statefulset/elasticsearch statefulset/tempo daemonset/filebeat >/dev/null
  kube -n "${NAMESPACE}" rollout status statefulset/elasticsearch --timeout=8m
  kube -n "${NAMESPACE}" rollout status statefulset/tempo --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/otel-collector --timeout=5m
  kube -n "${NAMESPACE}" rollout status daemonset/filebeat --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-api --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/prometheus --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/alertmanager --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/grafana --timeout=5m
  start_port_forward
  start_grafana_port_forward
  start_tempo_port_forward
  pass "CloudOps runtime restarted with persistent state preserved"
}

local_down() {
  validate_fixed_boundaries
  stop_port_forward
  stop_grafana_port_forward
  stop_tempo_port_forward
  if ! context_exists || ! release_exists; then
    note "CloudOps runtime is already stopped"
    return
  fi
  kube -n "${NAMESPACE}" scale \
    deployment/cloudops-api deployment/cloudops-worker deployment/prometheus deployment/alertmanager deployment/grafana deployment/otel-collector \
    --replicas=0 >/dev/null
  suspend_filebeat
  kube -n "${NAMESPACE}" scale statefulset/mysql statefulset/elasticsearch statefulset/tempo --replicas=0 >/dev/null
  pass "CloudOps workloads stopped; PVC and local secrets preserved"
}

local_reset() {
  local pvc_names
  validate_fixed_boundaries
  ensure_private_directories
  [[ "${CONFIRM_RESET:-}" == "RESET:${CLUSTER_NAME}" ]] ||
    die "set CONFIRM_RESET=RESET:${CLUSTER_NAME} to remove CloudOps persistent state"
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  if [[ "${SKIP_BACKUP:-0}" == "1" ]]; then
    [[ "${CONFIRM_RESET_WITHOUT_BACKUP:-}" == "RESET-WITHOUT-BACKUP:${CLUSTER_NAME}" ]] ||
      die "skipping backup also requires CONFIRM_RESET_WITHOUT_BACKUP=RESET-WITHOUT-BACKUP:${CLUSTER_NAME}"
  else
    if [[ -z "$(mysql_pod_if_running || true)" ]]; then
      kube -n "${NAMESPACE}" scale statefulset/mysql --replicas=1 >/dev/null
      kube -n "${NAMESPACE}" rollout status statefulset/mysql --timeout=5m
    fi
    create_backup
  fi
  stop_port_forward
  stop_grafana_port_forward
  stop_tempo_port_forward
	pvc_names="$(kube -n "${NAMESPACE}" get pvc \
		-l app.kubernetes.io/instance="${RELEASE_NAME}" \
    -o name 2>/dev/null || true)"
  helm --kube-context "${KUBE_CONTEXT}" -n "${NAMESPACE}" uninstall "${RELEASE_NAME}" --wait || true
  if [[ -n "${pvc_names}" ]]; then
    while IFS= read -r pvc; do
			case "${pvc}" in
					persistentvolumeclaim/data-mysql-0|persistentvolumeclaim/data-elasticsearch-0|persistentvolumeclaim/data-tempo-0|persistentvolumeclaim/cloudops-data) ;;
				*) die "refusing unexpected PVC reset target: ${pvc}" ;;
			esac
			kube -n "${NAMESPACE}" delete "${pvc}" --ignore-not-found --wait=true
    done <<<"${pvc_names}"
  fi
  kube -n "${NAMESPACE}" delete secret \
    cloudops-mysql-root cloudops-api-database cloudops-worker-database \
    cloudops-migrate-database cloudops-alertmanager-webhook --ignore-not-found >/dev/null
  rm -rf "${BOOTSTRAP_SECRET_DIR}" "${SECRET_DIR}"
  pass "CloudOps persistent state removed; verified backup remains under ${BACKUP_ROOT}"
}

doctor() {
  local failed=0 command_name pid grafana_pid tempo_pid pod latest_backup
  validate_fixed_boundaries
  validate_local_port
  validate_state_directory
  for command_name in docker kind kubectl helm jq openssl sha256sum git realpath curl rg; do
    if command -v "${command_name}" >/dev/null 2>&1; then
      printf 'PASS prerequisite %s\n' "${command_name}"
    else
      printf 'FAIL prerequisite %s missing\n' "${command_name}"
      failed=1
    fi
  done
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    printf 'PASS docker daemon available\n'
  else
    printf 'FAIL docker daemon unavailable\n'
    failed=1
  fi
  if validate_bootstrap_secret_directory; then
    printf 'PASS bootstrap secret directory is private and complete\n'
  else
    printf 'FAIL bootstrap secret directory is missing, incomplete, or unsafe\n'
    failed=1
  fi
  if validate_private_directory "${SECRET_DIR}"; then
    printf 'PASS operational secret directory is private\n'
  else
    printf 'FAIL operational secret directory is missing or unsafe\n'
    failed=1
  fi
  if context_exists; then
    printf 'PASS kubernetes context %s available\n' "${KUBE_CONTEXT}"
  else
    printf 'FAIL kubernetes context %s unavailable\n' "${KUBE_CONTEXT}"
    failed=1
  fi
  if release_exists; then
    printf 'PASS Helm release %s available\n' "${RELEASE_NAME}"
    pod="$(mysql_pod_if_running || true)"
		if [[ -n "${pod}" ]] && [[ "$(schema_version "${pod}" "${DATABASE_NAME}")" == "${LATEST_SCHEMA_VERSION}" ]]; then
			printf 'PASS semantic schema version %s available\n' "${LATEST_SCHEMA_VERSION}"
		else
			printf 'FAIL semantic schema version %s unavailable\n' "${LATEST_SCHEMA_VERSION}"
      failed=1
    fi
  else
    printf 'FAIL Helm release %s unavailable\n' "${RELEASE_NAME}"
    failed=1
  fi
  pid="$(port_forward_pid 2>/dev/null || true)"
  if [[ -n "${pid}" ]] && port_forward_process_matches "${pid}"; then
    printf 'PASS loopback process available pid=%s\n' "${pid}"
  else
    printf 'FAIL loopback process unavailable\n'
    failed=1
  fi
  grafana_pid="$(grafana_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${grafana_pid}" ]] && grafana_port_forward_process_matches "${grafana_pid}" &&
     curl --noproxy '*' --fail --silent --max-time 2 "${GRAFANA_LOCAL_URL}/api/health" >/dev/null 2>&1; then
    printf 'PASS Grafana loopback process available pid=%s\n' "${grafana_pid}"
  else
    printf 'FAIL Grafana loopback process unavailable\n'
    failed=1
  fi
  tempo_pid="$(tempo_port_forward_pid 2>/dev/null || true)"
  if [[ -n "${tempo_pid}" ]] && tempo_port_forward_process_matches "${tempo_pid}" &&
     curl --noproxy '*' --fail --silent --max-time 2 "${TEMPO_LOCAL_URL}/ready" >/dev/null 2>&1; then
    printf 'PASS Tempo loopback process available pid=%s\n' "${tempo_pid}"
  else
    printf 'FAIL Tempo loopback process unavailable\n'
    failed=1
  fi
  if [[ "$(kube -n "${NAMESPACE}" get statefulset/elasticsearch -o json 2>/dev/null | jq -r '(.spec.replicas == 1) and (.status.readyReplicas == 1)' 2>/dev/null || true)" == "true" ]]; then
    printf 'PASS Elasticsearch bounded logs Provider ready\n'
  else
    printf 'FAIL Elasticsearch bounded logs Provider unavailable\n'
    failed=1
  fi
  if [[ "$(kube -n "${NAMESPACE}" get statefulset/tempo -o json 2>/dev/null | jq -r '(.spec.replicas == 1) and (.status.readyReplicas == 1)' 2>/dev/null || true)" == "true" ]]; then
    printf 'PASS Tempo traces Provider ready\n'
  else
    printf 'FAIL Tempo traces Provider unavailable\n'
    failed=1
  fi
  if [[ "$(kube -n "${NAMESPACE}" get deployment/otel-collector -o json 2>/dev/null | jq -r '(.spec.replicas == 1) and (.status.readyReplicas == 1)' 2>/dev/null || true)" == "true" ]]; then
    printf 'PASS OpenTelemetry Collector ready\n'
  else
    printf 'FAIL OpenTelemetry Collector unavailable\n'
    failed=1
  fi
  if [[ "$(kube -n "${NAMESPACE}" get deployment/alertmanager -o json 2>/dev/null | jq -r '(.spec.replicas == 1) and (.status.readyReplicas == 1)' 2>/dev/null || true)" == "true" ]]; then
    printf 'PASS Alertmanager Provider ready\n'
  else
    printf 'FAIL Alertmanager Provider unavailable\n'
    failed=1
  fi
  if [[ "$(kube -n "${NAMESPACE}" get daemonset/filebeat -o json 2>/dev/null | jq -r '(.status.desiredNumberScheduled > 0) and (.status.numberReady == .status.desiredNumberScheduled)' 2>/dev/null || true)" == "true" ]]; then
    printf 'PASS Filebeat Kubernetes log collection ready\n'
  else
    printf 'FAIL Filebeat Kubernetes log collection unavailable\n'
    failed=1
  fi
  backup_summary
  latest_backup="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d ! -name '.incomplete.*' -printf '%p\n' 2>/dev/null | sort | tail -1)"
  if [[ -n "${latest_backup}" ]] && (validate_backup "${latest_backup}"); then
    printf 'PASS latest backup uses verified %s format %s\n' "${BACKUP_CONTRACT}" "${BACKUP_FORMAT_VERSION}"
  else
    printf 'FAIL latest backup is unavailable or does not satisfy the current semantic contract\n'
    failed=1
  fi
  if [[ "${failed}" == "0" ]]; then
    pass "local doctor"
  else
    die "local doctor found actionable failures"
  fi
}

usage() {
  printf 'usage: %s {up|open|status|logs [COMPONENT]|restart|doctor|down|backup|restore BACKUP|reset|scenario-up|scenario-status|scenario-down}\n' "$0" >&2
  exit 2
}

validate_fixed_boundaries

case "${1:-}" in
  up) [[ "$#" == "1" ]] || usage; local_up ;;
  open) [[ "$#" == "1" ]] || usage; local_open ;;
  status) [[ "$#" == "1" ]] || usage; print_status ;;
  logs) [[ "$#" -le "2" ]] || usage; local_logs "${2:-}" ;;
  restart) [[ "$#" == "1" ]] || usage; local_restart ;;
  doctor) [[ "$#" == "1" ]] || usage; doctor ;;
  down) [[ "$#" == "1" ]] || usage; local_down ;;
  backup) [[ "$#" == "1" ]] || usage; create_backup ;;
  restore) [[ "$#" == "2" ]] || usage; restore_backup "$2" ;;
  reset) [[ "$#" == "1" ]] || usage; local_reset ;;
  scenario-up) [[ "$#" == "1" ]] || usage; scenario_up ;;
  scenario-status) [[ "$#" == "1" ]] || usage; scenario_status ;;
  scenario-down) [[ "$#" == "1" ]] || usage; scenario_down ;;
  *) usage ;;
esac
