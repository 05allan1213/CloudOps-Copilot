# ADR 0030: Versioned Local Configuration

- Status: Accepted configuration decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0024

## Context

The current API and workers load environment variables at startup and construct provider clients once. This makes changing an LLM model, Kubernetes scope or observability provider require editing deployment configuration and restarting processes, while exposing all startup values through a settings UI would make bootstrapping and failure recovery circular.

## Decision

CloudOps separates Bootstrap Configuration from Operational Configuration.

Bootstrap Configuration contains only the values required to start the local processes and reach durable configuration, including the listen boundary, MySQL connection and backend-owned data directory. The Settings Workspace exposes effective bootstrap values as read-only diagnostics. Changing them remains an explicit local startup action and requires a local restart.

Operational Configuration includes LLM provider, model and limits; Kubernetes contexts and namespace scope; Prometheus, Alertmanager, Elasticsearch and Tempo connections; GitHub and Argo CD integrations; external Context Link bases; provider allowlists; and related runtime controls. The Owner edits these values through a native Settings API and Chinese-first Settings Workspace. Non-secret values are stored as immutable Configuration Revisions in MySQL.

Secret inputs are write-only. The backend stores them as private, revision-addressable local files with mode `0600`; API responses expose only configured, missing or invalid state and non-sensitive fingerprints. A secret value is never returned to the browser, logs or Git, and changing a secret creates a new secret version rather than overwriting the value referenced by in-progress work.

Applying Operational Configuration first validates its schema, bounded targets and provider connectivity. A successful apply atomically publishes a new Configuration Revision. New queries and new Agent runs use the latest active revision. Each Agent Investigation records and retains the revision it started with so its model, tools and Evidence cannot change meaning midway through the run.

API and worker processes observe active revision changes and rebuild affected provider clients at request or task boundaries. Applying Operational Configuration does not automatically restart a process or interrupt active work. Configuration that cannot be reloaded safely belongs in Bootstrap Configuration instead.

## Consequences

- The backend needs typed settings, validation, connection-test, secret-write and revision APIs rather than exposing raw environment variables.
- Configuration application must be atomic: a failed validation or client construction leaves the previous active revision intact.
- Provider health and last validation results are visible independently from whether a revision is active.
- Historical revisions redact secret values but retain enough identity to explain which configuration produced an Agent result.
- Secret-version cleanup must retain every version referenced by active or retained work.
- No web action is allowed to restart the API, worker, Docker or Kubernetes runtime implicitly.

## Rejected Alternatives

- Keep all configurable values in environment variables and require manual restarts.
- Store one mutable configuration row or overwrite secret files in place.
- Let the web process restart itself or external runtimes after every save.
- Return stored credentials to the browser for convenient editing.
