# ADR 0002: Root Module and API/Worker/Migrate Processes

- Status: Accepted target decision; implementation NOT RUN
- Date: 2026-07-18
- Owner: Phase 1

## Context

The current repository has module server-web plus module server-monitor/pkg. The server-web process serves HTTP and starts Agent, Remediation and Delivery/Verification loops. Runtime startup also invokes AutoMigrate.

## Decision

Use one root Go module with three entrypoints:

~~~text
cmd/cloudops-api
cmd/cloudops-worker
cmd/cloudops-migrate
~~~

cloudops-api owns webhook ingestion, MySQL-backed Query/Command/SSE, auth validation, health/metrics and built frontend assets. It never runs Agent, Delivery or Verification and has no Kubernetes token.

cloudops-worker owns the four async task pools and external adapters. It exposes only a management listener.

cloudops-migrate owns Goose under an advisory lock and runs as a Helm pre-install/pre-upgrade Job with a distinct DDL identity. Runtime binaries never AutoMigrate.

The move is feature-first and mechanical in Phase 1. No parallel V3 implementation or generic internal SDK is introduced. The current frontend stays in place until its single Phase 6 migration/adaptation.

## Consequences

- server-monitor/pkg is absorbed and its module/replace deleted after root builds pass.
- API and Worker health/config/termination contracts are separate.
- Any still-required legacy AutoMigrate schema must first gain explicit forward-Goose ownership.

## Rejected Alternatives

- Keep multiple Go modules.
- Keep all loops in server-web behind feature flags.
- Split each domain into a microservice.

## Evidence Required

Root test/race/vet/build, three binary builds, API negative goroutine/import checks, runtime no-AutoMigrate and process-specific shutdown/health tests.
