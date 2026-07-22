#!/usr/bin/env bash
# shellcheck disable=SC2016 # Contract needles intentionally contain literal shell placeholders.
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNNER="${ROOT_DIR}/server-monitor/scripts/golden-e2e.sh"
BOOTSTRAP="${ROOT_DIR}/server-monitor/scripts/gitops-demo-bootstrap.sh"
ROOT_MAKEFILE="${ROOT_DIR}/Makefile"
SERVER_MAKEFILE="${ROOT_DIR}/server-monitor/Makefile"
HEALTHY_DIR="${ROOT_DIR}/deploy/contracts/gitops-demo/healthy/apps/demo"
REGRESSION_DIR="${ROOT_DIR}/deploy/contracts/gitops-demo/regression/apps/demo"

die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

bash -n "${RUNNER}"
bash -n "${BOOTSTRAP}"
for target in demo-bootstrap-pr scenario-open-regression-pr e2e-gitops golden-e2e-contracts; do
  grep -Eq "^${target}:" "${ROOT_MAKEFILE}" || die "root Makefile is missing ${target}"
  grep -Eq "^${target}:" "${SERVER_MAKEFILE}" || die "server-monitor Makefile is missing ${target}"
done

for required in \
  'Status: `AGENT_QUALITY=PASS`' \
  'gh auth status' \
  '/git/refs' \
  '/pulls' \
  '/branches/main/protection' \
  'permission probe unexpectedly created a Git ref' \
  'permission probe unexpectedly created a pull request' \
  'argocd account can-i sync' \
  'current Kubernetes context is not kind' \
  'live LLM response contract failed' \
  '/api/v3/session/csrf' \
  'org.opencontainers.image.revision' \
  'required --json name,state,link' \
  'status.operationState.syncResult.revision' \
  'docs/evidence/${SOURCE_SHA}' \
  'NOT RUN'; do
  grep -Fq "${required}" "${RUNNER}" || die "runner is missing contract: ${required}"
done
if grep -Fq 'https://api.github.com/installation' "${RUNNER}"; then
  die "runner uses the user-to-server /installation endpoint for installation tokens"
fi

grep -Fq 'gh pr create' "${RUNNER}" || die "regression command does not create a PR"
grep -Fq 'regression/required-env-${SOURCE_SHA:0:12}' "${RUNNER}" || die "regression branch does not match the external required check"
grep -Fq 'select(.name=="demo")' "${RUNNER}" || die "regression command does not target container demo"
if grep -Eq 'gh pr merge|argocd app (sync|rollback)|kubectl (apply|patch|scale|delete)' "${RUNNER}" "${BOOTSTRAP}"; then
  die "runner contains a forbidden merge, Argo mutation, or direct Kubernetes repair"
fi
grep -Fq 'helm test' "${RUNNER}" || die "Golden-owned load generator is not started through Helm test"
grep -Fq 'regression fixture is not exactly REQUIRED_ENV removal' "${RUNNER}" || die "regression semantic guard is missing"
grep -Fq 'Credentials and provider raw responses are never persisted' "${RUNNER}" || die "secret-safe evidence declaration is missing"

for required in \
  'BRANCH="bootstrap/demo-manifests"' \
  'CloudOps source worktree must be clean' \
  'GitOps checkout must be clean' \
  'GitOps checkout must be at exact origin/main' \
  'origin/main apps/demo base tree must be empty' \
  'bootstrap PR must use the human repository-owner gh identity' \
  'CloudOps origin must be the fixed source repository without embedded credentials' \
  'GOLDEN_DEMO_IMAGE_DIGEST' \
  'GOLDEN_DEMO_SOURCE_REVISION' \
  'docker buildx imagetools inspect' \
  'gh pr create' \
  'git -C "${gitops_worktree}" push' \
  'bootstrap commit is not the exact five-file inventory' \
  'pushed bootstrap branch does not equal the validated commit' \
  'origin/main advanced before bootstrap PR creation' \
  '.base.sha == $base_sha' \
  '--draft'; do
  grep -Fq -- "${required}" "${BOOTSTRAP}" || die "bootstrap command is missing contract: ${required}"
done
for path in deployment.yaml diagnostics-service.yaml podmonitor.yaml prometheusrule.yaml service.yaml; do
  grep -Fq "apps/demo/${path}" "${BOOTSTRAP}" || die "bootstrap command is missing fixed file ${path}"
done
if grep -Fq 'gh pr merge' "${BOOTSTRAP}"; then
  die "bootstrap command may not merge its PR"
fi

go -C "${ROOT_DIR}" run ./cmd/gitops-demo-contract healthy "${HEALTHY_DIR}" >/dev/null
go -C "${ROOT_DIR}" run ./cmd/gitops-demo-contract regression "${HEALTHY_DIR}" "${REGRESSION_DIR}" >/dev/null
fixture_image="$(yq -r '.spec.template.spec.containers[] | select(.name == "demo") | .image' "${HEALTHY_DIR}/deployment.yaml")"
fixture_revision="$(yq -r '.spec.template.spec.containers[] | select(.name == "demo") | .env[] | select(.name == "SOURCE_REVISION") | .value' "${HEALTHY_DIR}/deployment.yaml")"
[[ "${fixture_image}" =~ @sha256:[0-9a-f]{64}$ && "${fixture_image}" != *@sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] || die "healthy fixture image is mutable or fake"
[[ "${fixture_revision}" =~ ^[0-9a-f]{40}$ ]] || die "healthy fixture source revision is not exact"
git -C "${ROOT_DIR}" cat-file -e "${fixture_revision}^{commit}" || die "healthy fixture source revision is not a repository commit"
if rg -n 'contract-fixture' "${HEALTHY_DIR}" "${REGRESSION_DIR}" >/dev/null; then
  die "fixtures contain a sentinel"
fi

printf 'PASS: Golden E2E shell and evidence contracts\n'
