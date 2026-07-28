# ADR 0003: MySQL Async Tasks and No Kafka/Redis

- Status: Accepted target decision; implementation NOT RUN
- Date: 2026-07-18
- Owner: Phase 2

## Context

MySQL already holds durable domain truth. The current implementation also has Kafka/Redis dependencies, three independent domain leases and a transactional domain-event outbox that has no relay or claim API.

## Decision

Use transactional enqueue plus a MySQL-backed durable work queue with at-least-once semantics. async_tasks and async_task_attempts are the only task/lease truth. Claims use queue-scoped short transactions, FOR UPDATE SKIP LOCKED, MySQL NOW(6), lease generation and expected subject version.

Task types are limited to investigation.advance, remediation.prepare, change.ensure_pr, delivery.observe and verification.advance. Four worker pools have independent bounded semaphores.

Do not use Kafka or Redis in V3. Do not claim exactly-once.

The audited outbox_events rows are domain-event audit records, not jobs. They are archived by published state/event type and never directly converted into async tasks. Cutover tasks derive only from existing target tasks or versioned non-terminal child converters with an anti-join.

## Consequences

- AgentRun, ChangeRequest and VerificationRun leases become read-only compatibility fields, then are removed in Phase 7B.
- External-write idempotency additionally requires durable intent and provider reconciliation.
- Dead replay creates a new generation and revalidates subject version.

## Rejected Alternatives

- Strimzi/Kafka relay/inbox/DLQ for this low-throughput single-store scope.
- Redis queue/lock/session as durable truth.
- In-memory goroutines as workflow scheduler.

## Evidence Required

Real MySQL claim/takeover/dead/replay/anti-join tests, index EXPLAIN, stale-writer negatives, pool isolation and graceful shutdown.
