#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

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
FAST_DEMO_IMAGE_TAG="${FAST_DEMO_IMAGE_TAG:-${REVISION:0:12}-step1}"
MYSQL_DATABASE="${MYSQL_DATABASE:-server_monitor_demo_$(date -u +%Y%m%d%H%M%S)}"
KUBECONFIG_FILE="${ROOT_DIR}/docker/kubeconfig"

export COMPOSE_PROJECT_NAME FAST_DEMO_IMAGE_TAG MYSQL_DATABASE
export DEMO_MYSQL_HOST_PORT
export FAST_DEMO_REVISION="${REVISION}"
export SERVER_WEB_HOST_PORT
export REDIS_HOST_PORT="${REDIS_HOST_PORT:-16379}"
export JAEGER_UI_HOST_PORT="${JAEGER_UI_HOST_PORT:-16687}"
export OTLP_GRPC_HOST_PORT="${OTLP_GRPC_HOST_PORT:-14317}"
export OTLP_HTTP_HOST_PORT="${OTLP_HTTP_HOST_PORT:-14318}"
export SERVER_PROBE_HOST_PORT="${SERVER_PROBE_HOST_PORT:-18083}"
export PROMETHEUS_HOST_PORT="${PROMETHEUS_HOST_PORT:-19090}"
export KAFKA_HOST_PORT="${KAFKA_HOST_PORT:-19092}"
export ALERT_SERVICE_HOST_PORT="${ALERT_SERVICE_HOST_PORT:-18081}"
export ALERTMANAGER_HOST_PORT="${ALERTMANAGER_HOST_PORT:-19093}"

COMPOSE=(docker compose --env-file .env.example -f docker-compose.yml -f docker-compose.fast-demo.yml)

info() { printf '[INFO] %s\n' "$*"; }
pass() { printf '[PASS] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

on_error() {
  local exit_code=$?
  trap - ERR
  printf '[FAIL] Demo stopped with exit code %s\n' "${exit_code}" >&2
  "${COMPOSE[@]}" logs --tail=120 server-web 2>/dev/null || true
  exit "${exit_code}"
}
trap on_error ERR

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

resolve_service_image() {
  local service=$1
  local variable=$2
  local fallback=$3
  local candidate="${!variable:-}"
  if [[ -z "${candidate}" ]] && docker container inspect "server-monitor-${service}-1" >/dev/null 2>&1; then
    candidate="$(docker container inspect "server-monitor-${service}-1" --format '{{.Config.Image}}')"
  fi
  if [[ -z "${candidate}" ]]; then
    candidate="$(docker image ls --format '{{.Repository}}:{{.Tag}}' | awk -v service="${service}" '$0 ~ "cloudops-local/" service ":" { print; exit }')"
  fi
  if [[ -z "${candidate}" ]]; then
    candidate="${fallback}"
  fi
  declare -gx "${variable}=${candidate}"
}

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

for command in docker kind kubectl curl jq go git sed awk; do
  require_command "${command}"
done
docker info >/dev/null 2>&1 || fail "Docker daemon is unavailable"
resolve_service_image server-probe FAST_DEMO_PROBE_IMAGE server-monitor/server-probe:local
resolve_service_image alert-service FAST_DEMO_ALERT_IMAGE server-monitor/alert-service:local

info "Preparing disposable kind cluster ${KIND_CLUSTER_NAME}"
if ! kind get clusters 2>/dev/null | awk -v name="${KIND_CLUSTER_NAME}" '$0 == name { found=1 } END { exit !found }'; then
  kind create cluster --name "${KIND_CLUSTER_NAME}" --wait 90s
fi
kubectl --context "${KIND_CONTEXT}" apply -f docker/fast-demo/rbac.yaml >/dev/null

info "Generating namespace-scoped demo kubeconfig"
token="$(kubectl --context "${KIND_CONTEXT}" -n "${DEMO_NAMESPACE}" create token demo-operator --duration=2h)"
kind get kubeconfig --name "${KIND_CLUSTER_NAME}" |
  sed -E "s|server: https://[^:]+:[0-9]+|server: https://${KIND_CLUSTER_NAME}-control-plane:6443|g" >"${KUBECONFIG_FILE}"
kubectl config --kubeconfig "${KUBECONFIG_FILE}" set-credentials demo-operator --token="${token}" >/dev/null
kubectl config --kubeconfig "${KUBECONFIG_FILE}" set-context "${KIND_CONTEXT}" --cluster="${KIND_CONTEXT}" --user=demo-operator --namespace="${DEMO_NAMESPACE}" >/dev/null
kubectl config --kubeconfig "${KUBECONFIG_FILE}" use-context "${KIND_CONTEXT}" >/dev/null
kubectl config --kubeconfig "${KUBECONFIG_FILE}" unset "users.${KIND_CONTEXT}" >/dev/null 2>&1 || true
chmod 644 "${KUBECONFIG_FILE}"
demo_subject="system:serviceaccount:${DEMO_NAMESPACE}:demo-operator"
kubectl --context "${KIND_CONTEXT}" auth can-i update deployments/scale -n "${DEMO_NAMESPACE}" --as="${demo_subject}" | grep -qx yes || fail "demo service account lacks scoped write permission"
outside_write="$(kubectl --context "${KIND_CONTEXT}" auth can-i update deployments/scale -n default --as="${demo_subject}" 2>/dev/null || true)"
[[ "${outside_write}" == "no" ]] || fail "demo service account unexpectedly writes outside the demo namespace"

info "Building and loading the disposable demo workload"
docker build -q -t cloudops-demo/workload:v2-demo -f docker/fast-demo/workload.Dockerfile . >/dev/null
kind load docker-image cloudops-demo/workload:v2-demo --name "${KIND_CLUSTER_NAME}" >/dev/null
kubectl --context "${KIND_CONTEXT}" apply -f docker/fast-demo/workload.yaml >/dev/null
kubectl --context "${KIND_CONTEXT}" -n "${DEMO_NAMESPACE}" rollout status deployment/"${DEMO_WORKLOAD}" --timeout=90s >/dev/null
kind_node_ip="$(docker inspect "${KIND_CLUSTER_NAME}-control-plane" --format '{{(index .NetworkSettings.Networks "kind").IPAddress}}')"
workload_url="http://${kind_node_ip}:${DEMO_NODE_PORT}/"
wait_http "${workload_url}metrics" 30 || fail "workload NodePort did not become ready"

info "Recreating only prior ${COMPOSE_PROJECT_NAME} disposable containers and volumes"
"${COMPOSE[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true

info "Building current-source demo application images"
for service_image in "server-probe:${FAST_DEMO_PROBE_IMAGE}" "alert-service:${FAST_DEMO_ALERT_IMAGE}"; do
  service="${service_image%%:*}"
  image="${service_image#*:}"
  if ! docker image inspect "${image}" >/dev/null 2>&1; then
    built=false
    for attempt in 1 2 3; do
      if BUILDKIT_PROGRESS=plain "${COMPOSE[@]}" build "${service}"; then
        built=true
        break
      fi
      sleep 2
    done
    [[ "${built}" == "true" ]] || fail "failed to build ${service} after three attempts"
  fi
done
BUILDKIT_PROGRESS=plain "${COMPOSE[@]}" build server-web

info "Starting disposable dependencies"
"${COMPOSE[@]}" up -d mysql redis kafka kafka-init jaeger
for ((attempt = 1; attempt <= 60; attempt++)); do
  if "${COMPOSE[@]}" exec -T mysql mysqladmin ping -h 127.0.0.1 -uroot -pserver-monitor-local-root --silent >/dev/null 2>&1; then
    break
  fi
  [[ ${attempt} -lt 60 ]] || fail "MySQL did not become ready"
  sleep 2
done
"${COMPOSE[@]}" exec -T mysql mysql -uroot -pserver-monitor-local-root -e "CREATE DATABASE IF NOT EXISTS \`${MYSQL_DATABASE}\`; GRANT ALL PRIVILEGES ON \`${MYSQL_DATABASE}\`.* TO 'server_monitor'@'%'; FLUSH PRIVILEGES;" >/dev/null

info "Applying explicit V2 Goose migrations to ${MYSQL_DATABASE}"
(
  cd server-web
  MYSQL_HOST=127.0.0.1 MYSQL_PORT="${DEMO_MYSQL_HOST_PORT}" MYSQL_DATABASE="${MYSQL_DATABASE}" MYSQL_USER=server_monitor MYSQL_PASSWORD=server-monitor-local-mysql \
    GOCACHE="${GOCACHE:-/tmp/cloudops-v2-demo-gocache}" go run ./cmd/migrate up
)

info "Starting the V2 demo application path"
"${COMPOSE[@]}" up -d server-probe alert-service prometheus alertmanager server-web
wait_http "${DEMO_BASE_URL}/readyz" 90 || fail "server-web did not become ready"
wait_http "${DEMO_BASE_URL}/incidents" 30 || fail "Incident Workbench is not accessible"

info "Injecting the demo failure through Kubernetes"
kubectl --context "${KIND_CONTEXT}" -n "${DEMO_NAMESPACE}" scale deployment/"${DEMO_WORKLOAD}" --replicas=0 >/dev/null
incident_id="$(wait_for_json_value "${DEMO_BASE_URL}/api/v2/workbench/incidents?page=1&page_size=50" '.data.items | map(select(.service == "cloudops-demo-workload" and .environment == "local-demo")) | first | .id' 90)" || fail "Incident was not created from the firing Signal"
pass "Incident created: ${incident_id}"

info "Starting the durable deterministic AgentRun"
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
pass "AgentRun completed: ${agent_run_id}; Evidence: ${evidence_id}; steps=${step_count}"

info "Creating, approving and executing the controlled Demo remediation"
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
verify_response="$(curl --fail --silent --show-error -X POST "${DEMO_BASE_URL}/api/v2/demo/incidents/${incident_id}/verify")"
verification_status="$(jq -er '.data.status' <<<"${verify_response}")"
[[ "${verification_status}" == "passed" ]] || fail "Verification ended as ${verification_status}"

incident_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}")"
incident_state="$(jq -er '.data.status' <<<"${incident_response}")"
[[ "${incident_state}" == "RESOLVED" ]] || fail "Incident final state is ${incident_state}"
checks_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}/verifications/${verification_run_id}")"
failed_required="$(jq -r '[.data.checks[] | select(.required == true and .status != "passed")] | length' <<<"${checks_response}")"
[[ "${failed_required}" == "0" ]] || fail "one or more required Verification checks did not pass"
postmortem_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/incidents/${incident_id}/postmortem")"
postmortem_id="$(jq -er '.data.id' <<<"${postmortem_response}")"
root_classification="$(jq -er '.data.root_cause.classification' <<<"${postmortem_response}")"
root_summary="$(jq -er '.data.root_cause.summary' <<<"${postmortem_response}")"
root_evidence="$(jq -r '.data.root_cause.evidence_ids | join(",")' <<<"${postmortem_response}")"
[[ "${root_classification}" == "fact" && "${root_summary}" != "unknown" && "${root_evidence}" == *"${evidence_id}"* ]] || fail "Postmortem confirmed fact/evidence was not preserved"
remediation_response="$(curl --fail --silent --show-error "${DEMO_BASE_URL}/api/v2/workbench/incidents/${incident_id}/remediation")"
approval_actor="$(jq -er '.data.approval_actor' <<<"${remediation_response}")"
[[ "${approval_actor}" == "demo-operator" ]] || fail "Demo approval actor was ${approval_actor}"

workbench_url="${DEMO_BASE_URL}/incidents/${incident_id}"
pass "LOCAL_DEMO_PASS"
printf '\nIncident ID: %s\n' "${incident_id}"
printf 'AgentRun ID: %s\n' "${agent_run_id}"
printf 'Evidence ID: %s\n' "${evidence_id}"
printf 'RemediationPlan ID: %s\n' "${plan_id}"
printf 'ChangeRequest ID: %s\n' "${change_request_id}"
printf 'VerificationRun ID: %s\n' "${verification_run_id}"
printf 'Postmortem ID: %s\n' "${postmortem_id}"
printf 'Incident final state: %s\n' "${incident_state}"
printf 'Required Verification checks: PASS\n'
printf 'Approval actor: %s (Demo)\n' "${approval_actor}"
printf 'Execution mode: CONTROLLED_DIRECT_LOCAL_DEMO\n'
printf 'Workbench URL: %s\n' "${workbench_url}"
printf 'Workload URL: %s\n' "${workload_url}"
printf 'Demo database: %s\n' "${MYSQL_DATABASE}"
printf 'PRODUCTION_GITOPS_E2E_VALIDATED=NO\n'
