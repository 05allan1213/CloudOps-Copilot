#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

scan_paths=(
  cmd
  internal
  frontend/src
  frontend/tests
  runbooks
  eval
  charts/cloudops
  scripts
  migrations
  Makefile
)

path_pattern='(^|/)(v2|v3|phase[-_]?([0-9]+))([._/-]|$)|values-phase'
content_pattern='(/api/v[23])|\b(V[23]_[A-Z0-9_]+)\b|\b(v[23]_[a-z0-9_]+)\b|\b(phase[-_]?[0-9]+)\b|cloudops[-_]v[23]\b|incident-agent-v[23]\b|domain_schema_version|v3_status|write_phase'

path_failures="$(find "${scan_paths[@]}" -print 2>/dev/null | grep -Eni "${path_pattern}" || true)"
content_failures="$(grep -REni --binary-files=without-match \
  --exclude='check-first-party-naming.sh' \
  --exclude='*.snap' \
  --exclude='package-lock.json' \
  --exclude-dir='node_modules' \
  "${content_pattern}" "${scan_paths[@]}" 2>/dev/null | \
  grep -Ev '^(internal/api/contract_test\.go|internal/router/(api|internal)_test\.go):[0-9]+:.*(/api/v[23])' | \
  grep -Ev '^internal/infra/alertmanagerapi/(adapter|adapter_test)\.go:[0-9]+:.*"/api/v2/silence(s|/)' | \
  grep -Ev '^scripts/local-lifecycle\.sh:[0-9]+:.*\/api\/v2\/alerts' | \
  grep -Ev '^(migrations/baseline_test\.go|internal/api/(resolution_report|workbench_contract)_test\.go|internal/taskhandler/evidence_authority_test\.go):[0-9]+:' || true)"

if [[ -n "${path_failures}" || -n "${content_failures}" ]]; then
  [[ -z "${path_failures}" ]] || printf '%s\n' "${path_failures}" >&2
  [[ -z "${content_failures}" ]] || printf '%s\n' "${content_failures}" >&2
  printf 'FAIL: first-party generation or numbered delivery identity remains\n' >&2
  exit 1
fi

printf 'PASS: first-party implementation naming is semantic and generation-free\n'
