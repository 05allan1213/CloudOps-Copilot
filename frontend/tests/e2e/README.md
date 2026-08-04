# Frontend Browser Tests

The Playwright suite validates deterministic presentation and interaction contracts against the local fixture server. Coverage includes navigation, URL and scroll restoration, responsive layout, focus management, unavailable states, SSE reconnect behavior, command feedback and operation identity.

The fixture server returns deterministic `/api/v1` responses. It does not prove the MySQL-backed API, production SSE runtime or Provider effects. Tests under `tests/real-integration/` cover the separate browser -> API -> MySQL/Provider -> refreshed UI boundary.

Run the fixture suite with:

```bash
npm run test:e2e
```

Run the stable read-only subset with:

```bash
npm run test:e2e:stable
```

Run the focused Incident and DevOps workspace suite with:

```bash
npm run test:e2e:incident-devops
```

The real integration suite requires a running local CloudOps deployment, a unique run ID and the current source revision. The independent Scope contract also requires its managed test environment:

```bash
export CLOUDOPS_REAL_INTEGRATION_RUN_ID="integration-$(date -u +%Y%m%dT%H%M%SZ)"
export CLOUDOPS_REAL_INTEGRATION_SOURCE_HEAD="$(git rev-parse HEAD)"
make real-integration-scope-up
scripts/run-real-ui-integration.sh
make real-integration-scope-down
```

Playwright reports, traces, screenshots and test results are generated locally and must not be committed.
