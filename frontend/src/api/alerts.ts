import type { AxiosRequestConfig } from "axios";

import type { ContextLink } from "./notifications";
import { getJSON, postJSON } from "./client";

export type AlertStatus = "firing" | "resolved";
export type AlertSeverity = "unknown" | "info" | "warning" | "critical";

export interface AlertActor {
  provider: string;
  login: string;
  role: string;
}

export interface AlertAcknowledgement {
  id: string;
  recurrence_no: number;
  alert_version: number;
  actor: AlertActor;
  reason: string;
  created_at: string;
}

export interface AlertMatcher {
  name: string;
  value: string;
  is_regex: boolean;
  is_equal: boolean;
}

export interface AlertSilence {
  id: string;
  provider_silence_id?: string;
  status: "pending" | "active" | "expired" | "failed";
  matchers: AlertMatcher[];
  reason: string;
  configuration_revision_id: string;
  starts_at: string;
  ends_at: string;
  expired_at?: string;
  provider_error_code?: string;
  created_at: string;
}

export interface AlertIncidentLink {
  id: string;
  incident_id: string;
  incident_cycle: number;
  incident_status: string;
  provenance: "owner_created" | "owner_attached" | "escalation_policy" | "legacy_automatic_ingress";
  configuration_revision_id?: string;
  escalation_policy_id?: string;
  created_at: string;
}

export interface AlertInvestigationLink {
  id: string;
  incident_id: string;
  status: string;
  created_at: string;
}

export interface AlertView {
  id: string;
  status: AlertStatus;
  severity: AlertSeverity;
  summary: string;
  category: string;
  source: string;
  fingerprint: string;
  correlation_key: string;
  cluster: string;
  environment: string;
  namespace: string;
  service_name: string;
  target_kind: string;
  target_name: string;
  first_seen_at: string;
  last_seen_at: string;
  starts_at: string;
  resolved_at?: string;
  recurrence_count: number;
  signal_count: number;
  version: number;
  acknowledgement?: AlertAcknowledgement;
  silence?: AlertSilence;
  incident_links: AlertIncidentLink[];
  investigations: AlertInvestigationLink[];
  context_link: ContextLink;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
  created_at: string;
  updated_at: string;
}

export interface AlertSignal {
  id: string;
  source_event_id: string;
  alert_instance_key: string;
  status: AlertStatus;
  severity: AlertSeverity;
  summary: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  starts_at: string;
  ends_at?: string;
  occurred_at: string;
  received_at: string;
  provenance: "signal_normalization" | "legacy_automatic_ingress";
}

export interface AlertEvent {
  id: string;
  type: string;
  actor_type: string;
  actor_id: string;
  summary: string;
  metadata: Record<string, unknown>;
  occurred_at: string;
}

export interface AlertDetail {
  alert: AlertView;
  signals: AlertSignal[];
  events: AlertEvent[];
}

export interface AlertPage {
  items: AlertView[];
  next_cursor?: string;
}

export interface AlertListQuery {
  cursor?: string;
  limit?: number;
  status?: AlertStatus | "";
  severity?: AlertSeverity | "";
  namespace?: string;
  search?: string;
}

const alertBase = "/api/v1/alerts";

export function listAlerts(query: AlertListQuery, signal?: AbortSignal): Promise<AlertPage> {
  return getJSON(alertBase, { params: query, signal });
}

export function getAlert(id: string, signal?: AbortSignal): Promise<AlertDetail> {
  return getJSON(`${alertBase}/${encodeURIComponent(id)}`, { signal });
}

export function acknowledgeAlert(id: string, expectedVersion: number, reason: string): Promise<AlertView> {
  return postAlertCommand(`${alertBase}/${encodeURIComponent(id)}/acknowledgements`, { expected_version: expectedVersion, reason }, "acknowledge", id);
}

export function createAlertSilence(id: string, expectedVersion: number, durationSeconds: number, reason: string): Promise<AlertSilence> {
  return postAlertCommand(`${alertBase}/${encodeURIComponent(id)}/silences`, {
    expected_version: expectedVersion,
    duration_seconds: durationSeconds,
    reason,
  }, "silence", id);
}

export function expireAlertSilence(silenceID: string, expectedVersion: number): Promise<AlertSilence> {
  return postAlertCommand(`/api/v1/silences/${encodeURIComponent(silenceID)}/expire`, {
    expected_version: expectedVersion,
  }, "expire-silence", silenceID);
}

export function createIncidentFromAlert(id: string, expectedVersion: number): Promise<AlertView> {
  return postAlertCommand(`${alertBase}/${encodeURIComponent(id)}/incident-links`, {
    expected_version: expectedVersion,
    incident_id: "",
    create: true,
  }, "create-incident", id);
}

export function attachAlertToIncident(id: string, incidentID: string, expectedVersion: number): Promise<AlertView> {
  return postAlertCommand(`${alertBase}/${encodeURIComponent(id)}/incident-links`, {
    expected_version: expectedVersion,
    incident_id: incidentID,
    create: false,
  }, "attach-incident", id);
}

export function startAlertInvestigation(id: string, expectedVersion: number, reason: string): Promise<AlertView> {
  return postAlertCommand(`${alertBase}/${encodeURIComponent(id)}/investigations`, {
    expected_version: expectedVersion,
    reason,
  }, "investigation", id);
}

function postAlertCommand<T>(url: string, body: unknown, operation: string, resourceID: string): Promise<T> {
  const config: AxiosRequestConfig = {
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": alertCommandKey(operation, resourceID),
    },
  };
  return postJSON<T>(url, body, config);
}

export function alertCommandKey(operation: string, resourceID: string): string {
  const nonce = typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `alerts-${operation}-${resourceID}-${nonce}`.slice(0, 128);
}
