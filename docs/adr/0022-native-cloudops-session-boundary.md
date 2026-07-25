# ADR 0022: Native CloudOps Session Boundary

- Status: Superseded by ADR 0024; retained as the historical authenticated-session decision
- Date: 2026-07-26
- Extends: ADR 0009 and ADR 0021

## Context

The unified platform must eliminate repeated login prompts across its own workspaces. Extending the same browser session into Grafana, Kibana, Argo CD and other provider consoles would require a broader identity federation and role-mapping architecture, while the primary workflows are intended to be available natively in CloudOps.

## Decision

One CloudOps login authorizes the Owner across every native CloudOps workspace as defined by ADR 0023. Provider-backed data and operations are accessed through CloudOps backend contracts, so moving between monitoring, alerts, logs and traces, Incidents, Agent, DevOps and settings does not require another login.

Optional external expert consoles are outside this session contract. They may require their own authentication. The CloudOps OAuth credential, access token and session cookie must not be forwarded or repurposed to create provider-console sessions.

## Consequences

- The frontend uses one application session and one consistent authentication-expiry flow across all native routes.
- Backend provider adapters use their own bounded machine identities or read credentials and enforce CloudOps authorization before access.
- A user can complete primary workflows without opening a provider console, so external authentication cannot block core product use.
- Future transparent cross-console SSO requires a separate ADR and explicit identity-provider, logout, role-mapping and token-audience contracts.

## Rejected Alternative

Require the first redesign release to federate one browser session into every external provider console. That expands the security boundary without improving native CloudOps workflows.
