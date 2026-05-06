import { getApiData } from "./client";
import type { AlertEvent, AlertRecord } from "../types";

export interface AlertEventsQuery {
  limit?: number;
  status?: "firing" | "resolved" | "all";
  severity?: "critical" | "warning" | "info" | "all";
}

export interface ActiveAlertsQuery {
  severity?: "critical" | "warning" | "info" | "all";
}

export async function fetchActiveAlerts(
  queryInput: ActiveAlertsQuery = {},
): Promise<AlertRecord[]> {
  const params: Record<string, string> = {};

  if (queryInput.severity && queryInput.severity !== "all") {
    params.severity = queryInput.severity;
  }

  return (await getApiData<AlertRecord[]>("/api/v1/alerts/active", {
    params,
  })) ?? [];
}

export async function fetchAlertEvents(
  queryInput: AlertEventsQuery = {},
): Promise<AlertEvent[]> {
  const params: Record<string, string | number> = {
    limit: queryInput.limit ?? 8,
  };

  if (queryInput.status && queryInput.status !== "all") {
    params.status = queryInput.status;
  }
  if (queryInput.severity && queryInput.severity !== "all") {
    params.severity = queryInput.severity;
  }

  return (await getApiData<AlertEvent[]>("/api/v1/alerts/events", {
    params,
  })) ?? [];
}
