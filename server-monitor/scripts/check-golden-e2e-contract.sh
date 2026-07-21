#!/usr/bin/env bash
# shellcheck disable=SC2016 # Contract needles intentionally contain literal shell placeholders.
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNNER="${ROOT_DIR}/server-monitor/scripts/golden-e2e.sh"
ROOT_MAKEFILE="${ROOT_DIR}/Makefile"
SERVER_MAKEFILE="${ROOT_DIR}/server-monitor/Makefile"

die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

bash -n "${RUNNER}"
for target in scenario-open-regression-pr e2e-gitops golden-e2e-contracts; do
  grep -Eq "^${target}:" "${ROOT_MAKEFILE}" || die "root Makefile is missing ${target}"
  grep -Eq "^${target}:" "${SERVER_MAKEFILE}" || die "server-monitor Makefile is missing ${target}"
done

for required in \
  'Status: `AGENT_QUALITY=PASS`' \
  'gh auth status' \
  'https://api.github.com/installation' \
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

grep -Fq 'gh pr create' "${RUNNER}" || die "regression command does not create a PR"
if grep -Eq 'gh pr merge|argocd app (sync|rollback)|kubectl (apply|patch|scale|delete)' "${RUNNER}"; then
  die "runner contains a forbidden merge, Argo mutation, or direct Kubernetes repair"
fi
grep -Fq 'helm test' "${RUNNER}" || die "Golden-owned load generator is not started through Helm test"
grep -Fq 'regression fixture is not exactly REQUIRED_ENV removal' "${RUNNER}" || die "regression semantic guard is missing"
grep -Fq 'Credentials and provider raw responses are never persisted' "${RUNNER}" || die "secret-safe evidence declaration is missing"

printf 'PASS: Golden E2E shell and evidence contracts\n'
