# ADR 0034: Guided and Expert Observability Queries

- Status: Accepted observability-query and authorization decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0021 and ADR 0026

## Context

The existing observability adapters execute fixed, bounded templates for Agent Investigation and recovery verification. They do not provide native user-facing Monitoring, Logs or Traces APIs. Restricting the new Workspaces to those templates would make everyday exploration too shallow, while sending every query directly from the browser to Prometheus, Elasticsearch or Tempo would expose credentials and fragment the product experience.

## Decision

Monitoring, Logs and Traces each provide two native query levels.

The default guided level uses Chinese-first filters, resource and time selectors, golden-signal views, log streams and trace search without requiring a provider query language. The expert level exposes canonical professional query languages: PromQL for metrics, a bounded Elasticsearch query surface for logs and TraceQL for traces.

All queries execute through typed CloudOps backend APIs. Provider credentials and mutable provider endpoints never reach the browser. The backend enforces Operational Scope, maximum lookback, timeout, concurrency, response bytes, series, samples, log rows and trace counts. Results expose provider identity, effective query time, collection time, truncation, staleness and partial-failure state and remain cancellable while running.

The Owner can save a successful query as an immutable-versioned Query Definition. Saved views reference a Query Definition rather than copying mutable query text. Query Definition edits create a new version so history and Evidence retain their original meaning.

Agent Investigation and Agent Consultation continue to use bounded typed tools by default. The Agent may draft an expert query and explain why it is needed. It may execute the draft only after Owner authorization, or execute an already authorized Query Definition within its declared provider, Operational Scope and resource bounds. A query result becomes Evidence only through the normal sanitized provenance contract; raw provider responses and secrets are never inserted directly into model context.

Query Authorization has two forms. `Run once` binds one execution to the exact normalized query, effective parameters, Provider and Operational Scope. `Save and authorize` binds repeat execution to one immutable Query Definition version, Provider, cluster and Namespace scope, maximum lookback and response limits. Persistent authorization has no arbitrary time expiry in Local Owner Mode, but remains explicitly revocable.

Every Agent execution is visible in its timeline and cancellable. Editing a Query Definition or changing its Provider, scope or resource bounds creates a new version and invalidates the previous authorization for future work. A new query or requested scope expansion always requires new authorization; earlier consent cannot authorize future text implicitly.

## Consequences

- The backend needs separate user-query contracts in addition to the current fixed Agent and verification adapters.
- Query parsing, normalization, cost bounds, cancellation and result shaping are provider-specific backend responsibilities.
- Guided and expert modes share URL state and the same result model, allowing a guided query to be inspected or saved without losing context.
- Saved queries, dashboards and Agent Evidence record the exact Query Definition version and effective parameters.
- Query Authorization and revocation require durable audit records and concurrency-safe checks immediately before execution.
- Provider query syntax remains untranslated even though surrounding UI, validation and explanations are Chinese-first.

## Rejected Alternatives

- Expose only fixed dashboards and Agent templates.
- Give the frontend direct Provider API access or credentials.
- Give the Agent permanent unrestricted raw-query execution.
- Require the Owner to manually reproduce every Agent-suggested query in an external console.
