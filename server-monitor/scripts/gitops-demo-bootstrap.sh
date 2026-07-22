#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FIXTURE_DIR="${ROOT_DIR}/deploy/contracts/gitops-demo/healthy/apps/demo"
CONTRACT_PACKAGE="./cmd/gitops-demo-contract"
GITOPS_REPOSITORY="05allan1213/cloudops-gitops-demo"
GITOPS_REMOTE_HTTPS="https://github.com/${GITOPS_REPOSITORY}.git"
GITOPS_REMOTE_SSH="git@github.com:${GITOPS_REPOSITORY}.git"
IMAGE_REPOSITORY="ghcr.io/05allan1213/cloudops-demo"
SOURCE_REPOSITORY="https://github.com/05allan1213/CloudOps-Copilot"
SOURCE_REMOTE_HTTPS="${SOURCE_REPOSITORY}.git"
SOURCE_REMOTE_SSH="git@github.com:05allan1213/CloudOps-Copilot.git"
BRANCH="bootstrap/demo-manifests"
TARGET_DIR="apps/demo"
EXPECTED_PATHS=$'apps/demo/deployment.yaml\napps/demo/diagnostics-service.yaml\napps/demo/podmonitor.yaml\napps/demo/prometheusrule.yaml\napps/demo/service.yaml'

die() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

require_env() {
  [[ -n "${!1:-}" ]] || die "missing required environment variable: $1"
}

source_revision=""
image_digest=""
image_reference=""
gitops_worktree=""
gitops_base_sha=""
actor=""

preflight() {
  local source_origin origin_url current_branch remote_digest labels remote_revision remote_source remote_branch existing_pr
  for command_name in git gh jq yq go docker rg install awk sed sort; do
    require_cmd "${command_name}"
  done
  require_env GOLDEN_GITOPS_WORKTREE
  require_env GOLDEN_DEMO_IMAGE_DIGEST
  require_env GOLDEN_DEMO_SOURCE_REVISION

  source_revision="${GOLDEN_DEMO_SOURCE_REVISION}"
  image_digest="${GOLDEN_DEMO_IMAGE_DIGEST}"
  [[ "${source_revision}" =~ ^[0-9a-f]{40}$ ]] || die "GOLDEN_DEMO_SOURCE_REVISION must be an exact lowercase 40-character Git SHA"
  [[ "${image_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || die "GOLDEN_DEMO_IMAGE_DIGEST must be an exact lowercase sha256 digest"
  [[ -z "$(git -C "${ROOT_DIR}" status --porcelain --untracked-files=normal)" ]] || die "CloudOps source worktree must be clean"
  [[ "$(git -C "${ROOT_DIR}" rev-parse HEAD)" == "${source_revision}" ]] || die "GOLDEN_DEMO_SOURCE_REVISION must equal the clean CloudOps HEAD"
  git -C "${ROOT_DIR}" cat-file -e "${source_revision}^{commit}" || die "CloudOps source revision is not a local commit"
  source_origin="$(git -C "${ROOT_DIR}" remote get-url origin)"
  [[ "${source_origin}" == "${SOURCE_REMOTE_HTTPS}" || "${source_origin}" == "${SOURCE_REMOTE_SSH}" ]] || die "CloudOps origin must be the fixed source repository without embedded credentials"

  image_reference="${IMAGE_REPOSITORY}@${image_digest}"
  remote_digest="$(docker buildx imagetools inspect "${image_reference}" --format '{{json .Manifest}}' | jq -r '.digest // empty')"
  [[ "${remote_digest}" == "${image_digest}" ]] || die "registry manifest digest does not equal GOLDEN_DEMO_IMAGE_DIGEST"
  labels="$(docker buildx imagetools inspect "${image_reference}" --format '{{json .Image.config.Labels}}')"
  remote_revision="$(jq -r '."org.opencontainers.image.revision" // empty' <<<"${labels}")"
  remote_source="$(jq -r '."org.opencontainers.image.source" // empty' <<<"${labels}")"
  [[ "${remote_revision}" == "${source_revision}" ]] || die "Demo image OCI revision does not equal GOLDEN_DEMO_SOURCE_REVISION"
  [[ "${remote_source}" == "${SOURCE_REPOSITORY}" ]] || die "Demo image OCI source repository is not the fixed CloudOps repository"
  unset labels

  gitops_worktree="$(cd "${GOLDEN_GITOPS_WORKTREE}" && pwd)"
  [[ "$(git -C "${gitops_worktree}" rev-parse --show-toplevel)" == "${gitops_worktree}" ]] || die "GOLDEN_GITOPS_WORKTREE must name the checkout root"
  [[ -z "$(git -C "${gitops_worktree}" status --porcelain --untracked-files=all)" ]] || die "GitOps checkout must be clean"
  origin_url="$(git -C "${gitops_worktree}" remote get-url origin)"
  [[ "${origin_url}" == "${GITOPS_REMOTE_HTTPS}" || "${origin_url}" == "${GITOPS_REMOTE_SSH}" ]] || die "GitOps origin must be the fixed external repository without embedded credentials"
  git -C "${gitops_worktree}" fetch --quiet --no-tags origin main
  current_branch="$(git -C "${gitops_worktree}" symbolic-ref --quiet --short HEAD)" || die "GitOps checkout must be on branch main"
  [[ "${current_branch}" == "main" ]] || die "GitOps checkout must be on branch main"
  [[ "$(git -C "${gitops_worktree}" rev-parse HEAD)" == "$(git -C "${gitops_worktree}" rev-parse refs/remotes/origin/main)" ]] || die "GitOps checkout must be at exact origin/main"
  gitops_base_sha="$(git -C "${gitops_worktree}" rev-parse HEAD)"
  [[ -z "$(git -C "${gitops_worktree}" ls-tree -r --name-only refs/remotes/origin/main -- "${TARGET_DIR}")" ]] || die "origin/main apps/demo base tree must be empty"
  [[ ! -e "${gitops_worktree}/${TARGET_DIR}" ]] || die "clean GitOps checkout unexpectedly contains apps/demo"
  git -C "${gitops_worktree}" show-ref --verify --quiet "refs/heads/${BRANCH}" && die "fixed bootstrap branch already exists locally"
  remote_branch="$(git -C "${gitops_worktree}" ls-remote --heads origin "refs/heads/${BRANCH}")"
  [[ -z "${remote_branch}" ]] || die "fixed bootstrap branch already exists on origin"

  gh auth status >/dev/null 2>&1 || die "gh has no authenticated human session"
  actor="$(gh api user --jq .login)"
  [[ "$(gh api user --jq .type)" == "User" && "${actor,,}" == "05allan1213" ]] || die "bootstrap PR must use the human repository-owner gh identity"
  [[ "$(gh repo view "${GITOPS_REPOSITORY}" --json nameWithOwner --jq .nameWithOwner)" == "${GITOPS_REPOSITORY}" ]] || die "gh identity cannot resolve the fixed GitOps repository"
  existing_pr="$(gh pr list -R "${GITOPS_REPOSITORY}" --head "${BRANCH}" --state all --json number --jq '.[0].number // empty')"
  [[ -z "${existing_pr}" ]] || die "a bootstrap/demo-manifests PR already exists"
}

render_and_validate() {
  local actual_paths path mode
  git -C "${gitops_worktree}" switch -c "${BRANCH}" --no-track
  mkdir -p "${gitops_worktree}/${TARGET_DIR}"
  for path in deployment.yaml diagnostics-service.yaml podmonitor.yaml prometheusrule.yaml service.yaml; do
    install -m 0644 "${FIXTURE_DIR}/${path}" "${gitops_worktree}/${TARGET_DIR}/${path}"
  done

  DEMO_IMAGE_REFERENCE="${image_reference}" DEMO_SOURCE_REVISION="${source_revision}" \
    yq -i '
      (.spec.template.spec.containers[] | select(.name == "demo") | .image) = strenv(DEMO_IMAGE_REFERENCE) |
      (.spec.template.spec.containers[] | select(.name == "demo") | .env[] | select(.name == "SERVICE_VERSION" or .name == "SOURCE_REVISION") | .value) = strenv(DEMO_SOURCE_REVISION)
    ' "${gitops_worktree}/${TARGET_DIR}/deployment.yaml"

  go -C "${ROOT_DIR}" run "${CONTRACT_PACKAGE}" healthy "${gitops_worktree}/${TARGET_DIR}"
  [[ "$(git -C "${gitops_worktree}" branch --show-current)" == "${BRANCH}" ]] || die "generated manifests are not on the fixed bootstrap branch"
  [[ "$(yq -r '.spec.template.spec.containers[] | select(.name == "demo") | .image' "${gitops_worktree}/${TARGET_DIR}/deployment.yaml")" == "${image_reference}" ]] || die "generated Deployment image does not equal the verified digest"
  [[ "$(yq -r '.spec.template.spec.containers[] | select(.name == "demo") | .env[] | select(.name == "SOURCE_REVISION") | .value' "${gitops_worktree}/${TARGET_DIR}/deployment.yaml")" == "${source_revision}" ]] || die "generated SOURCE_REVISION does not equal the verified source SHA"
  if rg -n 'contract-fixture|BEGIN [A-Z ]*PRIVATE KEY|github_pat_|gh[pousr]_[A-Za-z0-9_]{20,}' "${gitops_worktree}/${TARGET_DIR}" >/dev/null; then
    die "generated manifests contain a fixture sentinel or credential-shaped material"
  fi

  actual_paths="$(git -C "${gitops_worktree}" status --porcelain=v1 --untracked-files=all | sed -n 's/^?? //p' | sort)"
  [[ "${actual_paths}" == "${EXPECTED_PATHS}" ]] || die "bootstrap generation did not produce the exact five untracked files"

  git -C "${gitops_worktree}" add -- \
    apps/demo/deployment.yaml \
    apps/demo/diagnostics-service.yaml \
    apps/demo/podmonitor.yaml \
    apps/demo/prometheusrule.yaml \
    apps/demo/service.yaml
  [[ -z "$(git -C "${gitops_worktree}" diff --cached --name-only --diff-filter=CDMRTUXB refs/remotes/origin/main --)" ]] || die "bootstrap may only add files"
  actual_paths="$(git -C "${gitops_worktree}" diff --cached --name-only --diff-filter=A refs/remotes/origin/main -- | sort)"
  [[ "${actual_paths}" == "${EXPECTED_PATHS}" ]] || die "staged bootstrap diff is not the exact five-file inventory"
  while read -r mode _object _stage path; do
    [[ "${mode}" == "100644" ]] || die "bootstrap file must be a regular non-executable blob: ${path}"
  done < <(git -C "${gitops_worktree}" ls-files -s -- "${TARGET_DIR}")
  go -C "${ROOT_DIR}" run "${CONTRACT_PACKAGE}" healthy "${gitops_worktree}/${TARGET_DIR}"
}

publish_pr_without_merge() {
  local pr_url pr_json pr_api pr_number commit_sha remote_sha remote_main_sha actual_paths mode object object_id path
  git -C "${gitops_worktree}" commit -m "feat(gitops): bootstrap demo manifests for ${source_revision:0:12}"
  commit_sha="$(git -C "${gitops_worktree}" rev-parse HEAD)"
  [[ "$(git -C "${gitops_worktree}" rev-parse HEAD^)" == "$(git -C "${gitops_worktree}" rev-parse refs/remotes/origin/main)" ]] || die "bootstrap commit parent is not exact origin/main"
  [[ -z "$(git -C "${gitops_worktree}" status --porcelain --untracked-files=all)" ]] || die "GitOps checkout changed during commit"
  [[ -z "$(git -C "${gitops_worktree}" diff-tree --no-commit-id --name-only -r --no-renames --diff-filter=CDMRTUXB HEAD)" ]] || die "bootstrap commit may only add files"
  actual_paths="$(git -C "${gitops_worktree}" diff-tree --no-commit-id --name-only -r --no-renames --diff-filter=A HEAD | sort)"
  [[ "${actual_paths}" == "${EXPECTED_PATHS}" ]] || die "bootstrap commit is not the exact five-file inventory"
  while read -r mode object object_id path; do
    [[ "${mode}" == "100644" && "${object}" == "blob" && -n "${object_id}" ]] || die "bootstrap commit contains a non-regular manifest: ${path}"
  done < <(git -C "${gitops_worktree}" ls-tree -r HEAD -- "${TARGET_DIR}")
  go -C "${ROOT_DIR}" run "${CONTRACT_PACKAGE}" healthy "${gitops_worktree}/${TARGET_DIR}"
  remote_main_sha="$(git -C "${gitops_worktree}" ls-remote --heads origin refs/heads/main | awk '{print $1}')"
  [[ "${remote_main_sha}" == "${gitops_base_sha}" ]] || die "origin/main advanced after bootstrap validation"
  git -C "${gitops_worktree}" push --set-upstream origin "${BRANCH}"
  remote_sha="$(git -C "${gitops_worktree}" ls-remote --heads origin "refs/heads/${BRANCH}" | awk '{print $1}')"
  [[ "${remote_sha}" == "${commit_sha}" ]] || die "pushed bootstrap branch does not equal the validated commit"
  remote_main_sha="$(git -C "${gitops_worktree}" ls-remote --heads origin refs/heads/main | awk '{print $1}')"
  [[ "${remote_main_sha}" == "${gitops_base_sha}" ]] || die "origin/main advanced before bootstrap PR creation"
  pr_url="$(gh pr create -R "${GITOPS_REPOSITORY}" --base main --head "${BRANCH}" --draft \
    --title "Bootstrap Demo manifests (${source_revision:0:12})" \
    --body "CloudOps source exact SHA: ${source_revision}\nDemo image: ${image_reference}\n\nThis bootstrap adds only the fixed five-file apps/demo inventory. It never merges the PR; a human must review it and mark it ready.")"
  pr_json="$(gh pr view "${pr_url}" -R "${GITOPS_REPOSITORY}" --json author,baseRefName,headRefName,headRefOid,isDraft,mergedAt,number,state,url)"
  pr_number="$(jq -r '.number // empty' <<<"${pr_json}")"
  [[ "${pr_number}" =~ ^[1-9][0-9]*$ ]] || die "created bootstrap PR number is invalid"
  pr_api="$(gh api "repos/${GITOPS_REPOSITORY}/pulls/${pr_number}")"
  jq -e --arg actor "${actor}" --arg branch "${BRANCH}" --arg commit_sha "${commit_sha}" '
    .author.login == $actor and .baseRefName == "main" and .headRefName == $branch and
    .headRefOid == $commit_sha and .isDraft == true and .mergedAt == null and .state == "OPEN"
  ' <<<"${pr_json}" >/dev/null || die "created bootstrap PR identity or non-merge state is invalid"
  jq -e --arg base_sha "${gitops_base_sha}" --arg commit_sha "${commit_sha}" '
    .base.sha == $base_sha and .head.sha == $commit_sha and .draft == true and .merged == false
  ' <<<"${pr_api}" >/dev/null || die "created bootstrap PR base/head SHA contract is invalid"
  printf 'PASS: demo bootstrap branch pushed and draft PR created without merge: %s\n' "${pr_url}"
}

preflight
render_and_validate
publish_pr_without_merge
