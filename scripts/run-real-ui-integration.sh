#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run_id="${CLOUDOPS_REAL_INTEGRATION_RUN_ID:?CLOUDOPS_REAL_INTEGRATION_RUN_ID is required}"
source_head="${CLOUDOPS_REAL_INTEGRATION_SOURCE_HEAD:?CLOUDOPS_REAL_INTEGRATION_SOURCE_HEAD is required}"
base_url="${CLOUDOPS_REAL_INTEGRATION_BASE_URL:-http://127.0.0.1:18080}"
invocation_id="${CLOUDOPS_REAL_INTEGRATION_INVOCATION_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"

if [[ ! "$run_id" =~ ^[A-Za-z0-9._-]+$ || ! "$invocation_id" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "FAIL: run and invocation identities may only contain letters, digits, dot, underscore, and dash" >&2
  exit 1
fi
export CLOUDOPS_REAL_INTEGRATION_INVOCATION_ID="$invocation_id"

if [[ "$(git -C "$repo_root" rev-parse HEAD)" != "$source_head" ]]; then
  echo "FAIL: source HEAD differs from CLOUDOPS_REAL_INTEGRATION_SOURCE_HEAD" >&2
  exit 1
fi
curl -fsS "$base_url/readyz" >/dev/null

cd "$repo_root/frontend"
exec npx playwright test --config playwright.real-integration.config.ts "$@"
