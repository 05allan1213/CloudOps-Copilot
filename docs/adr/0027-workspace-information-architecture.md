# ADR 0027: Workspace Information Architecture and Context Links

- Status: Accepted information-architecture decision; implementation NOT RUN
- Date: 2026-07-26
- Supersedes: The two-product-page navigation constraint in ADR 0001 and the existing frontend redesign plan

## Context

The current sidebar exposes only Incident Workbench, while monitoring, Alerts, logs, traces, Agent activity, delivery and configuration are either absent, embedded in one long Incident page or reachable only by manually opening external services. That structure hides the product core and makes navigation state difficult to preserve.

## Decision

The native product navigation is grouped into these first-class Workspaces:

| Group | Workspace | Route |
|---|---|---|
| Home | Overview | `/overview` |
| Cloud Native | Infrastructure | `/infrastructure` |
| Cloud Native | Monitoring | `/monitoring` |
| Cloud Native | Alerts | `/alerts` |
| Cloud Native | Logs | `/logs` |
| Cloud Native | Traces | `/traces` |
| Intelligent Response | Agent | `/agent` |
| Intelligent Response | Incidents | `/incidents` |
| Delivery | DevOps | `/devops` |
| System | Settings | `/settings` |

`/overview` is the default product entry. Each Workspace owns deep-linkable list, filter, time-range, selection and detail state rather than hiding those states in one page-level scroll position.

Every Workspace must expose relevant Context Links. Internal links use native routes. Optional expert links to GitHub, Grafana, Kibana, Tempo, Argo CD or another allowlisted cloud-native service must target the exact repository, commit, pull request, application, resource, query or time range whenever the provider supports it. The UI clearly identifies external destinations and opens them without discarding the current CloudOps context.

Primary workflows remain native as required by ADR 0021. Context Links supplement those workflows; they do not replace missing CloudOps interfaces.

## Consequences

- The existing four-route router and single-item navigation contract must be replaced.
- Backend response models need typed related-resource links or enough safe identity data for a trusted link builder; arbitrary provider URLs cannot be rendered directly from untrusted payloads.
- Cross-Workspace navigation must carry resource identity and time context, for example Metric to Logs, Alert to Incident, Kubernetes workload to Agent Consultation, and Delivery to exact GitHub or Argo evidence.
- Broken or unavailable external providers must not break native navigation and must produce an explicit unavailable state.
- Desktop and mobile navigation may present the hierarchy differently, but both expose all Workspaces without manual URL entry.

## Rejected Alternatives

- Keep Incident List and Incident Detail as the only product routes.
- Add navigation labels that still render every capability inside one page.
- Send users to provider home pages and require them to reconstruct the investigation context manually.
