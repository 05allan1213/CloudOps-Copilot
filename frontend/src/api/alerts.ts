import type { AxiosRequestConfig } from "axios";

import type { ContextLink } from "./notifications";
import { ApiError, getJSON, postJSONWithMeta } from "./client";

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
  incident?: string;
}

export type AlertListLimit = 25 | 50 | 100;

export interface AlertListRouteState {
  status: AlertStatus | "";
  severity: AlertSeverity | "";
  namespace: string;
  search: string;
  incident: string;
  cursor: string;
  limit: AlertListLimit;
  selected: string;
}

export type AlertRouteQueryValue = string | null | (string | null)[] | undefined;
export type AlertRouteQuery = Record<string, AlertRouteQueryValue>;

export type AlertCommandOperation =
  | "acknowledge"
  | "silence"
  | "expire-silence"
  | "create-incident"
  | "attach-incident"
  | "investigation";

export interface AlertCommandOptions {
  idempotencyKey?: string;
}

export interface AlertCommandResult<T> {
  data: T;
  httpStatus: number;
  requestID: string;
  traceID: string;
  idempotentReplay: boolean;
  idempotencyKey: string;
  expectedVersion: number;
  operation: AlertCommandOperation;
}

export interface AlertProbeReconciliation {
  items: AlertView[];
  pendingItems: AlertView[];
}

export interface AlertContextRouteLink {
  label: string;
  path: "/infrastructure" | "/monitoring" | "/logs" | "/traces";
  query: Record<string, string>;
}

const alertBase = "/api/v1/alerts";

export function listAlerts(query: AlertListQuery, signal?: AbortSignal): Promise<AlertPage> {
  return getJSON(alertBase, { params: query, signal });
}

export function getAlert(id: string, signal?: AbortSignal): Promise<AlertDetail> {
  return getJSON(`${alertBase}/${encodeURIComponent(id)}`, { signal });
}

export function acknowledgeAlert(
  id: string,
  expectedVersion: number,
  reason: string,
  options: AlertCommandOptions = {},
): Promise<AlertCommandResult<AlertView>> {
  return postAlertCommand(
    `${alertBase}/${encodeURIComponent(id)}/acknowledgements`,
    { expected_version: expectedVersion, reason },
    "acknowledge",
    id,
    expectedVersion,
    [200],
    options,
  );
}

export function createAlertSilence(
  id: string,
  expectedVersion: number,
  durationSeconds: number,
  reason: string,
  options: AlertCommandOptions = {},
): Promise<AlertCommandResult<AlertSilence>> {
  return postAlertCommand(
    `${alertBase}/${encodeURIComponent(id)}/silences`,
    {
      expected_version: expectedVersion,
      duration_seconds: durationSeconds,
      reason,
    },
    "silence",
    id,
    expectedVersion,
    [201],
    options,
  );
}

export function expireAlertSilence(
  silenceID: string,
  expectedVersion: number,
  options: AlertCommandOptions = {},
): Promise<AlertCommandResult<AlertSilence>> {
  return postAlertCommand(
    `/api/v1/silences/${encodeURIComponent(silenceID)}/expire`,
    { expected_version: expectedVersion },
    "expire-silence",
    silenceID,
    expectedVersion,
    [200],
    options,
  );
}

export function createIncidentFromAlert(
  id: string,
  expectedVersion: number,
  options: AlertCommandOptions = {},
): Promise<AlertCommandResult<AlertView>> {
  return postAlertCommand(
    `${alertBase}/${encodeURIComponent(id)}/incident-links`,
    {
      expected_version: expectedVersion,
      incident_id: "",
      create: true,
    },
    "create-incident",
    id,
    expectedVersion,
    [201],
    options,
  );
}

export function attachAlertToIncident(
  id: string,
  incidentID: string,
  expectedVersion: number,
  options: AlertCommandOptions = {},
): Promise<AlertCommandResult<AlertView>> {
  return postAlertCommand(
    `${alertBase}/${encodeURIComponent(id)}/incident-links`,
    {
      expected_version: expectedVersion,
      incident_id: incidentID,
      create: false,
    },
    "attach-incident",
    id,
    expectedVersion,
    [201],
    options,
  );
}

export function startAlertInvestigation(
  id: string,
  expectedVersion: number,
  reason: string,
  options: AlertCommandOptions = {},
): Promise<AlertCommandResult<AlertView>> {
  return postAlertCommand(
    `${alertBase}/${encodeURIComponent(id)}/investigations`,
    { expected_version: expectedVersion, reason },
    "investigation",
    id,
    expectedVersion,
    [202],
    options,
  );
}

function postAlertCommand<T>(
  url: string,
  body: unknown,
  operation: AlertCommandOperation,
  resourceID: string,
  expectedVersion: number,
  expectedStatuses: readonly number[],
  options: AlertCommandOptions,
): Promise<AlertCommandResult<T>> {
  const idempotencyKey = options.idempotencyKey || alertCommandKey(operation, resourceID);
  const config: AxiosRequestConfig = {
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": idempotencyKey,
    },
  };
  return postJSONWithMeta<T>(url, body, config).then((response) => {
    if (!expectedStatuses.includes(response.status)) {
      throw new ApiError(
        `Alert command returned unexpected HTTP ${response.status}; expected ${expectedStatuses.join(" or ")}`,
        response.status,
        "UNEXPECTED_COMMAND_STATUS",
        response.requestID,
        response.traceID,
        response.idempotentReplay,
      );
    }
    return {
      data: response.data,
      httpStatus: response.status,
      requestID: response.requestID,
      traceID: response.traceID,
      idempotentReplay: response.idempotentReplay,
      idempotencyKey,
      expectedVersion,
      operation,
    };
  });
}

export function alertCommandKey(operation: string, resourceID: string): string {
  const nonce = typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `alerts-${operation}-${resourceID}-${nonce}`.slice(0, 128);
}

export function parseAlertListRouteQuery(query: AlertRouteQuery): AlertListRouteState {
  const status = routeQueryText(query.status);
  const severity = routeQueryText(query.severity);
  const parsedLimit = Number(routeQueryText(query.limit));
  return {
    status: status === "firing" || status === "resolved" ? status : "",
    severity: ["unknown", "info", "warning", "critical"].includes(severity)
      ? severity as AlertSeverity
      : "",
    namespace: boundedRouteQueryText(query.namespace, 255),
    search: boundedRouteQueryText(query.search, 255),
    incident: boundedRouteQueryText(query.incident, 128),
    cursor: boundedRouteQueryText(query.cursor, 512),
    limit: parsedLimit === 25 || parsedLimit === 100 ? parsedLimit : 50,
    selected: boundedRouteQueryText(query.selected, 128),
  };
}

export function alertListRouteQuery(
  state: AlertListRouteState,
  current: AlertRouteQuery = {},
): AlertRouteQuery {
  const next = canonicalAlertResourceQuery(current);
  for (const key of ["status", "severity", "namespace", "search", "incident", "cursor", "limit", "selected"]) {
    delete next[key];
  }
  if (state.status) next.status = state.status;
  if (state.severity) next.severity = state.severity;
  if (state.namespace.trim()) next.namespace = state.namespace.trim();
  if (state.search.trim()) next.search = state.search.trim();
  if (state.incident.trim()) next.incident = state.incident.trim();
  if (state.cursor) next.cursor = state.cursor;
  if (state.limit !== 50) next.limit = String(state.limit);
  if (state.selected) next.selected = state.selected;
  return next;
}

export function canonicalAlertResourceQuery(query: AlertRouteQuery): AlertRouteQuery {
  const next = { ...query };
  const resource = boundedRouteQueryText(query.resource, 512)
    || boundedRouteQueryText(query.workload, 512);
  delete next.workload;
  if (resource) next.resource = resource;
  else delete next.resource;
  return next;
}

export function reconcileAlertProbe(
  currentItems: readonly AlertView[],
  probedItems: readonly AlertView[],
): AlertProbeReconciliation {
  const currentIDs = new Set(currentItems.map((item) => item.id));
  const probedByID = new Map(probedItems.map((item) => [item.id, item]));
  return {
    items: currentItems.map((item) => {
      const candidate = probedByID.get(item.id);
      return candidate && candidate.version >= item.version ? candidate : item;
    }),
    pendingItems: probedItems.filter((item) => !currentIDs.has(item.id)),
  };
}

export function alertContextRouteLinks(
  alert: AlertView,
  rangeEnd = new Date().toISOString(),
): AlertContextRouteLink[] {
  const shared = {
    cluster: alert.cluster,
    namespace: alert.namespace,
    from: alert.starts_at,
    to: alert.resolved_at || rangeEnd,
  };
  return [
    {
      label: "基础设施",
      path: "/infrastructure",
      query: {
        ...shared,
        resource: `${alert.target_kind}/${alert.namespace}/${alert.target_name}`,
      },
    },
    {
      label: "监控",
      path: "/monitoring",
      query: { ...shared, resource: alert.target_name },
    },
    {
      label: "日志",
      path: "/logs",
      query: { ...shared, resource: alert.target_name },
    },
    {
      label: "链路",
      path: "/traces",
      query: { ...shared, resource: alert.target_name },
    },
  ];
}

export function alertInspectorHistory(
  events: readonly AlertEvent[],
  limit = 8,
): AlertEvent[] {
  return events.slice(0, Math.max(0, limit));
}

export function isAlertPublicID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

function routeQueryText(value: AlertRouteQueryValue): string {
  const candidate = Array.isArray(value) ? value.find((item): item is string => typeof item === "string") : value;
  return typeof candidate === "string" ? candidate : "";
}

function boundedRouteQueryText(value: AlertRouteQueryValue, maxLength: number): string {
  const candidate = routeQueryText(value);
  return candidate.length <= maxLength && !/[\0\r\n]/.test(candidate) ? candidate : "";
}
