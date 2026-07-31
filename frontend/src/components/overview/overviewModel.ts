import type { AgentRun } from "../../api/agent";
import type { AlertView } from "../../api/alerts";
import type { DeliveryProjection } from "../../api/devops";
import type { IncidentView } from "../../types/incidents";

const activeIncidentStatuses = new Set<IncidentView["status"]>([
  "detected",
  "investigating",
  "awaiting_approval",
  "delivering",
  "verifying",
]);

function timestamp(value?: string): number {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
}

export function activeIncidents(items: readonly IncidentView[]): IncidentView[] {
  return items
    .filter((item) => activeIncidentStatuses.has(item.status))
    .sort((left, right) => timestamp(right.updated_at ?? right.last_seen_at) - timestamp(left.updated_at ?? left.last_seen_at));
}

export function recentlyResolvedIncidents(items: readonly IncidentView[]): IncidentView[] {
  return items
    .filter((item) => item.status === "resolved" || item.status === "closed")
    .sort((left, right) => timestamp(right.resolved_at ?? right.updated_at) - timestamp(left.resolved_at ?? left.updated_at));
}

export function unlinkedFiringAlerts(items: readonly AlertView[]): AlertView[] {
  return items
    .filter((item) => item.status === "firing" && item.incident_links.length === 0)
    .sort((left, right) => timestamp(right.last_seen_at) - timestamp(left.last_seen_at));
}

export function latestAgentRun(items: readonly AgentRun[]): AgentRun | null {
  return [...items].sort((left, right) => timestamp(right.updated_at) - timestamp(left.updated_at))[0] ?? null;
}

export function pendingAgentItems(run: AgentRun | null): number {
  if (!run) return 0;
  return run.action_cards.filter((item) => item.status === "proposed").length
    + run.operation_plans.filter((item) => item.status === "proposed").length;
}

export function recentDeliveries(items: readonly DeliveryProjection[]): DeliveryProjection[] {
  return [...items]
    .sort((left, right) => timestamp(right.last_observed_at) - timestamp(left.last_observed_at))
    .slice(0, 4);
}

export function latestVerificationStatusForDelivery(
  item: DeliveryProjection,
  incidents: readonly IncidentView[],
): string {
  return incidents.find((incident) => incident.id === item.incident_id)
    ?.recovery.latest_verification_status ?? "";
}

export function deliveryHasPassedVerification(
  item: DeliveryProjection,
  incidents: readonly IncidentView[],
): boolean {
  return latestVerificationStatusForDelivery(item, incidents) === "passed";
}
