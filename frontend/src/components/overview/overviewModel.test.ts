import { describe, expect, it } from "vitest";

import type { AgentRun } from "../../api/agent";
import type { AlertView } from "../../api/alerts";
import type { DeliveryProjection } from "../../api/devops";
import type { IncidentView } from "../../types/incidents";
import {
  activeIncidents,
  deliveryHasPassedVerification,
  latestAgentRun,
  latestVerificationStatusForDelivery,
  pendingAgentItems,
  recentDeliveries,
  recentlyResolvedIncidents,
  unlinkedFiringAlerts,
} from "./overviewModel";

function incident(id: string, status: IncidentView["status"], updatedAt: string): IncidentView {
  return { id, status, updated_at: updatedAt } as IncidentView;
}

function agentRun(id: string, updatedAt: string): AgentRun {
  return { id, updated_at: updatedAt, action_cards: [], operation_plans: [] } as unknown as AgentRun;
}

function delivery(id: string, observedAt: string): DeliveryProjection {
  return {
    id,
    status: "pending",
    last_observed_at: observedAt,
    argo_sync_status: "Unknown",
    argo_health_status: "Unknown",
    available_replicas: 0,
    desired_replicas: 1,
  } as unknown as DeliveryProjection;
}

describe("Overview Command Center model", () => {
  it("orders active and resolved Incidents by their authoritative timestamps", () => {
    const items = [
      incident("resolved-old", "resolved", "2026-07-30T08:00:00Z"),
      incident("active-new", "investigating", "2026-07-31T08:00:00Z"),
      incident("active-old", "detected", "2026-07-31T07:00:00Z"),
      incident("closed-new", "closed", "2026-07-30T09:00:00Z"),
    ];

    expect(activeIncidents(items).map((item) => item.id)).toEqual(["active-new", "active-old"]);
    expect(recentlyResolvedIncidents(items).map((item) => item.id)).toEqual(["closed-new", "resolved-old"]);
  });

  it("keeps only firing Alerts that have no Incident link", () => {
    const items = [
      { id: "unlinked-old", status: "firing", incident_links: [], last_seen_at: "2026-07-31T07:00:00Z" },
      { id: "resolved", status: "resolved", incident_links: [], last_seen_at: "2026-07-31T09:00:00Z" },
      { id: "linked", status: "firing", incident_links: [{ id: "link" }], last_seen_at: "2026-07-31T10:00:00Z" },
      { id: "unlinked-new", status: "firing", incident_links: [], last_seen_at: "2026-07-31T08:00:00Z" },
    ] as unknown as AlertView[];

    expect(unlinkedFiringAlerts(items).map((item) => item.id)).toEqual(["unlinked-new", "unlinked-old"]);
  });

  it("selects the latest Agent run and counts pending proposals", () => {
    const older = agentRun("older", "2026-07-31T07:00:00Z");
    const latest = agentRun("latest", "2026-07-31T08:00:00Z");
    latest.action_cards = [{ status: "proposed" }, { status: "accepted" }] as AgentRun["action_cards"];
    latest.operation_plans = [{ status: "proposed" }] as AgentRun["operation_plans"];

    expect(latestAgentRun([older, latest])?.id).toBe("latest");
    expect(pendingAgentItems(latest)).toBe(2);
  });

  it("limits recent Delivery projections", () => {
    const items = Array.from({ length: 6 }, (_, index) => (
      delivery(`delivery-${index}`, `2026-07-31T0${index}:00:00Z`)
    ));
    expect(recentDeliveries(items).map((item) => item.id)).toEqual([
      "delivery-5", "delivery-4", "delivery-3", "delivery-2",
    ]);
  });

  it("uses persisted Incident Verification truth instead of inferring from rollout health", () => {
    const healthyDelivery = {
      ...delivery("healthy", "2026-07-31T08:00:00Z"),
      incident_id: "incident-verified",
      status: "delivered",
      argo_sync_status: "Synced",
      argo_health_status: "Healthy",
      desired_replicas: 3,
      available_replicas: 3,
    };
    const incidentWithVerification = (status?: string) => ({
      id: "incident-verified",
      recovery: { latest_verification_status: status },
    } as IncidentView);

    expect(deliveryHasPassedVerification(healthyDelivery, [])).toBe(false);
    expect(deliveryHasPassedVerification(healthyDelivery, [incidentWithVerification("running")])).toBe(false);
    expect(deliveryHasPassedVerification(healthyDelivery, [incidentWithVerification("passed")])).toBe(true);
    expect(latestVerificationStatusForDelivery(
      healthyDelivery,
      [incidentWithVerification("failed")],
    )).toBe("failed");
  });
});
