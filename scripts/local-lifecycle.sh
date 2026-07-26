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
PORT_FORWARD_PID_FILE="${RUNTIME_DIR}/api-port-forward.pid"
PORT_FORWARD_LOG="${RUNTIME_DIR}/api-port-forward.log"
KIND_NODE_IMAGE="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"
MYSQL_IMAGE="mysql:8.0.46"
MYSQL_REPOSITORY="mysql"
MYSQL_PLATFORM="linux/amd64"
MYSQL_DIGEST="sha256:62fb722c78b24245ddff1796a0fcee4a49cc5b87e0aaaf20c92d1da9e0a2497b"
MIN_INOTIFY_INSTANCES=512
BACKUP_FORMAT_VERSION=2
BACKUP_CONTRACT="cloudops-semantic"
RESTORE_STAGING_DATABASE="cloudops_restore_staging"

RESTORE_CLEANUP_POD=""
RESTORE_CLEANUP_STAGING=0
RESTORE_RECOVER_RUNTIME=0
RESTORE_API_REPLICAS=0
RESTORE_WORKER_REPLICAS=0

die() {
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

build_application_images() {
  local exact_sha source_url target
  exact_sha="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
  source_url="$(git -C "${ROOT_DIR}" remote get-url origin 2>/dev/null || printf 'local')"
  for target in api worker migrate; do
    note "building cloudops-${target}:local"
    docker build \
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
  for image in cloudops-api:local cloudops-worker:local cloudops-migrate:local "${MYSQL_IMAGE}"; do
    kind load docker-image "${image}" --name "${CLUSTER_NAME}"
  done
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
  local helm_args current_status
  helm_args=(
    --kube-context "${KUBE_CONTEXT}"
    --namespace "${NAMESPACE}"
    --values "${VALUES_FILE}"
    --timeout 8m
  )
  current_status="$(release_status 2>/dev/null || true)"
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

  note "reconciling API, Worker, Migrate, and MySQL"
  helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" "${helm_args[@]}" \
    --wait --wait-for-jobs
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-api --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m
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

local_up() {
  preflight
  ensure_private_directories
  ensure_cluster
  ensure_namespaces
  ensure_runtime_secrets
  build_application_images
  load_runtime_images
  install_runtime
  start_port_forward
  print_status
}

local_open() {
  validate_fixed_boundaries
  validate_local_port
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  release_exists || die "CloudOps release is unavailable; run make local-up"
  start_port_forward
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
  local backup="$1" staged previous
  staged="$(mktemp -d "${STATE_DIR}/.secrets.restore.XXXXXX")"
  previous="${STATE_DIR}/.secrets.previous.$$"
  [[ ! -e "${previous}" ]] || die "stale secret restore directory exists: ${previous}"
  cp -a "${backup}/secret-files/." "${staged}/"
  chmod -R go-rwx "${staged}"
  mv "${SECRET_DIR}" "${previous}"
  mv "${staged}" "${SECRET_DIR}"
  chmod 700 "${SECRET_DIR}"
  rm -rf "${previous}"
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
  [[ "${version}" == "1" ]] ||
    die "refusing non-semantic backup schema_version=${version}; expected 1"

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
  [[ "${version}" == "1" ]] || die "backup schema is not the semantic baseline: ${version}"
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
  for command_name in kubectl jq sha256sum realpath; do
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
  [[ "${restored_version}" == "1" ]] ||
    die "staging restore schema_version=${restored_version}; expected 1"
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
  [[ "${restored_version}" == "1" ]] ||
    die "target restore schema_version=${restored_version}; expected 1"
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
  local pod version pid gateway
  require_command kubectl
  require_command jq
  validate_fixed_boundaries
  validate_local_port
  validate_identifier "${DATABASE_NAME}"
  printf 'cluster_name=%s\nkube_context=%s\nnamespace=%s\nrelease=%s\nurl=%s\n' \
    "${CLUSTER_NAME}" "${KUBE_CONTEXT}" "${NAMESPACE}" "${RELEASE_NAME}" "${LOCAL_URL}"
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
  kube -n "${NAMESPACE}" get deployments,statefulsets,jobs \
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
  kube -n "${NAMESPACE}" get pvc -l app.kubernetes.io/instance="${RELEASE_NAME}" \
    -o custom-columns='PVC:.metadata.name,STATUS:.status.phase,CAPACITY:.status.capacity.storage' --no-headers 2>/dev/null || true
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
    mysql) kube -n "${NAMESPACE}" logs statefulset/mysql --all-containers --tail="${lines}" ;;
    migrate)
      job="$(kube -n "${NAMESPACE}" get jobs -l app.kubernetes.io/component=migrate \
        --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null || true)"
      [[ -n "${job}" ]] || die "no migration Job is retained"
      kube -n "${NAMESPACE}" logs "job/${job}" --all-containers --tail="${lines}"
      ;;
    all)
      for component in api worker migrate mysql; do
        printf '== %s ==\n' "${component}"
        local_logs "${component}" || true
      done
      ;;
    *) die "COMPONENT must be api, worker, migrate, mysql, or empty" ;;
  esac
}

local_restart() {
  validate_fixed_boundaries
  context_exists || die "Kubernetes context is unavailable: ${KUBE_CONTEXT}"
  release_exists || die "CloudOps release is unavailable; run make local-up"
  kube -n "${NAMESPACE}" scale statefulset/mysql --replicas=1 >/dev/null
  kube -n "${NAMESPACE}" rollout status statefulset/mysql --timeout=5m
  kube -n "${NAMESPACE}" scale deployment/cloudops-api deployment/cloudops-worker --replicas=1 >/dev/null
  kube -n "${NAMESPACE}" rollout restart deployment/cloudops-api deployment/cloudops-worker >/dev/null
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-api --timeout=5m
  kube -n "${NAMESPACE}" rollout status deployment/cloudops-worker --timeout=5m
  start_port_forward
  pass "CloudOps runtime restarted with persistent state preserved"
}

local_down() {
  validate_fixed_boundaries
  stop_port_forward
  if ! context_exists || ! release_exists; then
    note "CloudOps runtime is already stopped"
    return
  fi
  kube -n "${NAMESPACE}" scale deployment/cloudops-api deployment/cloudops-worker --replicas=0 >/dev/null
  kube -n "${NAMESPACE}" scale statefulset/mysql --replicas=0 >/dev/null
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
  pvc_names="$(kube -n "${NAMESPACE}" get pvc \
    -l app.kubernetes.io/instance="${RELEASE_NAME}",app.kubernetes.io/component=database \
    -o name 2>/dev/null || true)"
  helm --kube-context "${KUBE_CONTEXT}" -n "${NAMESPACE}" uninstall "${RELEASE_NAME}" --wait || true
  if [[ -n "${pvc_names}" ]]; then
    while IFS= read -r pvc; do
      [[ "${pvc}" == persistentvolumeclaim/data-mysql-0 ]] || die "refusing unexpected PVC reset target: ${pvc}"
      kube -n "${NAMESPACE}" delete "${pvc}" --wait=true
    done <<<"${pvc_names}"
  fi
  kube -n "${NAMESPACE}" delete secret \
    cloudops-mysql-root cloudops-api-database cloudops-worker-database \
    cloudops-migrate-database cloudops-alertmanager-webhook --ignore-not-found >/dev/null
  rm -rf "${BOOTSTRAP_SECRET_DIR}" "${SECRET_DIR}"
  pass "CloudOps persistent state removed; verified backup remains under ${BACKUP_ROOT}"
}

doctor() {
  local failed=0 command_name pid pod latest_backup
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
    if [[ -n "${pod}" ]] && [[ "$(schema_version "${pod}" "${DATABASE_NAME}")" == "1" ]]; then
      printf 'PASS semantic schema version 1 available\n'
    else
      printf 'FAIL semantic schema version 1 unavailable\n'
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
  printf 'usage: %s {up|open|status|logs [COMPONENT]|restart|doctor|down|backup|restore BACKUP|reset}\n' "$0" >&2
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
  *) usage ;;
esac
