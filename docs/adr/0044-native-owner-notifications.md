# ADR 0044: Native Owner Notifications

- Status: Accepted product decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0020, ADR 0024, ADR 0027 and ADR 0036

## Context

Long-running investigations and operational actions can finish while the Owner is viewing another Workspace or another browser tab. This personal local project needs a reliable record and selective attention cues, but email, SMS and chat integrations would add configuration and delivery complexity without improving the primary local workflow.

## Decision

The native Notification Inbox is the authoritative, durable record of Owner Notifications and read state. Each notification identifies its source event, severity, creation time and exact Context Link. Navigating to a notification resumes the relevant Alert, Agent Investigation, Agent Consultation, authorization request or Operation result rather than opening a generic Workspace or provider home page.

Browser system notifications are an optional mirror enabled by one explicit browser permission. They are emitted only while the CloudOps page is open but not in the foreground, and only for a new or materially changed P1/P2 Alert, an Agent Investigation completion or failure, an action waiting for authorization, or an Operation completion or failure. Foreground activity uses the native interface and Notification Inbox without duplicating a system notification.

Lower-severity events remain in the Notification Inbox. Notifications are grouped and deduplicated by source identity and material state transition, with cooldowns that prevent repeated firing updates from interrupting the Owner. Raw Metrics, Logs, Traces and Kubernetes watch changes never notify directly; they must first become an Alert, Agent outcome or other defined operational event.

The first redesign does not implement or expose configuration for email, SMS, DingTalk, Slack or any other outbound messaging channel. A future revision may add provider-backed or pluggable delivery channels, but they will mirror Owner Notifications rather than become the CloudOps source of truth. With the browser completely closed, this version makes no delivery guarantee.

## Consequences

- Native notification history and read state survive navigation and normal local shutdown.
- Notification badges and browser permission state cannot replace the durable Inbox record.
- Every actionable notification needs an exact Context Link and must preserve the same Operational Scope as its source.
- The UI must communicate that browser notifications are optional and cannot claim background delivery after the browser closes.
- The first implementation contains no inactive outbound-channel controls or placeholders that imply unsupported delivery.

## Rejected Alternatives

- Treat transient toast messages or browser notification history as the notification source of truth.
- Notify directly for every raw telemetry or Kubernetes state update.
- Require email, DingTalk, Slack, SMS or a hosted push service for the local first version.
- Claim guaranteed background delivery when the browser is closed.
