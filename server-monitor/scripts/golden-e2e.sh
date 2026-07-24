#!/usr/bin/env bash
# shellcheck disable=SC2016 # Literal Markdown backticks and jq/sed programs are intentional.
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REGRESSION_FIXTURE_DIR="${ROOT_DIR}/deploy/contracts/gitops-demo/regression/apps/demo"
HEALTHY_FIXTURE_DIR="${ROOT_DIR}/deploy/contracts/gitops-demo/healthy/apps/demo"
CONTRACT_PACKAGE="./cmd/gitops-demo-contract"
QUALITY_REPORT="${GOLDEN_AGENT_QUALITY_REPORT:-${ROOT_DIR}/docs/evidence/phase-4-agent-quality-v5-report.md}"
SOURCE_SHA="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
SOURCE_REPO="${GOLDEN_SOURCE_REPO:-05allan1213/CloudOps-Copilot}"
GITOPS_REPO="${GOLDEN_GITOPS_REPO:-05allan1213/cloudops-gitops-demo}"
GITOPS_PATH="${GOLDEN_GITOPS_PATH:-apps/demo/deployment.yaml}"
ARGO_APP="${GOLDEN_ARGO_APPLICATION:-cloudops-demo}"
APP_NAMESPACE="${GOLDEN_APP_NAMESPACE:-cloudops-system}"
DEMO_NAMESPACE="${GOLDEN_DEMO_NAMESPACE:-demo}"
DEMO_RELEASE="${GOLDEN_DEMO_RELEASE:-cloudops-demo}"
API_BASE_URL="${GOLDEN_API_BASE_URL:-}"
EVIDENCE_DIR="${ROOT_DIR}/docs/evidence/${SOURCE_SHA}"
MANIFEST="${EVIDENCE_DIR}/manifest.md"
TMP_ROOT=""
WRITE_MANIFEST=false
FAILURE_REASON=""

declare -a COMMANDS=()
declare -a GATES=(source agent_quality human_gh github_apps kind argo llm oauth environment_clean images regression_pr regression_ci argo_bad incident agent plan_approval remediation_pr remediation_ci argo_fix verification resolution resources)
declare -a REQUIRED_PASS_GATES=(source agent_quality human_gh github_apps kind argo llm oauth environment_clean images regression_pr regression_ci argo_bad incident agent plan_approval remediation_pr remediation_ci argo_fix verification resolution)
declare -A STATUS DETAIL
for gate in "${GATES[@]}"; do STATUS["${gate}"]="NOT RUN"; DETAIL["${gate}"]="not reached"; done

die() { FAILURE_REASON="$*"; printf 'FAIL: %s\n' "$*" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }
require_env() { [[ -n "${!1:-}" ]] || die "missing required environment variable: $1"; }
pass() { STATUS["$1"]="PASS"; DETAIL["$1"]="$2"; }
fail() { STATUS["$1"]="FAIL"; DETAIL["$1"]="$2"; die "$2"; }
not_run() { STATUS["$1"]="NOT RUN"; DETAIL["$1"]="$2"; }
record_command() { COMMANDS+=("$*"); }
is_sha() { [[ "$1" =~ ^[0-9a-f]{40}$ ]]; }
is_uuid() { [[ "$1" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]]; }
safe_file() {
  [[ -f "$1" && ! -L "$1" && -s "$1" ]] || die "credential file must be a non-empty regular file: $1"
  [[ "$(wc -c <"$1")" -le 16384 ]] || die "credential file is too large: $1"
}
md() { printf '%s' "$1" | tr '\r\n' '  ' | sed 's/|/\\|/g; s/`/'"'"'/g'; }

cleanup() {
  local code=$?
  if [[ "${WRITE_MANIFEST}" == true ]]; then write_manifest "${code}" || true; fi
  [[ -z "${TMP_ROOT}" ]] || rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

interrupted() {
  local signal="$1" code="$2"
  FAILURE_REASON="interrupted by ${signal}"
  trap - INT TERM
  exit "${code}"
}
trap 'interrupted SIGINT 130' INT
trap 'interrupted SIGTERM 143' TERM

write_manifest() {
  local exit_code="$1" overall="FAIL" generated command gate versions incomplete=""
  if [[ "${exit_code}" -eq 0 ]]; then
    overall="PASS"
    for gate in "${REQUIRED_PASS_GATES[@]}"; do
      if [[ "${STATUS[$gate]}" != PASS ]]; then
        overall="FAIL"
        incomplete="${incomplete}${incomplete:+,}${gate}:${STATUS[$gate]}"
      fi
    done
    if [[ "${overall}" != PASS && -z "${FAILURE_REASON}" ]]; then
      FAILURE_REASON="incomplete Golden gate ledger: ${incomplete}"
    fi
  fi
  versions="${VERSIONS_MD:-}"
  if [[ -z "${versions}" ]]; then
    printf -v versions '| Component | Version |\n|---|---|\n| cluster versions | NOT RUN |'
  fi
  mkdir -p "${EVIDENCE_DIR}"
  generated="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  {
    printf '# CloudOps Golden E2E Evidence Manifest\n\n'
    printf -- '- Overall: `%s`\n- Generated at: `%s`\n- CloudOps exact source SHA: `%s`\n- Source repository: `%s`\n' "${overall}" "${generated}" "${SOURCE_SHA}" "$(md "${SOURCE_REPO}")"
    printf -- '- Failure reason: `%s`\n\n' "$(md "${FAILURE_REASON:-none}")"
    printf '## Gate ledger\n\n| Gate | Result | Evidence |\n|---|---|---|\n'
    for gate in "${GATES[@]}"; do printf '| `%s` | `%s` | %s |\n' "${gate}" "${STATUS[$gate]}" "$(md "${DETAIL[$gate]}")"; done
    printf '\n## Exact identities\n\n'
    printf '| Identity | Value |\n|---|---|\n'
    printf '| Regression actor / PR / head / merged SHA | `%s` / `%s` / `%s` / `%s` |\n' "${REGRESSION_ACTOR:-NOT RUN}" "${REGRESSION_PR:-NOT RUN}" "${REGRESSION_HEAD_SHA:-NOT RUN}" "${BAD_SHA:-NOT RUN}"
    printf '| Remediation PR / validated head / merged SHA | `%s` / `%s` / `%s` |\n' "${REMEDIATION_PR:-NOT RUN}" "${REMEDIATION_HEAD_SHA:-NOT RUN}" "${FIX_SHA:-NOT RUN}"
    printf '| Argo deployed / successful syncResult revision | `%s` / `%s` |\n' "${ARGO_REVISION:-NOT RUN}" "${ARGO_SYNC_REVISION:-NOT RUN}"
    printf '| API / Worker image digest | `%s` / `%s` |\n' "${API_DIGEST:-NOT RUN}" "${WORKER_DIGEST:-NOT RUN}"
    printf '| Incident / AgentRun / Evidence IDs | `%s` / `%s` / `%s` |\n' "${INCIDENT_ID:-NOT RUN}" "${AGENT_RUN_ID:-NOT RUN}" "${EVIDENCE_IDS:-NOT RUN}"
    printf '| Plan / Approval / ChangeRequest / Verification / Resolution IDs | `%s` / `%s` / `%s` / `%s` / `%s` |\n' "${PLAN_ID:-NOT RUN}" "${APPROVAL_ID:-NOT RUN}" "${CHANGE_ID:-NOT RUN}" "${VERIFICATION_ID:-NOT RUN}" "${RESOLUTION_ID:-NOT RUN}"
    printf '| Approval-bound tree / post-image hash | `%s` / `%s` |\n' "${APPROVED_TREE_HASH:-NOT RUN}" "${APPROVED_POST_IMAGE_HASH:-NOT RUN}"
    printf '| Agent provider / model / prompt / tool-schema hash | `%s` / `%s` / `%s` / `%s` |\n' "${MODEL_PROVIDER:-NOT RUN}" "${MODEL_NAME:-NOT RUN}" "${PROMPT_HASH:-NOT RUN}" "${TOOL_SCHEMA_HASH:-NOT RUN}"
    printf '| GitHub Actions (source / regression / remediation) | %s / %s / %s |\n' "$(md "${SOURCE_ACTIONS:-NOT RUN}")" "$(md "${REGRESSION_ACTIONS:-NOT RUN}")" "$(md "${REMEDIATION_ACTIONS:-NOT RUN}")"
    printf '\n## Measured timing and resources\n\n'
    printf '| Measurement | Value |\n|---|---|\n'
    printf '| bad merge -> incident detected | `%s` |\n' "${BAD_TO_INCIDENT_MS:-NOT RUN}"
    printf '| incident -> Agent completed | `%s` |\n' "${INCIDENT_TO_AGENT_MS:-NOT RUN}"
    printf '| Agent completed -> human approval | `%s` |\n' "${AGENT_TO_APPROVAL_MS:-NOT RUN}"
    printf '| approval -> fix merge | `%s` |\n' "${APPROVAL_TO_FIX_MS:-NOT RUN}"
    printf '| fix merge -> Argo sync | `%s` |\n' "${FIX_TO_ARGO_MS:-NOT RUN}"
    printf '| Argo sync -> Verification passed | `%s` |\n' "${ARGO_TO_VERIFY_MS:-NOT RUN}"
    printf '| full bad merge -> Verification passed | `%s` |\n' "${TOTAL_E2E_MS:-NOT RUN}"
    printf '| Agent tool calls / model calls / tokens | `%s` / `%s` / `%s` |\n' "${TOOL_CALLS:-NOT RUN}" "${MODEL_CALLS:-NOT RUN}" "${TOKENS:-NOT RUN}"
    printf '| Observed resource peak | `%s` |\n' "$(md "${RESOURCE_PEAK:-NOT RUN}")"
    printf '| Bootstrap / image download / model latency / human waits | `NOT RUN unless explicitly represented above; never inferred` |\n'
    printf '\n## Versions\n\n%s\n' "${versions}"
    printf '\n## Commands\n\n'
    for command in "${COMMANDS[@]}"; do printf -- '- `%s`\n' "$(md "${command}")"; done
    printf '\n## Known limitations and NOT RUN\n\n- Credentials and provider raw responses are never persisted.\n- Human PR merges and remediation approval are intentionally outside this harness.\n- A gate without authoritative live evidence remains `NOT RUN`; fixture or mock output is never substituted.\n- `CONTRACT-V3` and Phase 7B cleanup are `NOT RUN`.\n'
  } >"${MANIFEST}.tmp"
  mv "${MANIFEST}.tmp" "${MANIFEST}"
  printf 'Evidence manifest: %s\n' "${MANIFEST}" >&2
}

read_secret() { safe_file "$1"; tr -d '\r\n' <"$1"; }

check_source() {
  [[ -z "$(git -C "${ROOT_DIR}" status --porcelain --untracked-files=normal)" ]] || fail source "CloudOps worktree must be clean before exact-SHA execution"
  is_sha "${SOURCE_SHA}" || fail source "invalid exact source SHA"
  pass source "clean worktree at ${SOURCE_SHA}"
}

check_agent_quality() {
  local eval_ref eval_sha
  grep -Fq 'Status: `AGENT_QUALITY=PASS`' "${QUALITY_REPORT}" || fail agent_quality "Agent Quality report is not PASS"
  eval_ref="$(sed -n 's/^- Exact evaluated source SHA: `\([^`]*\)`.*/\1/p' "${QUALITY_REPORT}" | head -1)"
  [[ -n "${eval_ref}" ]] || fail agent_quality "Agent Quality exact SHA is missing"
  eval_sha="$(git -C "${ROOT_DIR}" rev-parse "${eval_ref}^{commit}")"
  git -C "${ROOT_DIR}" diff --quiet "${eval_sha}" "${SOURCE_SHA}" -- cmd/cloudops-agent-eval internal/agent eval || fail agent_quality "eval/runtime material changed after the passing quality SHA"
  pass agent_quality "PASS report ${eval_sha}; relevant material unchanged"
}

check_human_gh() {
  local login runs
  gh auth status >/dev/null 2>&1 || fail human_gh "gh has no authenticated human session"
  login="$(gh api user --jq .login)"
  [[ -n "${login}" && "${login}" != *"[bot]" ]] || fail human_gh "gh identity is absent or is a bot"
  gh repo view "${GITOPS_REPO}" --json nameWithOwner >/dev/null
  runs="$(gh run list -R "${SOURCE_REPO}" --commit "${SOURCE_SHA}" --limit 20 --json url,status,conclusion,headSha,workflowName)"
  jq -e --arg sha "${SOURCE_SHA}" 'any(.[]; .headSha==$sha and .status=="completed" and .conclusion=="success")' <<<"${runs}" >/dev/null || fail human_gh "no successful GitHub Actions run exists for the exact source SHA"
  SOURCE_ACTIONS="$(jq -r --arg sha "${SOURCE_SHA}" '[.[]|select(.headSha==$sha)|(.workflowName+":"+.conclusion+":"+.url)]|join(", ")' <<<"${runs}")"
  pass human_gh "authenticated human ${login}; exact-SHA Actions PASS"
}

github_status() {
  local token="$1" method="$2" url="$3" payload="${4:-}" response
  if [[ -n "${payload}" ]]; then
    response="$(curl --silent --show-error --max-time 20 --request "${method}" \
      -H 'Accept: application/vnd.github+json' -H "Authorization: Bearer ${token}" \
      -H 'X-GitHub-Api-Version: 2022-11-28' --data-binary "${payload}" \
      --write-out $'\n%{http_code}' "${url}")"
  else
    response="$(curl --silent --show-error --max-time 20 --request "${method}" \
      -H 'Accept: application/vnd.github+json' -H "Authorization: Bearer ${token}" \
      -H 'X-GitHub-Api-Version: 2022-11-28' --write-out $'\n%{http_code}' "${url}")"
  fi
  printf '%s' "${response##*$'\n'}"
}

check_github_app_token() {
  local file="$1" expected_mode="$2" label="$3" token repo_api probe_branch ref_status pr_status admin_status absent_status pulls
  safe_file "${file}"; token="$(read_secret "${file}")"
  repo_api="https://api.github.com/repos/${GITOPS_REPO}"
  probe_branch="cloudops-permission-probe-${SOURCE_SHA:0:12}"

  curl --fail --silent --show-error --max-time 20 \
    -H 'Accept: application/vnd.github+json' -H "Authorization: Bearer ${token}" \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    "${repo_api}/contents?ref=main" >/dev/null || fail github_apps "${label} cannot read repository contents"
  curl --fail --silent --show-error --max-time 20 \
    -H 'Accept: application/vnd.github+json' -H "Authorization: Bearer ${token}" \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    "${repo_api}/pulls?state=open&per_page=1" >/dev/null || fail github_apps "${label} cannot read pull requests"

  ref_status="$(github_status "${token}" POST "${repo_api}/git/refs" \
    "$(jq -cn --arg ref "refs/heads/${probe_branch}" '{ref:$ref,sha:"0000000000000000000000000000000000000000"}')")"
  pr_status="$(github_status "${token}" POST "${repo_api}/pulls" \
    "$(jq -cn --arg head "${probe_branch}" '{title:"",head:$head,base:"main"}')")"
  admin_status="$(github_status "${token}" PUT "${repo_api}/branches/main/protection" '{}')"

  if [[ "${expected_mode}" == read ]]; then
    [[ "${ref_status}" == 403 && "${pr_status}" == 403 ]] || fail github_apps "${label} unexpectedly has contents or pull-request write permission"
  else
    [[ "${ref_status}" == 422 && "${pr_status}" == 422 ]] || fail github_apps "${label} did not expose bounded contents and pull-request write capability"
  fi
  [[ "${admin_status}" == 403 ]] || fail github_apps "${label} unexpectedly has repository administration write permission"

  absent_status="$(github_status "${token}" GET "${repo_api}/git/ref/heads/${probe_branch}")"
  [[ "${absent_status}" == 404 ]] || fail github_apps "${label} permission probe unexpectedly created a Git ref"
  pulls="$(curl --fail --silent --show-error --max-time 20 \
    -H 'Accept: application/vnd.github+json' -H "Authorization: Bearer ${token}" \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    "${repo_api}/pulls?state=all&head=${GITOPS_REPO%%/*}:${probe_branch}")"
  jq -e 'length == 0' <<<"${pulls}" >/dev/null || fail github_apps "${label} permission probe unexpectedly created a pull request"
  unset token
}

check_github_apps() {
  require_env GOLDEN_GITHUB_READ_APP_TOKEN_FILE; require_env GOLDEN_GITHUB_WRITE_APP_TOKEN_FILE
  [[ "$(sha256sum "${GOLDEN_GITHUB_READ_APP_TOKEN_FILE}" | cut -d' ' -f1)" != "$(sha256sum "${GOLDEN_GITHUB_WRITE_APP_TOKEN_FILE}" | cut -d' ' -f1)" ]] || fail github_apps "read and write GitHub App tokens must be distinct"
  check_github_app_token "${GOLDEN_GITHUB_READ_APP_TOKEN_FILE}" read read-app
  check_github_app_token "${GOLDEN_GITHUB_WRITE_APP_TOKEN_FILE}" write write-app
  pass github_apps "separate live read/write installation tokens and bounded permissions verified"
}

argo_json() {
  local token; token="$(read_secret "${GOLDEN_ARGO_TOKEN_FILE}")"
  argocd app get "${ARGO_APP}" --server "${GOLDEN_ARGO_SERVER}" --auth-token "${token}" --grpc-web -o json
  unset token
}

check_kind_argo() {
  local context cluster token can_sync app
  context="$(kubectl config current-context)"; [[ "${context}" == kind-* ]] || fail kind "current Kubernetes context is not kind"
  cluster="${context#kind-}"; kind get clusters | grep -Fxq "${cluster}" || fail kind "current kind cluster is not present"
  kubectl -n "${APP_NAMESPACE}" rollout status deployment/cloudops-api --timeout=10s >/dev/null
  kubectl -n "${APP_NAMESPACE}" rollout status deployment/cloudops-worker --timeout=10s >/dev/null
  [[ "$(kubectl auth can-i patch deployments.apps -n "${DEMO_NAMESPACE}" --as="system:serviceaccount:${APP_NAMESPACE}:cloudops-worker")" == no ]] || fail kind "worker can patch Kubernetes Deployments"
  pass kind "live kind context ${context}; API/Worker ready; worker write denied"
  require_env GOLDEN_ARGO_SERVER; require_env GOLDEN_ARGO_TOKEN_FILE
  app="$(argo_json)"; jq -e --arg app "${ARGO_APP}" '.metadata.name==$app and .spec.syncPolicy.automated.enabled==true' <<<"${app}" >/dev/null || fail argo "live Argo Application contract is invalid"
  token="$(read_secret "${GOLDEN_ARGO_TOKEN_FILE}")"
  can_sync="$(argocd account can-i sync applications "${ARGO_APP}" --server "${GOLDEN_ARGO_SERVER}" --auth-token "${token}" --grpc-web 2>/dev/null || true)"; unset token
  [[ "${can_sync,,}" == no ]] || fail argo "Argo observer identity can sync applications"
  pass argo "live Argo Application readable and sync denied"
}

check_llm() {
  local key payload response
  require_env GOLDEN_LLM_API_KEY_FILE; require_env GOLDEN_LLM_API_URL; require_env GOLDEN_LLM_MODEL
  [[ "${GOLDEN_LLM_API_URL}" == https://* ]] || fail llm "LLM API URL must use HTTPS"
  key="$(read_secret "${GOLDEN_LLM_API_KEY_FILE}")"
  payload="$(jq -cn --arg model "${GOLDEN_LLM_MODEL}" '{model:$model,temperature:0,max_tokens:64,messages:[{role:"user",content:"Reply only with OK."}]}')"
  response="$(curl --fail --silent --show-error --max-time 60 -H 'Content-Type: application/json' -H "Authorization: Bearer ${key}" --data-binary "${payload}" "${GOLDEN_LLM_API_URL}")"; unset key
  jq -e '.choices[0].message.content|type=="string" and length>0' <<<"${response}" >/dev/null || fail llm "live LLM response contract failed"
  MODEL_PROVIDER="${GOLDEN_LLM_PROVIDER:-unknown}"; MODEL_NAME="${GOLDEN_LLM_MODEL}"
  pass llm "live provider completion returned a typed response"
}

api_get() { curl --fail --silent --show-error --max-time 20 --cookie "${GOLDEN_OAUTH_COOKIE_JAR}" "${API_BASE_URL}$1"; }
check_oauth() {
  local session
  require_env GOLDEN_API_BASE_URL; API_BASE_URL="${GOLDEN_API_BASE_URL%/}"; require_env GOLDEN_OAUTH_COOKIE_JAR; safe_file "${GOLDEN_OAUTH_COOKIE_JAR}"
  [[ "${API_BASE_URL}" == https://* || "${API_BASE_URL}" == http://127.0.0.1:* || "${API_BASE_URL}" == http://localhost:* ]] || fail oauth "API base URL must be HTTPS or loopback"
  session="$(api_get /api/v3/session/csrf)"
  jq -e '.token|type=="string" and length>20' <<<"${session}" >/dev/null
  jq -e '.actor.provider=="github" and (.actor.login|length>0) and (.actor.role=="operator" or .actor.role=="viewer")' <<<"${session}" >/dev/null || fail oauth "live oauth2-proxy session identity contract failed"
  pass oauth "authenticated GitHub OAuth session reached the API without persisting its token"
}

check_environment_clean() {
  local incidents active
  incidents="$(api_get "/api/v3/incidents?service=${GOLDEN_TARGET_SERVICE:-demo}&limit=100")"
  active="$(jq -r '[.items[] | select((.migrated_legacy | not) and (.migrated_legacy_context | not) and (.status != "resolved" and .status != "closed"))] | length' <<<"${incidents}")"
  [[ "${active}" == 0 ]] || fail environment_clean "Golden target has ${active} active native Incident(s); resolve test residue before fault injection"
  pass environment_clean "no active native Incident exists for the Golden target"
}

check_images() {
  local service image image_id digest revision
  for service in api worker; do
    image="$(kubectl -n "${APP_NAMESPACE}" get deployment "cloudops-${service}" -o jsonpath='{.spec.template.spec.containers[?(@.name=="cloudops-'"${service}"'")].image}')"
    image_id="$(kubectl -n "${APP_NAMESPACE}" get pods -l "app.kubernetes.io/name=cloudops-${service}" -o json | jq -r '[.items[].status.containerStatuses[]?|select(.name=="cloudops-'"${service}"'")|.imageID]|unique|if length==1 then .[0] else empty end')"
    digest="${image_id##*@}"; [[ "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || fail images "${service} Pod does not expose one immutable image digest"
    [[ "${image}" == *@"${digest}" ]] || fail images "${service} Deployment is not pinned to its running digest"
    revision="$(docker buildx imagetools inspect "${image}" --format '{{json .Image}}' | jq -r '.config.Labels["org.opencontainers.image.revision"] // empty')"
    [[ "${revision}" == "${SOURCE_SHA}" ]] || fail images "${service} OCI revision does not equal the exact source SHA"
    if [[ "${service}" == api ]]; then API_DIGEST="${digest}"; else WORKER_DIGEST="${digest}"; fi
  done
  pass images "API/Worker immutable digests and OCI revision ${SOURCE_SHA} verified"
}

required_checks() {
  local repo="$1" pr="$2" output
  output="$(gh pr checks "${pr}" -R "${repo}" --required --json name,state,link)"
  jq -e 'length>0 and all(.[]; .state=="SUCCESS")' <<<"${output}" >/dev/null || return 1
  jq -r '[.[]|(.name+":"+.state+":"+.link)]|join(", ")' <<<"${output}"
}

milliseconds_between() {
  local start="$1" end="$2"; [[ -n "${start}" && -n "${end}" ]] || { printf 'NOT RUN'; return; }
  printf '%s' "$(( $(date -u -d "${end}" +%s%3N) - $(date -u -d "${start}" +%s%3N) ))"
}

wait_for_json() {
  local seconds="${GOLDEN_TIMEOUT_SECONDS:-2700}" elapsed=0 command="$1" filter="$2" result
  while (( elapsed < seconds )); do
    result="$(eval "${command}" 2>/dev/null || true)"
    if [[ -n "${result}" ]] && jq -e "${filter}" <<<"${result}" >/dev/null 2>&1; then printf '%s' "${result}"; return 0; fi
    sleep 10; elapsed=$((elapsed+10))
  done
  return 1
}

collect_versions() {
  local kube kind_v helm_v argo_v
  kube="$(kubectl version -o json | jq -r '.serverVersion.gitVersion')"; kind_v="$(kind version | tr '\n' ' ')"; helm_v="$(helm version --short)"; argo_v="$(argocd version --client --short 2>/dev/null | tr '\n' ' ')"
  printf -v VERSIONS_MD '| Component | Version |\n|---|---|\n| Kubernetes | %s |\n| kind | %s |\n| Helm | %s |\n| Argo CD CLI | %s |' "${kube}" "${kind_v}" "${helm_v}" "${argo_v}"
}

run_preflight() {
  local command
  for command in git jq curl gh kubectl kind helm argocd docker sha256sum sed date; do require_cmd "${command}"; done
  check_source; check_agent_quality; check_human_gh; check_github_apps; check_kind_argo; check_llm; check_oauth; check_environment_clean; check_images; collect_versions
  record_command "make e2e-gitops"
  record_command "gh run list / gh pr checks --required (read-only)"
  record_command "argocd app get / argocd account can-i (read-only)"
  record_command "kubectl get / rollout status / auth can-i / top (read-only)"
  record_command "GET /api/v3 Incident Workbench projections with OAuth cookie (read-only)"
  record_command "one bounded real-model completion preflight"
}

scenario_open_regression_pr() {
  local worktree branch actor actor_type existing origin_url original_json expected_json actual_json pr_url changed
  for command in git jq yq gh go; do require_cmd "${command}"; done
  check_source; require_env GOLDEN_GITOPS_WORKTREE; worktree="$(cd "${GOLDEN_GITOPS_WORKTREE}" && pwd)"
  [[ -z "$(git -C "${worktree}" status --porcelain)" ]] || die "GitOps worktree must be clean"
  origin_url="$(git -C "${worktree}" remote get-url origin)"
  [[ "${origin_url}" == "https://github.com/${GITOPS_REPO}.git" || "${origin_url}" == "git@github.com:${GITOPS_REPO}.git" ]] || die "GitOps origin does not match the fixed repository or embeds credentials"
  gh auth status >/dev/null 2>&1; actor="$(gh api user --jq .login)"; actor_type="$(gh api user --jq .type)"
  [[ "${actor_type}" == "User" && "${actor,,}" == "05allan1213" ]] || die "regression actor must be the human repository owner"
  git -C "${worktree}" fetch --quiet origin main
  [[ "$(git -C "${worktree}" symbolic-ref --quiet --short HEAD)" == "main" ]] || die "GitOps worktree must be on branch main"
  [[ "$(git -C "${worktree}" rev-parse HEAD)" == "$(git -C "${worktree}" rev-parse origin/main)" ]] || die "GitOps worktree must be at exact origin/main"
  go -C "${ROOT_DIR}" run "${CONTRACT_PACKAGE}" healthy "${worktree}/apps/demo" >/dev/null || die "external GitOps base does not match the fixed five-file required-check contract"
  go -C "${ROOT_DIR}" run "${CONTRACT_PACKAGE}" regression "${HEALTHY_FIXTURE_DIR}" "${REGRESSION_FIXTURE_DIR}" >/dev/null || die "local regression fixture is not exactly REQUIRED_ENV removal"
  branch="regression/required-env-${SOURCE_SHA:0:12}"
  existing="$(gh pr list -R "${GITOPS_REPO}" --head "${branch}" --state all --json url --jq '.[0].url // empty')"
  [[ -z "${existing}" ]] || { printf '%s\n' "${existing}"; return 0; }
  original_json="$(yq -o=json -I=0 '.' "${worktree}/${GITOPS_PATH}")"
  expected_json="$(jq -c 'if ([.spec.template.spec.containers[]|select(.name=="demo")|.env[]|select(.name=="REQUIRED_ENV")]|length)!=1 then error("REQUIRED_ENV count must equal one") else (.spec.template.spec.containers[]|select(.name=="demo")|.env) |= map(select(.name!="REQUIRED_ENV")) end' <<<"${original_json}")" || die "external GitOps manifest must contain exactly one REQUIRED_ENV"
  git -C "${worktree}" switch -c "${branch}"
  yq -i '(.spec.template.spec.containers[] | select(.name == "demo") | .env) |= map(select(.name != "REQUIRED_ENV"))' "${worktree}/${GITOPS_PATH}"
  actual_json="$(yq -o=json -I=0 '.' "${worktree}/${GITOPS_PATH}")"
  jq -e --argjson expected "${expected_json}" '. == $expected' <<<"${actual_json}" >/dev/null || die "regression edit changed content beyond REQUIRED_ENV removal"
  changed="$(git -C "${worktree}" diff --name-status --no-renames origin/main --)"
  [[ "${changed}" == $'M\tapps/demo/deployment.yaml' ]] || die "regression branch must modify only apps/demo/deployment.yaml"
  git -C "${worktree}" add -- "${GITOPS_PATH}"
  git -C "${worktree}" diff --cached --quiet && die "regression patch is empty"
  git -C "${worktree}" commit -m "test(golden): remove REQUIRED_ENV for ${SOURCE_SHA:0:12}"
  git -C "${worktree}" push --set-upstream origin "${branch}"
  pr_url="$(gh pr create -R "${GITOPS_REPO}" --base main --head "${branch}" --title "Golden regression: remove REQUIRED_ENV (${SOURCE_SHA:0:12})" --body "CloudOps source exact SHA: ${SOURCE_SHA}\n\nDeterministic Golden regression only. This command does not merge the PR; a human must review and merge it.")"
  printf 'Regression actor=%s branch=%s PR=%s\n' "${actor}" "${branch}" "${pr_url}"
}

run_golden() {
  local regression delivery plans verifications resolution argo final_incidents plan checks load_pid="" bad_merge_at incident_at agent_at approval_at fix_merge_at argo_at verify_at
  [[ ! -e "${MANIFEST}" || "${GOLDEN_OVERWRITE_EVIDENCE:-false}" == true ]] || die "evidence manifest already exists: ${MANIFEST}"
  TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/cloudops-golden.XXXXXX")"; WRITE_MANIFEST=true
  require_env GOLDEN_REGRESSION_PR; REGRESSION_PR="${GOLDEN_REGRESSION_PR}"; [[ "${REGRESSION_PR}" =~ ^[1-9][0-9]*$ ]] || die "invalid GOLDEN_REGRESSION_PR"
  run_preflight
  regression="$(gh pr view "${REGRESSION_PR}" -R "${GITOPS_REPO}" --json author,headRefOid,mergeCommit,mergedAt,state,url)"
  jq -e '.state=="MERGED" and .mergeCommit.oid!=""' <<<"${regression}" >/dev/null || fail regression_pr "regression PR must be human-merged before e2e-gitops"
  REGRESSION_ACTOR="$(jq -r .author.login <<<"${regression}")"; REGRESSION_HEAD_SHA="$(jq -r .headRefOid <<<"${regression}")"; BAD_SHA="$(jq -r .mergeCommit.oid <<<"${regression}")"; bad_merge_at="$(jq -r .mergedAt <<<"${regression}")"
  is_sha "${BAD_SHA}" || fail regression_pr "regression merged SHA is invalid"; pass regression_pr "PR ${REGRESSION_PR} merged as ${BAD_SHA} by ${REGRESSION_ACTOR}"
  REGRESSION_ACTIONS="$(required_checks "${GITOPS_REPO}" "${REGRESSION_PR}")" || fail regression_ci "regression required checks are missing or not successful"; pass regression_ci "${REGRESSION_ACTIONS}"
  record_command "helm test ${DEMO_RELEASE} -n ${DEMO_NAMESPACE} --logs --timeout 31m"
  helm test "${DEMO_RELEASE}" -n "${DEMO_NAMESPACE}" --logs --timeout 31m >"${TMP_ROOT}/load-generator.log" 2>&1 & load_pid=$!
  argo="$(wait_for_json argo_json '.status.sync.revision=="'"${BAD_SHA}"'" and .status.sync.status=="Synced"')" || fail argo_bad "Argo did not observe the exact bad merged SHA"
  pass argo_bad "Argo synced bad revision ${BAD_SHA}"
  final_incidents="$(wait_for_json 'api_get "/api/v3/incidents?service=demo&limit=100"' '[.items[]|select(.created_at>="'"${bad_merge_at}"'" and (.migrated_legacy|not) and (.migrated_legacy_context|not))]|length>0')" || fail incident "no native current-cycle incident appeared after the bad merge"
  INCIDENT_ID="$(jq -r --arg at "${bad_merge_at}" '[.items[]|select(.created_at >= $at and (.migrated_legacy|not) and (.migrated_legacy_context|not))]|sort_by(.created_at)|last.id' <<<"${final_incidents}")"; is_uuid "${INCIDENT_ID}" || fail incident "incident public ID is invalid"; incident_at="$(jq -r --arg id "${INCIDENT_ID}" '.items[]|select(.id==$id)|.created_at' <<<"${final_incidents}")"; pass incident "Incident ${INCIDENT_ID} detected"
  plans="$(wait_for_json 'api_get "/api/v3/incidents/'"${INCIDENT_ID}"'/remediation-plans?limit=100"' '[.items[]|select(.operation_type=="restore_required_env" and .decision.decision=="approved" and (.migrated_legacy|not) and (.migrated_legacy_context|not))]|length==1')" || fail plan_approval "no unique native approved restore_required_env plan appeared"
  plan="$(jq -c '[.items[]|select(.operation_type=="restore_required_env" and .decision.decision=="approved" and (.migrated_legacy|not) and (.migrated_legacy_context|not))][0]' <<<"${plans}")"; PLAN_ID="$(jq -r .id <<<"${plan}")"; AGENT_RUN_ID="$(jq -r .created_by_agent_run_id <<<"${plan}")"; APPROVAL_ID="$(jq -r .decision.id <<<"${plan}")"; APPROVED_TREE_HASH="$(jq -r .decision.approved_tree_hash <<<"${plan}")"; APPROVED_POST_IMAGE_HASH="$(jq -r .decision.approved_post_image_hash <<<"${plan}")"; EVIDENCE_IDS="$(jq -r '[.evidence_bindings[].id]|join(",")' <<<"${plan}")"; agent_at="$(jq -r .created_at <<<"${plan}")"; approval_at="$(jq -r .decision.created_at <<<"${plan}")"; pass agent "AgentRun ${AGENT_RUN_ID} produced one bounded plan"; pass plan_approval "Plan ${PLAN_ID}, approval ${APPROVAL_ID}, hashes bound"
  delivery="$(wait_for_json 'api_get "/api/v3/incidents/'"${INCIDENT_ID}"'/delivery"' '.resource.status=="delivered" and .resource.merged_commit_sha!="" and (.resource.migrated_legacy|not) and (.resource.migrated_legacy_context|not)')" || fail remediation_pr "native delivery did not reach delivered"
  CHANGE_ID="$(jq -r .resource.id <<<"${delivery}")"; REMEDIATION_PR="$(jq -r .resource.pr_number <<<"${delivery}")"; REMEDIATION_HEAD_SHA="$(jq -r .resource.commit_sha <<<"${delivery}")"; FIX_SHA="$(jq -r .resource.merged_commit_sha <<<"${delivery}")"; argo_at="$(jq -r .resource.sync_completed_at <<<"${delivery}")"
  checks="$(gh pr view "${REMEDIATION_PR}" -R "${GITOPS_REPO}" --json headRefOid,mergeCommit,mergedAt,state)"; jq -e --arg head "${REMEDIATION_HEAD_SHA}" --arg fix "${FIX_SHA}" '.state=="MERGED" and .headRefOid==$head and .mergeCommit.oid==$fix' <<<"${checks}" >/dev/null || fail remediation_pr "remediation PR identity disagrees with durable delivery"; fix_merge_at="$(jq -r .mergedAt <<<"${checks}")"; pass remediation_pr "PR ${REMEDIATION_PR}, validated head ${REMEDIATION_HEAD_SHA}, merged ${FIX_SHA}"
  REMEDIATION_ACTIONS="$(required_checks "${GITOPS_REPO}" "${REMEDIATION_PR}")" || fail remediation_ci "remediation required checks are missing or not successful"; pass remediation_ci "${REMEDIATION_ACTIONS}"
  argo="$(argo_json)"; ARGO_REVISION="$(jq -r .status.sync.revision <<<"${argo}")"; ARGO_SYNC_REVISION="$(jq -r '.status.operationState.syncResult.revision' <<<"${argo}")"; [[ "${ARGO_REVISION}" == "${FIX_SHA}" && "${ARGO_SYNC_REVISION}" == "${FIX_SHA}" ]] || fail argo_fix "Argo deployed/syncResult revision does not equal fix SHA"; pass argo_fix "deployed and successful syncResult revision ${FIX_SHA}"
  verifications="$(api_get "/api/v3/incidents/${INCIDENT_ID}/verifications?limit=100")"; jq -e --arg sha "${FIX_SHA}" '[.items[]|select(.status=="passed" and .profile.id=="golden-required-env/v1" and .revisions.gitops_revision==$sha and .common_window.stability_window_ms==60000 and (.migrated_legacy|not) and (.migrated_legacy_context|not) and all(.checks[]; ((.migrated_legacy|not) and (.migrated_legacy_context|not) and ((.required|not) or .status=="passed"))))]|length==1' <<<"${verifications}" >/dev/null || fail verification "unique native Golden verification with all required checks did not pass"; VERIFICATION_ID="$(jq -r --arg sha "${FIX_SHA}" '.items[]|select(.status=="passed" and .revisions.gitops_revision==$sha and (.migrated_legacy|not) and (.migrated_legacy_context|not))|.id' <<<"${verifications}")"; verify_at="$(jq -r --arg id "${VERIFICATION_ID}" '.items[]|select(.id==$id)|.completed_at' <<<"${verifications}")"; pass verification "Verification ${VERIFICATION_ID} passed the common 60s window"
  resolution="$(api_get "/api/v3/incidents/${INCIDENT_ID}/resolution-report")"; jq -e --arg bad "${BAD_SHA}" --arg fix "${FIX_SHA}" '.resource.status=="resolved" and .resource.revisions.bad_gitops_revision==$bad and .resource.revisions.fix_gitops_revision==$fix and (.resource.migrated_legacy_context|not)' <<<"${resolution}" >/dev/null || fail resolution "native ResolutionReport does not bind the bad and fix revisions"; RESOLUTION_ID="$(jq -r .resource.id <<<"${resolution}")"; TOOL_CALLS="$(jq -r '.resource.agent_usage.tool_calls // "NOT RUN"' <<<"${resolution}")"; MODEL_CALLS="$(jq -r '.resource.agent_usage.model_calls // "NOT RUN"' <<<"${resolution}")"; TOKENS="$(jq -r '.resource.agent_usage.tokens // "NOT RUN"' <<<"${resolution}")"; PROMPT_HASH="$(jq -r '.resource.agent_usage.prompt_hash // "NOT RUN"' <<<"${resolution}")"; TOOL_SCHEMA_HASH="$(jq -r '.resource.agent_usage.tool_schema_hash // "NOT RUN"' <<<"${resolution}")"; pass resolution "ResolutionReport ${RESOLUTION_ID} binds the exact recovered cycle"
  BAD_TO_INCIDENT_MS="$(milliseconds_between "${bad_merge_at}" "${incident_at}")"; INCIDENT_TO_AGENT_MS="$(milliseconds_between "${incident_at}" "${agent_at}")"; AGENT_TO_APPROVAL_MS="$(milliseconds_between "${agent_at}" "${approval_at}")"; APPROVAL_TO_FIX_MS="$(milliseconds_between "${approval_at}" "${fix_merge_at}")"; FIX_TO_ARGO_MS="$(milliseconds_between "${fix_merge_at}" "${argo_at}")"; ARGO_TO_VERIFY_MS="$(milliseconds_between "${argo_at}" "${verify_at}")"; TOTAL_E2E_MS="$(milliseconds_between "${bad_merge_at}" "${verify_at}")"
  if kubectl top pods -A --no-headers >"${TMP_ROOT}/resources" 2>/dev/null; then RESOURCE_PEAK="NOT RUN (completion snapshot only: $(sort -k3 -hr "${TMP_ROOT}/resources" | head -1 | tr '\n' ' '))"; not_run resources "continuous peak sampling was not available; completion snapshot retained only"; else not_run resources "metrics API unavailable; resource peak NOT RUN"; fi
  wait "${load_pid}" || fail resources "Helm-owned load generator failed"
  DETAIL[resources]="Helm test completed and hook delete policy/TTL owns cleanup; resource peak remains NOT RUN"
  printf 'PASS: Golden E2E verified; manifest will be written at %s\n' "${MANIFEST}"
}

main() {
  case "${1:-}" in
    scenario-open-regression-pr) scenario_open_regression_pr ;;
    preflight) TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/cloudops-golden.XXXXXX")"; run_preflight; printf 'PASS: Golden E2E preflight\n' ;;
    run) run_golden ;;
    *) printf 'usage: %s {preflight|scenario-open-regression-pr|run}\n' "$0" >&2; return 64 ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
