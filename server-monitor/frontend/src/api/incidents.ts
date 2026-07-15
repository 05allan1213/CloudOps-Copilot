import { getApiData } from "./client";
import type {
  BoundedPage,
  DeliveryDTO,
  IncidentDTO,
  IncidentEvidenceDTO,
  IncidentListQuery,
  IncidentSignalDTO,
  IncidentTimelineDTO,
  InvestigationDTO,
  PostmortemDTO,
  RemediationDTO,
  VerificationDetailDTO,
  VerificationRunDTO,
} from "../types/incidents";

const base = "/api/v2/workbench/incidents";

export function listIncidents(query: IncidentListQuery, signal?: AbortSignal) {
  return getApiData<BoundedPage<IncidentDTO>>(base, { params: query, signal });
}

export function getIncident(incidentID: string, signal?: AbortSignal) {
  return getApiData<IncidentDTO>(`${base}/${encodeURIComponent(incidentID)}`, { signal });
}

export function listIncidentSignals(incidentID: string, page = 1, pageSize = 20, signal?: AbortSignal) {
  return getApiData<BoundedPage<IncidentSignalDTO>>(`${base}/${encodeURIComponent(incidentID)}/signals`, {
    params: { page, page_size: pageSize },
    signal,
  });
}

export function listIncidentTimeline(incidentID: string, page = 1, pageSize = 20, signal?: AbortSignal) {
  return getApiData<BoundedPage<IncidentTimelineDTO>>(`${base}/${encodeURIComponent(incidentID)}/timeline`, {
    params: { page, page_size: pageSize },
    signal,
  });
}

export function listIncidentEvidence(incidentID: string, page = 1, pageSize = 20, signal?: AbortSignal) {
  return getApiData<BoundedPage<IncidentEvidenceDTO>>(`${base}/${encodeURIComponent(incidentID)}/evidence`, {
    params: { page, page_size: pageSize },
    signal,
  });
}

export function getIncidentInvestigation(incidentID: string, signal?: AbortSignal) {
  return getApiData<InvestigationDTO>(`${base}/${encodeURIComponent(incidentID)}/investigation`, { signal });
}

export function getIncidentRemediation(incidentID: string, signal?: AbortSignal) {
  return getApiData<RemediationDTO>(`${base}/${encodeURIComponent(incidentID)}/remediation`, { signal });
}

export function getIncidentDelivery(incidentID: string, signal?: AbortSignal) {
  return getApiData<DeliveryDTO>(`${base}/${encodeURIComponent(incidentID)}/delivery`, { signal });
}

export function listIncidentVerifications(incidentID: string, page = 1, pageSize = 20, signal?: AbortSignal) {
  return getApiData<BoundedPage<VerificationRunDTO>>(`${base}/${encodeURIComponent(incidentID)}/verifications`, {
    params: { page, page_size: pageSize },
    signal,
  });
}

export function getIncidentVerification(incidentID: string, verificationID: string, signal?: AbortSignal) {
  return getApiData<VerificationDetailDTO>(
    `${base}/${encodeURIComponent(incidentID)}/verifications/${encodeURIComponent(verificationID)}`,
    { signal },
  );
}

export function getIncidentPostmortem(incidentID: string, signal?: AbortSignal) {
  return getApiData<PostmortemDTO>(`/api/v2/incidents/${encodeURIComponent(incidentID)}/postmortem`, { signal });
}

export function incidentRealtimeURL(incidentID: string): string {
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "";
  return `${apiBaseUrl}${base}/${encodeURIComponent(incidentID)}/realtime`;
}
