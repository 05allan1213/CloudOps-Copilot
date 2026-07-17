#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

MODE="${1:-run}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-cloudops-v2-demo}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-cloudops-demo}"
KIND_CONTEXT="kind-${KIND_CLUSTER_NAME}"
DEMO_NAMESPACE="cloudops-demo"
DEMO_WORKLOAD="cloudops-demo-workload"
SERVER_WEB_HOST_PORT="${SERVER_WEB_HOST_PORT:-18080}"
DEMO_NODE_PORT="${DEMO_NODE_PORT:-30082}"
DEMO_MYSQL_HOST_PORT="${DEMO_MYSQL_HOST_PORT:-33307}"
DEMO_BASE_URL="http://127.0.0.1:${SERVER_WEB_HOST_PORT}"
REVISION="$(git rev-parse HEAD)"
FAST_DEMO_IMAGE_TAG="${FAST_DEMO_IMAGE_TAG:-${REVISION:0:12}-step3}"
MYSQL_DATABASE="${MYSQL_DATABASE:-server_monitor_demo_$(date -u +%Y%m%d%H%M%S)}"
KUBECONFIG_FILE="${ROOT_DIR}/docker/kubeconfig"
CURRENT_STAGE="initialization"
FAILURE_REASON=""
START_EPOCH="$(date +%s)"

export COMPOSE_PROJECT_NAME FAST_DEMO_IMAGE_TAG MYSQL_DATABASE
export DEMO_MYSQL_HOST_PORT FAST_DEMO_REVISION="${REVISION}" SERVER_WEB_HOST_PORT
export REDIS_HOST_PORT="${REDIS_HOST_PORT:-16379}"
export JAEGER_UI_HOST_PORT="${JAEGER_UI_HOST_PORT:-16687}"
export OTLP_GRPC_HOST_PORT="${OTLP_GRPC_HOST_PORT:-14317}"
export OTLP_HTTP_HOST_PORT="${OTLP_HTTP_HOST_PORT:-14318}"
export PROMETHEUS_HOST_PORT="${PROMETHEUS_HOST_PORT:-19090}"
export KAFKA_HOST_PORT="${KAFKA_HOST_PORT:-19092}"
export ALERTMANAGER_HOST_PORT="${ALERTMANAGER_HOST_PORT:-19093}"

COMPOSE=(docker compose --env-file .env.example -f docker-compose.yml -f docker-compose.fast-demo.yml)

stage() {
  CURRENT_STAGE=$2
  printf '\n[%s/8] %s\n' "$1" "$2"
}

pass() { printf '[PASS] %s\n' "$*"; }

fail() {
  FAILURE_REASON="$*"
  printf '[FAIL] %s\n' "$*" >&2
  return 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

diagnostics() {
  local container_id service
  printf '\nDEMO_STATUS=FAIL\n' >&2
  printf 'FAILED_STAGE=%s\n' "${CURRENT_STAGE}" >&2
  printf 'FAILED_COMMAND=%s\n' "${1:-unknown}" >&2
  printf 'FAILURE_REASON=%s\n' "${FAILURE_REASON:-unknown}" >&2
  printf 'SERVICE_STATUS_BEGIN\n' >&2
  docker ps -a --filter "label=com.docker.compose.project=${COMPOSE_PROJECT_NAME}" --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' >&2 || true
  printf 'SERVICE_STATUS_END\n' >&2
  for service in server-web prometheus alertmanager mysql kafka redis; do
    printf 'LOG_TAIL_%s_BEGIN\n' "${service}" >&2
    container_id="$(docker ps -aq --filter "label=com.docker.compose.project=${COMPOSE_PROJECT_NAME}" --filter "label=com.docker.compose.service=${service}" | head -n1)"
    if [[ -n "${container_id}" ]]; then
      docker logs --tail=60 "${container_id}" >&2 2>/dev/null || true
    fi
    printf 'LOG_TAIL_%s_END\n' "${service}" >&2
  done
  kubectl --context "${KIND_CONTEXT}" -n "${DEMO_NAMESPACE}" get pods,deployments,services 2>/dev/null >&2 || true
  printf 'SAFE_RETRY=make demo-v2\n' >&2
  printf 'SAFE_CLEAN=make demo-v2-clean\n' >&2
}

on_error() {
  local exit_code=$1
  local failed_command=$2
  trap - ERR
  diagnostics "${failed_command}"
  exit "${exit_code}"
}
trap 'on_error "$?" "$BASH_COMMAND"' ERR

wait_http() {
  local url=$1
  local attempts=${2:-90}
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if curl --noproxy '*' --fail --silent --show-error "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_for_json_value() {
  local url=$1
  local filter=$2
  local attempts=${3:-90}
  local payload value
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    payload="$(curl --fail --silent --show-error "${url}" 2>/dev/null || true)"
    value="$(jq -r "${filter} // empty" <<<"${payload}" 2>/dev/null || true)"
    if [[ -n "${value}" && "${value}" != "null" ]]; then
      printf '%s' "${value}"
      return 0
    fi
    sleep 2
  done
  return 1
}

check_required_files() {
  local file
  for file in \
    docker-compose.yml \
    docker-compose.fast-demo.yml \
    docker/fast-demo/alertmanager.yml \
    docker/fast-demo/alerts.yml \
    docker/fast-demo/prometheus.yml \
    docker/fast-demo/rbac.yaml \
    docker/fast-demo/workload.yaml \
    server-web/migrations/00001_incident_foundation.sql \
    server-web/migrations/00006_observability_verification_postmortem.sql; do
    [[ -f "${file}" ]] || fail "required file is missing: ${file}"
  done
}

check_dependencies() {
  local command
  for command in docker kind kubectl curl jq go git sed awk; do
    require_command "${command}"
  done
  docker info >/dev/null 2>&1 || fail "Docker daemon is unavailable"
  check_required_files
  "${COMPOSE[@]}" config --quiet
}

report_port_owners() {
  local port owners
  for port in "${SERVER_WEB_HOST_PORT}" "${DEMO_MYSQL_HOST_PORT}" "${REDIS_HOST_PORT}" "${PROMETHEUS_HOST_PORT}" "${KAFKA_HOST_PORT}" "${ALERTMANAGER_HOST_PORT}"; do
    owners="$(docker ps --format '{{.Names}} {{.Ports}}' | awk -v port=":${port}->" 'index($0, port) {print $1}' | paste -sd, -)"
    if [[ -n "${owners}" ]]; then
      printf '[INFO] port %s currently published by %s\n' "${port}" "${owners}"
    else
      printf '[PASS] port %s has no Docker publisher\n' "${port}"
    fi
  done
}

run_check() {
  CURRENT_STAGE="preflight check"
  check_dependencies
  require_command helm
  docker compose --env-file .env.example -f docker-compose.yml config --quiet
  "${COMPOSE[@]}" config --quiet
  helm lint charts/server-monitor
  helm template server-monitor charts/server-monitor >/dev/null
  bash -n scripts/run-v2-demo.sh
  if command -v shellcheck >/dev/null 2>&1; then
    shellcheck scripts/run-v2-demo.sh
  fi
  report_port_owners
  if kind get clusters 2>/dev/null | awk -v name="${KIND_CLUSTER_NAME}" '$0 == name {found=1} END {exit !found}'; then
    kubectl --context "${KIND_CONTEXT}" cluster-info >/dev/null
    pass "existing disposable kind cluster is reachable"
  else
    printf '[INFO] disposable kind cluster will be created by make demo-v2\n'
  fi
  printf '\nDEMO_CHECK_STATUS=PASS\n'
}

run_clean() {
  CURRENT_STAGE="cleaning disposable Demo resources"
  check_dependencies
  "${COMPOSE[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  if kind get clusters 2>/dev/null | awk -v name="${KIND_CLUSTER_NAME}" '$0 == name {found=1} END {exit !found}'; then
    kind delete cluster --name "${KIND_CLUSTER_NAME}"
  fi
  rm -f "${KUBECONFIG_FILE}"
  printf 'DEMO_CLEAN_STATUS=PASS\n'
  printf 'CLEANED_COMPOSE_PROJECT=%s\n' "${COMPOSE_PROJECT_NAME}"
  printf 'CLEANED_KIND_CLUSTER=%s\n' "${KIND_CLUSTER_NAME}"
}

case "${MODE}" in
  --check) run_check; exit 0 ;;
  --clean) run_clean; exit 0 ;;
  run|"") ;;
  *) fail "unsupported mode: ${MODE}" ;;
esac

stage 1 "Checking local dependencies"
check_dependencies
pass "dependencies, required files and Compose rendering"

stage 2 "Preparing disposable infrastructure"
"${COMPOSE[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
if ! kind get clusters 2>/dev/null | awk -v name="${KIND_CLUSTER_NAME}" '$0 == name {found=1} END {exit !found}'; then
  kind create cluster --name "${KIND_CLUSTER_NAME}" --wait 90s
fi
kubectl --context "${KIND_CONTEXT}" apply -f docker/fast-demo/rbac.yaml >/dev/null

token="$(kubectl --context "${KIND_CONTEXT}" -n "${DEMO_NAMESPACE}" create token demo-operator --duration=2h)"
kind get kubeconfig --name "${KIND_CLUSTER_NAME}" |
  sed -E "s|server: https://[^:]+:[0-9]+|server: https://${KIND_CLUSTER_NAME}-control-plane:6443|g" >"${KUBECONFIG_FILE}"
kubectl config --kubeconfig "${KUBECONFIG_FILE}" set-credentials demo-operator --token="${token}" >/dev/null
kubectl config --kubeconfig "${KUBECONFIG_FILE}" set-context "${KIND_CONTEXT}" --cluster="${KIND_CONTEXT}" --user=demo-operator --namespace="${DEMO_NAMESPACE}" >/dev/null
kubectl config --kubeconfig "${KUBECONFIG_FILE}" use-context "${KIND_CONTEXT}" >/dev/null
kubectl config --kubeconfig "${KUBECONFIG_FILE}" unset "users.${KIND_CONTEXT}" >/dev/null 2>&1 || true
# The non-root application container reads this disposable bind mount once
# during startup. Restrict it again immediately after readiness succeeds.
chmod 644 "${KUBECONFIG_FILE}"
demo_subject="system:serviceaccount:${DEMO_NAMESPACE}:demo-operator"
kubectl --context "${KIND_CONTEXT}" auth can-i update deployments/scale -n "${DEMO_NAMESPACE}" --as="${demo_subject}" | grep -qx yes || fail "demo service account lacks scoped write permission"
outside_write="$(kubectl --context "${KIND_CONTEXT}" auth can-i update deployments/scale -n default --as="${demo_subject}" 2>/dev/null || true)"
[[ "${outside_write}" == "no" ]] || fail "demo service account unexpectedly writes outside the demo namespace"

docker build -q -t cloudops-demo/workload:v2-demo -f docker/fast-demo/workload.Dockerfile . >/dev/null
kind load docker-image cloudops-demo/workload:v2-demo --name "${KIND_CLUSTER_NAME}" >/dev/null
kubectl --context "${KIND_CONTEXT}" apply -f docker/fast-demo/workload.yaml >/dev/null
kubectl --context "${KIND_CONTEXT}" -n "${DEMO_NAMESPACE}" rollout status deployment/"${DEMO_WORKLOAD}" --timeout=90s >/dev/null
kind_node_ip="$(docker inspect "${KIND_CLUSTER_NAME}-control-plane" --format '{{(index .NetworkSettings.Networks "kind").IPAddress}}')"
workload_url="http://${kind_node_ip}:${DEMO_NODE_PORT}/"
wait_http "${workload_url}metrics" 30 || fail "workload NodePort did not become ready"
pass "kind cluster, guarded RBAC and workload are ready"

stage 3 "Starting CloudOps services"
BUILDKIT_PROGRESS=plain "${COMPOSE[@]}" build server-web
"${COMPOSE[@]}" up -d mysql redis kafka kafka-init jaeger
for ((attempt = 1; attempt <= 60; attempt++)); do
  if "${COMPOSE[@]}" exec -T mysql mysqladmin ping -h 127.0.0.1 -uroot -pserver-monitor-local-root --silent >/dev/null 2>&1; then
    break
  fi
  [[ ${attempt} -lt 60 ]] || fail "MySQL did not become ready"
  sleep 2
done
"${COMPOSE[@]}" exec -T mysql mysql -uroot -pserver-monitor-local-root -e "CREATE DATABASE IF NOT EXISTS \`${MYSQL_DATABASE}\`; GRANT ALL PRIVILEGES ON \`${MYSQL_DATABASE}\`.* TO 'server_monitor'@'%'; FLUSH PRIVILEGES;" >/dev/null
(
  cd server-web
  MYSQL_HOST=127.0.0.1 MYSQL_PORT="${DEMO_MYSQL_HOST_PORT}" MYSQL_DATABASE="${MYSQL_DATABASE}" MYSQL_USER=server_monitor MYSQL_PASSWORD=server-monitor-local-mysql \
    GOCACHE="${GOCACHE:-/tmp/cloudops-v2-demo-gocache}" go run ./cmd/migrate up
)
"${COMPOSE[@]}" up -d prometheus alertmanager server-web
wait_http "${DEMO_BASE_URL}/readyz" 90 || fail "server-web did not become ready"
chmod 600 "${KUBECONFIG_FILE}"
wait_http "${DEMO_BASE_URL}/incidents" 30 || fail "Incident Workbench is not accessible"
pass "MySQL migrations 00001-00006 and CloudOps services are ready"

stage 4 "Injecting workload failure"
kubectl --context "${KIND_CONTEXT}" -n "${DEMO_NAMESPACE}" scale deployment/"${DEMO_WORKLOAD}" --replicas=0 >/dev/null
incident_id="$(wait_for_json_value "${DEMO_BASE_URL}/api/v2/workbench/incidents?page=1&page_size=50" '.data.items | map(select(.service == "cloudops-demo-workload" and .environment == "local-demo")) | first | .id' 90)" || fail "Incident was not created from the firing Signal"
pass "real Alertmanager Signal created Incident ${incident_id}"

stage 5 "Running Agent investigation"
agent_response="$(curl --fail --silent --show-error -X POST -H "Idempotency-Key: demo-${REVISION:0:12}-$(date +%s)" "${DEMO_BASE_URL}/api/v2/incidents/${incident_id}/agent-runs")"
agent_run_id="$(jq -er '.data.id' <<<"${agent_response}")"
for ((attempt = 1; attempt <= 90; attempt++)); do
  agent_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/agent-runs/${agent_run_id}")"
  agent_status="$(jq -r '.data.status' <<<"${agent_response}")"
  [[ "${agent_status}" == "COMPLETED" ]] && break
  [[ "${agent_status}" != "FAILED" && "${agent_status}" != "CANCELLED" ]] || fail "AgentRun ended as ${agent_status}"
  [[ ${attempt} -lt 90 ]] || fail "AgentRun did not complete"
  sleep 2
done
steps_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/agent-runs/${agent_run_id}/steps")"
step_count="$(jq -r '.data | length' <<<"${steps_response}")"
[[ "${step_count}" -gt 1 ]] || fail "AgentRun did not persist multiple AgentSteps"
evidence_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/agent-runs/${agent_run_id}/evidence")"
evidence_id="$(jq -er '.data | map(select(.valid == true)) | first | .id' <<<"${evidence_response}")"
pass "AgentRun ${agent_run_id}; AgentSteps=${step_count}; Evidence=${evidence_id}"

stage 6 "Approving and applying Demo remediation"
plan_response="$(curl --fail --silent --show-error -X POST "${DEMO_BASE_URL}/api/v2/demo/incidents/${incident_id}/plan")"
plan_id="$(jq -er '.data.id' <<<"${plan_response}")"
plan_hash="$(jq -er '.data.plan_hash' <<<"${plan_response}")"
patch_hash="$(jq -er '.data.proposed_patch_hash' <<<"${plan_response}")"
plan_version="$(jq -er '.data.version' <<<"${plan_response}")"
approval_body="$(jq -cn --arg plan_hash "${plan_hash}" --arg patch_hash "${patch_hash}" --argjson version "${plan_version}" '{plan_hash:$plan_hash,patch_hash:$patch_hash,version:$version}')"
approval_response="$(curl --fail --silent --show-error -X POST -H 'Content-Type: application/json' --data "${approval_body}" "${DEMO_BASE_URL}/api/v2/remediations/${plan_id}/approve")"
change_request_id="$(jq -er '.data.change_request_id' <<<"${approval_response}")"
execute_response="$(curl --fail --silent --show-error -X POST "${DEMO_BASE_URL}/api/v2/demo/remediations/${plan_id}/execute")"
verification_run_id="$(jq -er '.data.id' <<<"${execute_response}")"
pass "RemediationPlan ${plan_id}; ChangeRequest=${change_request_id}; actor=demo-operator"

stage 7 "Running Verification"
verify_response="$(curl --fail --silent --show-error -X POST "${DEMO_BASE_URL}/api/v2/demo/incidents/${incident_id}/verify")"
verification_status="$(jq -er '.data.status' <<<"${verify_response}")"
[[ "${verification_status}" == "passed" ]] || fail "Verification ended as ${verification_status}"
incident_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}")"
incident_state="$(jq -er '.data.status' <<<"${incident_response}")"
[[ "${incident_state}" == "RESOLVED" ]] || fail "Incident final state is ${incident_state}"
checks_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}/verifications/${verification_run_id}")"
failed_required="$(jq -r '[.data.checks[] | select(.required == true and .status != "passed")] | length' <<<"${checks_response}")"
[[ "${failed_required}" == "0" ]] || fail "one or more required Verification checks did not pass"
required_checks="$(jq -r '[.data.checks[] | select(.required == true) | .type + "=" + (.status | ascii_upcase)] | join(",")' <<<"${checks_response}")"
postmortem_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/incidents/${incident_id}/postmortem")"
postmortem_id="$(jq -er '.data.id' <<<"${postmortem_response}")"
root_classification="$(jq -er '.data.root_cause.classification' <<<"${postmortem_response}")"
root_summary="$(jq -er '.data.root_cause.summary' <<<"${postmortem_response}")"
root_evidence="$(jq -r '.data.root_cause.evidence_ids | join(",")' <<<"${postmortem_response}")"
[[ "${root_classification}" == "fact" && "${root_summary}" != "unknown" && "${root_evidence}" == *"${evidence_id}"* ]] || fail "Postmortem confirmed fact/evidence was not preserved"
remediation_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}/remediation")"
approval_actor="$(jq -er '.data.approval_actor' <<<"${remediation_response}")"
[[ "${approval_actor}" == "demo-operator" ]] || fail "Demo approval actor was ${approval_actor}"
signals_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}/signals")"
timeline_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}/timeline")"
investigation_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}/investigation")"
signal_count="$(jq -er '.data | length' <<<"${signals_response}")"
timeline_count="$(jq -er '.data | length' <<<"${timeline_response}")"
workbench_run_count="$(jq -er '.data.runs | length' <<<"${investigation_response}")"
workbench_step_count="$(jq -er '.data.steps | length' <<<"${investigation_response}")"
workbench_evidence_count="$(jq -er '.data.evidence | length' <<<"${investigation_response}")"
[[ "${signal_count}" -gt 0 && "${timeline_count}" -gt 0 && "${workbench_run_count}" -gt 0 && "${workbench_step_count}" -gt 1 && "${workbench_evidence_count}" -gt 0 ]] || fail "Workbench core sections are incomplete"
pass "Verification required checks passed; Incident RESOLVED; Workbench sections populated"

stage 8 "Demo completed"
workbench_url="${DEMO_BASE_URL}/incidents/${incident_id}"
duration_seconds="$(( $(date +%s) - START_EPOCH ))"
printf '\nDEMO_STATUS=PASS\n'
printf 'Incident ID: %s\n' "${incident_id}"
printf 'AgentRun ID: %s\n' "${agent_run_id}"
printf 'Evidence ID: %s\n' "${evidence_id}"
printf 'RemediationPlan ID: %s\n' "${plan_id}"
printf 'ChangeRequest ID: %s\n' "${change_request_id}"
printf 'VerificationRun ID: %s\n' "${verification_run_id}"
printf 'Postmortem ID: %s\n' "${postmortem_id}"
printf 'Final Incident state: %s\n' "${incident_state}"
printf 'Verification required checks: %s\n' "${required_checks}"
printf 'Workbench sections: signals=%s,timeline=%s,agent_runs=%s,agent_steps=%s,evidence=%s,remediation=present,verification=present,postmortem=fact\n' "${signal_count}" "${timeline_count}" "${workbench_run_count}" "${workbench_step_count}" "${workbench_evidence_count}"
printf 'Workbench URL: %s\n' "${workbench_url}"
printf 'Execution mode: CONTROLLED_DIRECT_LOCAL_DEMO\n'
printf 'Approval actor: %s (Demo)\n' "${approval_actor}"
printf 'Demo database: %s\n' "${MYSQL_DATABASE}"
printf 'Demo duration seconds: %s\n' "${duration_seconds}"
printf 'Production disclaimer: local disposable Demo only; no production GitOps validation.\n'
printf 'PRODUCTION_GITOPS_E2E_VALIDATED=NO\n'
