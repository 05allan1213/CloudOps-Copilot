# ADR 0045: Single V1 Contract and Semantic Code

- Status: Accepted naming and migration decision; implementation NOT RUN
- Date: 2026-07-26
- Supersedes: Product-generation and code-phase naming in ADR 0001 through ADR 0017
- Refines: ADR 0018, ADR 0027, ADR 0030, ADR 0038 and ADR 0040

## Context

The repository currently describes successive V2 and V3 products and embeds numbered implementation phases into API paths, runtime types, database columns, environment variables, Helm profiles, scripts, tests and resource names. CloudOps is one personal product with one current contract, so those labels create false parallel generations and make historical delivery sequencing part of the permanent architecture.

## Decision

CloudOps has one supported Product Contract: V1. V2 and V3 do not exist as product generations. The product is presented to the Owner simply as CloudOps; V1 appears only where an explicit first-party contract identity is useful, including the public `/api/v1` boundary and its OpenAPI description.

CloudOps-owned implementation names are semantic and generation-free. Packages, types, functions, tables, columns, indexes, environment variables, configuration keys, Helm values, Kubernetes resource names, scripts, Make targets, fixtures and tests do not contain V2, V3 or a numbered phase. V1 is not added as a prefix to ordinary internal names; contract-specific adapters may use it only when they genuinely distinguish the public V1 shape from an internal domain model.

Implementation documents may use numbered Phases to express dependency order, scope and acceptance boundaries. Those labels are planning metadata only and cannot become runtime profiles, feature flags, code ownership names, data values, file names or resource identities.

Existing CloudOps-owned V2, V3 and code-phase names must be removed rather than retained as permanent compatibility aliases. API consumers move to `/api/v1`; active data is transformed into the semantic V1 schema; clean installations use one squashed semantic baseline. Because retained local domain history must survive, the transition uses a verified backup and one-time data transformation before obsolete migrations and compatibility code are removed from the final tree.

Technical versions owned by external contracts remain intact. Examples include Kubernetes and Helm `apiVersion`, Prometheus provider paths, Go module major suffixes, dependency and image versions, and external fields such as the Argo CD operation phase. CloudOps maps external terms into its own semantic model where they reach the UI or domain, but does not corrupt provider payloads or required manifests.

Immutable revisions remain valid domain concepts. Configuration Revisions, Knowledge Item revisions, Evidence schema identities, query definitions and optimistic row versions use explicit revision IDs, content hashes or schema identities; they are not presented as V2 or V3 product generations.

Current checked-in specifications, reports and ADR navigation must stop describing V2 or V3 as real products. Relevant facts are consolidated under the V1 platform design, while Git history remains unchanged and continues to provide historical provenance.

## Consequences

- The naming cleanup is a cross-stack migration, not a search-and-replace task.
- The final repository needs an automated first-party naming check with narrow allowlists for external protocol and dependency versions.
- Existing local data must be backed up, transformed, restored and integration-tested before obsolete schema artifacts are removed.
- Implementation Phases may be documented, but each Phase is delivered as a vertical frontend, backend, data and provider capability rather than encoded into the product.
- No V2 or V3 compatibility surface remains in the completed V1 runtime.

## Rejected Alternatives

- Keep separate V2 and V3 routes, runtimes or schemas during normal operation.
- Rename every current internal symbol from V3 to V1 while preserving generation-prefixed architecture.
- Remove mandatory third-party protocol versions through blind textual replacement.
- Delete existing local data or rewrite Git history solely to simplify the naming migration.
