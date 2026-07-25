# ADR 0025: Cloud-Native Operations and Agent Are the Product Core

- Status: Accepted product-priority decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0018

## Context

The earlier Incident-only design made GitHub and Argo CD delivery central to the product narrative. The expanded personal product instead needs to make cloud-native system state and Agent capability immediately visible and useful, including when no GitOps change is required.

## Decision

Cloud-native operations and the Incident Agent are the product core. Kubernetes state, metrics and Alerts, logs, traces, their correlations, and Agent-assisted investigation form the primary product experience.

DevOps and GitOps remain supporting capabilities. They provide change context, controlled delivery and recovery evidence when relevant, but they are not the main product identity and are not a mandatory stage of every Alert, Incident or Agent investigation.

## Consequences

- Product navigation, first-screen hierarchy and demo narratives must foreground cloud-native state and Agent reasoning before GitOps delivery.
- The product must remain useful for observation, diagnosis, explanation and no-change recovery without creating a pull request.
- GitHub and Argo CD integrations remain available through native CloudOps views, but they cannot dominate the information architecture.
- Agent value must be demonstrated with attributable metric, Alert, log, trace and Kubernetes evidence, not only with a successful remediation PR.

## Rejected Alternative

Keep a GitOps remediation pipeline as the only complete story and treat observability and Agent behavior merely as inputs to that pipeline.
