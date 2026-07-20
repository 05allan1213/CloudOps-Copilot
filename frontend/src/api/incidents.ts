import type { AxiosRequestConfig } from "axios";

import { getJSON, postJSON } from "./client";
import type {
  CollectionResponse,
  CommandResponse,
  DecisionCommand,
  IncidentListQuery,
  IncidentResponse,
  IncidentView,
  ResourceResponse,
  ResourceView,
  VersionedCommand,
} from "../types/incidents";

const base = "/api/v3/incidents";

export function listIncidents(query: IncidentListQuery, signal?: AbortSignal): Promise<CollectionResponse<IncidentView>> {
  return getJSON<CollectionResponse<IncidentView>>(base, { params: query, signal });
}

export async function getIncident(incidentID: string, signal?: AbortSignal): Promise<IncidentView> {
  const response = await getJSON<IncidentResponse>(`${base}/${encodeURIComponent(incidentID)}`, { signal });
  return response.incident;
}

export function listIncidentSignals(incidentID: string, cursor = "", signal?: AbortSignal) {
  return listResources(incidentID, "signals", cursor, false, signal);
}

export function listIncidentTimeline(incidentID: string, afterID = "", signal?: AbortSignal) {
  return listResources(incidentID, "timeline", afterID, true, signal);
}

export function listIncidentEvidence(incidentID: string, cursor = "", signal?: AbortSignal) {
  return listResources(incidentID, "evidence", cursor, false, signal);
}

export function listIncidentInvestigations(incidentID: string, cursor = "", signal?: AbortSignal) {
  return listResources(incidentID, "investigations", cursor, false, signal);
}

export function listIncidentRemediationPlans(incidentID: string, cursor = "", signal?: AbortSignal) {
  return listResources(incidentID, "remediation-plans", cursor, false, signal);
}

export function listIncidentVerifications(incidentID: string, cursor = "", signal?: AbortSignal) {
  return listResources(incidentID, "verifications", cursor, false, signal);
}

export async function getIncidentDelivery(incidentID: string, signal?: AbortSignal): Promise<ResourceView> {
  return getResource(incidentID, "delivery", signal);
}

export async function getIncidentResolutionReport(incidentID: string, signal?: AbortSignal): Promise<ResourceView> {
  return getResource(incidentID, "resolution-report", signal);
}

export function startInvestigation(incidentID: string, body: VersionedCommand, csrfToken: string): Promise<CommandResponse> {
  return postCommand(`${base}/${encodeURIComponent(incidentID)}/investigations`, body, csrfToken, "investigate", incidentID);
}

export function closeIncident(incidentID: string, body: VersionedCommand, csrfToken: string): Promise<CommandResponse> {
  return postCommand(`${base}/${encodeURIComponent(incidentID)}/close`, body, csrfToken, "close", incidentID);
}

export function decideRemediation(planID: string, body: DecisionCommand, csrfToken: string): Promise<CommandResponse> {
  return postCommand(`/api/v3/remediation-plans/${encodeURIComponent(planID)}/decisions`, body, csrfToken, body.decision, planID);
}

export function incidentRealtimeURL(incidentID: string): string {
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "";
  return `${apiBaseUrl}${base}/${encodeURIComponent(incidentID)}/events`;
}

async function listResources(
  incidentID: string,
  resource: string,
  cursor: string,
  timeline: boolean,
  signal?: AbortSignal,
): Promise<CollectionResponse<ResourceView>> {
  const params: Record<string, string | number> = { limit: 100 };
  if (cursor) params[timeline ? "after_id" : "cursor"] = cursor;
  return getJSON<CollectionResponse<ResourceView>>(
    `${base}/${encodeURIComponent(incidentID)}/${resource}`,
    { params, signal },
  );
}

async function getResource(incidentID: string, resource: string, signal?: AbortSignal): Promise<ResourceView> {
  const response = await getJSON<ResourceResponse>(`${base}/${encodeURIComponent(incidentID)}/${resource}`, { signal });
  return response.resource;
}

function postCommand<TBody>(
  url: string,
  body: TBody,
  csrfToken: string,
  action: string,
  resourceID: string,
): Promise<CommandResponse> {
  const config: AxiosRequestConfig = {
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": csrfToken,
      "Idempotency-Key": newCommandKey(action, resourceID),
    },
  };
  return postJSON<CommandResponse, TBody>(url, body, config);
}

export function newCommandKey(action: string, resourceID: string): string {
  const nonce = typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `workbench-${action}-${resourceID}-${nonce}`.slice(0, 128);
}
