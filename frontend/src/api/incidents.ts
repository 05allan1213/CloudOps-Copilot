import type { AxiosRequestConfig } from "axios";

import { ApiError, getJSON, postJSONWithMeta } from "./client";
import type {
  CollectionResponse,
  CommandOutcome,
  CommandResponse,
  DeliveryView,
  DecisionCommand,
  IncidentAlertRelationView,
  IncidentDecisionView,
  IncidentEvidenceView,
  IncidentInvestigationView,
  IncidentListQuery,
  IncidentResponse,
  IncidentTimelineEventView,
  IncidentView,
  RemediationPlanView,
  RecoveryDecisionCommand,
  ResolutionReportView,
  ResourceResponse,
  ResourceView,
  VerificationRunView,
  VersionedCommand,
} from "../types/incidents";

const base = "/api/v1/incidents";

export interface CommandRequestOptions {
  idempotencyKey?: string;
}

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

export function listIncidentAlerts(
  incidentID: string,
  cursor = "",
  signal?: AbortSignal,
): Promise<CollectionResponse<IncidentAlertRelationView>> {
  return listTypedResources<IncidentAlertRelationView>(incidentID, "alerts", cursor, signal);
}

export function listIncidentTimeline(
  incidentID: string,
  afterID = "",
  signal?: AbortSignal,
): Promise<CollectionResponse<IncidentTimelineEventView>> {
  return listTypedResources<IncidentTimelineEventView>(incidentID, "timeline", afterID, signal, true);
}

export function listIncidentEvidence(
  incidentID: string,
  cursor = "",
  signal?: AbortSignal,
): Promise<CollectionResponse<IncidentEvidenceView>> {
  return listTypedResources<IncidentEvidenceView>(incidentID, "evidence", cursor, signal);
}

export function listIncidentInvestigations(
  incidentID: string,
  cursor = "",
  signal?: AbortSignal,
): Promise<CollectionResponse<IncidentInvestigationView>> {
  return listTypedResources<IncidentInvestigationView>(incidentID, "investigations", cursor, signal);
}

export async function getIncidentDecision(incidentID: string, signal?: AbortSignal): Promise<IncidentDecisionView | null> {
  const response = await getJSON<{ decision: IncidentDecisionView | null }>(
    `${base}/${encodeURIComponent(incidentID)}/decision`,
    { signal },
  );
  return response.decision;
}

export function listIncidentRemediationPlans(
  incidentID: string,
  cursor = "",
  signal?: AbortSignal,
): Promise<CollectionResponse<RemediationPlanView>> {
  return listTypedResources<RemediationPlanView>(incidentID, "remediation-plans", cursor, signal);
}

export function listIncidentVerifications(
  incidentID: string,
  cursor = "",
  signal?: AbortSignal,
): Promise<CollectionResponse<VerificationRunView>> {
  return listTypedResources<VerificationRunView>(incidentID, "verifications", cursor, signal);
}

export async function getIncidentDelivery(incidentID: string, signal?: AbortSignal): Promise<DeliveryView | null> {
  return getTypedResource<DeliveryView>(incidentID, "delivery", signal);
}

export async function getIncidentResolutionReport(incidentID: string, signal?: AbortSignal): Promise<ResolutionReportView | null> {
  return getTypedResource<ResolutionReportView>(incidentID, "resolution-report", signal);
}

export function startInvestigation(
  incidentID: string,
  body: VersionedCommand,
  options?: CommandRequestOptions,
): Promise<CommandOutcome> {
  return postCommand(`${base}/${encodeURIComponent(incidentID)}/investigations`, body, "investigate", incidentID, options);
}

export function closeIncident(
  incidentID: string,
  body: VersionedCommand,
  options?: CommandRequestOptions,
): Promise<CommandOutcome> {
  return postCommand(`${base}/${encodeURIComponent(incidentID)}/close`, body, "close", incidentID, options);
}

export function decideIncidentRecovery(
  incidentID: string,
  body: RecoveryDecisionCommand,
  options?: CommandRequestOptions,
): Promise<CommandOutcome> {
  return postCommand(
    `${base}/${encodeURIComponent(incidentID)}/decision`,
    body,
    body.decision,
    incidentID,
    options,
  );
}

export function decideRemediation(
  planID: string,
  body: DecisionCommand,
  options?: CommandRequestOptions,
): Promise<CommandOutcome> {
  return postCommand(`/api/v1/remediation-plans/${encodeURIComponent(planID)}/decisions`, body, body.decision, planID, options);
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

function listTypedResources<T>(
  incidentID: string,
  resource: string,
  cursor: string,
  signal?: AbortSignal,
  timeline = false,
): Promise<CollectionResponse<T>> {
  const params: Record<string, string | number> = { limit: 100 };
  if (cursor) params[timeline ? "after_id" : "cursor"] = cursor;
  return getJSON<CollectionResponse<T>>(
    `${base}/${encodeURIComponent(incidentID)}/${resource}`,
    { params, signal },
  );
}

async function getTypedResource<T>(incidentID: string, resource: string, signal?: AbortSignal): Promise<T | null> {
  const response = await getJSON<ResourceResponse<T | null>>(`${base}/${encodeURIComponent(incidentID)}/${resource}`, { signal });
  return response.resource;
}

function postCommand<TBody>(
  url: string,
  body: TBody,
  action: string,
  resourceID: string,
  options: CommandRequestOptions = {},
): Promise<CommandOutcome> {
  const idempotencyKey = options.idempotencyKey || newCommandKey(action, resourceID);
  const config: AxiosRequestConfig = { headers: commandHeaders(idempotencyKey) };
  return postJSONWithMeta<CommandResponse, TBody>(url, body, config).then((response) => {
    if (response.status !== 202) {
      throw new ApiError(
        `Command returned unexpected HTTP ${response.status}; expected 202 Accepted`,
        response.status,
        "UNEXPECTED_COMMAND_STATUS",
        response.requestID,
        response.traceID,
        response.idempotentReplay,
      );
    }
    return {
      result: response.data,
      httpStatus: response.status,
      requestID: response.requestID,
      traceID: response.traceID,
      idempotentReplay: response.idempotentReplay,
      idempotencyKey,
    };
  });
}

export function commandHeaders(idempotencyKey: string): Record<string, string> {
  return {
    "Content-Type": "application/json",
    "Idempotency-Key": idempotencyKey,
  };
}

export function newCommandKey(action: string, resourceID: string): string {
  const nonce = typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `workbench-${action}-${resourceID}-${nonce}`.slice(0, 128);
}
