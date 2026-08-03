import { getJSON, postJSON } from "./client";
import type { OperationalScope } from "./platform";

export type TelemetryProviderState = "available" | "partial" | "unavailable" | "disabled";
export type TelemetryQueryMode = "guided" | "expert";

export interface TelemetryResourceReference {
  id: string;
  kind: string;
  namespace: string;
  name: string;
}

export interface TelemetryTimeRange {
  from: string;
  to: string;
}

export interface TelemetrySource {
  provider: "elasticsearch" | "tempo";
  identity: string;
  server_version?: string;
  collected_at: string;
}

export interface TelemetryContextLink {
  kind: "internal" | "provider" | "source" | "operation";
  label: string;
  href: string;
  target: "current" | "external";
  provider?: string;
  resource_ref?: string;
  from?: string;
  to?: string;
  availability: "available" | "unavailable" | "misconfigured";
}

export interface TelemetryBounds {
  max_lookback_seconds: number;
  timeout_ms: number;
  max_response_bytes: number;
  max_results: number;
  concurrency_limit: number;
}

export interface TelemetryCatalog {
  provider: "elasticsearch" | "tempo";
  configuration_revision_id: string;
  scope: OperationalScope;
  resource: TelemetryResourceReference;
  provider_state: TelemetryProviderState;
  provider_detail: string;
  source: TelemetrySource;
  bounds: TelemetryBounds;
  collected_at: string;
}

export interface TelemetryContext {
  cluster_id: string;
  namespace: string;
  resource: TelemetryResourceReference;
}

export interface HistogramBucket {
  from: string;
  to: string;
  count: number;
}

export interface LogEntry {
  id: string;
  timestamp: string;
  level?: string;
  message: string;
  service?: string;
  trace_id?: string;
  span_id?: string;
  resource: TelemetryResourceReference;
  attributes: Record<string, string>;
  links: TelemetryContextLink[];
}

export interface LogQuery {
  id: string;
  configuration_revision_id: string;
  provider: "elasticsearch";
  mode: TelemetryQueryMode;
  query: string;
  query_hash: string;
  scope: OperationalScope;
  resource: TelemetryResourceReference;
  time_range: TelemetryTimeRange;
  bounds: TelemetryBounds;
  status: "pending" | "running" | "succeeded" | "failed" | "cancelled";
  source: TelemetrySource;
  histogram: HistogramBucket[];
  entries: LogEntry[];
  fields: string[];
  result_expired: boolean;
  result_count: number;
  response_bytes: number;
  partial: boolean;
  truncated: boolean;
  stale: boolean;
  tail: boolean;
  error_code?: string;
  error_detail?: string;
  links: TelemetryContextLink[];
  created_at: string;
  completed_at?: string;
}

export interface TraceSummary {
  trace_id: string;
  root_service: string;
  root_operation: string;
  start_time: string;
  duration_ms: number;
  span_count: number;
  error_span_count: number;
  resource: TelemetryResourceReference;
  links: TelemetryContextLink[];
}

export interface TraceSearch {
  id: string;
  configuration_revision_id: string;
  provider: "tempo";
  mode: TelemetryQueryMode;
  query: string;
  query_hash: string;
  scope: OperationalScope;
  resource: TelemetryResourceReference;
  time_range: TelemetryTimeRange;
  bounds: TelemetryBounds;
  status: "pending" | "running" | "succeeded" | "failed" | "cancelled";
  source: TelemetrySource;
  traces: TraceSummary[];
  result_expired: boolean;
  result_count: number;
  response_bytes: number;
  partial: boolean;
  truncated: boolean;
  stale: boolean;
  error_code?: string;
  error_detail?: string;
  links: TelemetryContextLink[];
  created_at: string;
  completed_at?: string;
}

export interface SpanEvent {
  name: string;
  timestamp: string;
  attributes: Record<string, string>;
}

export interface TraceSpan {
  span_id: string;
  parent_span_id?: string;
  service: string;
  name: string;
  kind?: string;
  start_time: string;
  duration_ms: number;
  depth: number;
  status: string;
  critical_path: boolean;
  attributes: Record<string, string>;
  events: SpanEvent[];
  resource: TelemetryResourceReference;
  links: TelemetryContextLink[];
}

export interface TraceDetail {
  query_id: string;
  trace_id: string;
  configuration_revision_id: string;
  scope: OperationalScope;
  resource: TelemetryResourceReference;
  time_range: TelemetryTimeRange;
  source: TelemetrySource;
  root_service: string;
  root_operation: string;
  start_time: string;
  duration_ms: number;
  spans: TraceSpan[];
  attributes: Record<string, string>;
  partial: boolean;
  truncated: boolean;
  response_bytes: number;
  links: TelemetryContextLink[];
}

export interface TelemetryEvidence {
  id: string;
  type: string;
  source: string;
  query_id: string;
  configuration_revision_id: string;
  scope: OperationalScope;
  resource: TelemetryResourceReference;
  time_range: TelemetryTimeRange;
  summary: string;
  item_count: number;
  content_hash: string;
  truncated: boolean;
  collected_at: string;
}

export interface ContextSnapshot {
  id: string;
  consultation_id: string;
  configuration_revision_id: string;
  scope: OperationalScope;
  resource_refs: TelemetryResourceReference[];
  time_range: TelemetryTimeRange;
  query_execution_refs: string[];
  evidence_refs: string[];
  content_hash: string;
  created_at: string;
}

export interface Consultation {
  id: string;
  title: string;
  status: string;
  context_snapshot: ContextSnapshot;
  created_at: string;
}

export interface StartLogQueryInput extends TelemetryContext {
  mode: TelemetryQueryMode;
  query?: string;
  filter: { text?: string; levels?: string[]; trace_id?: string };
  from: string;
  to: string;
  limit: number;
  tail: boolean;
}

export interface StartTraceSearchInput extends TelemetryContext {
  mode: TelemetryQueryMode;
  query?: string;
  filter: { service?: string; operation?: string; status?: string; min_duration_ms?: number; max_duration_ms?: number };
  from: string;
  to: string;
  limit: number;
}

interface HistoryFilter {
  cluster_id?: string;
  namespace?: string;
  resource_id?: string;
  limit?: number;
}

function contextQuery(context: TelemetryContext): string {
  return new URLSearchParams({
    cluster_id: context.cluster_id,
    namespace: context.namespace,
    resource_id: context.resource.id,
    resource_kind: context.resource.kind,
    resource_name: context.resource.name,
  }).toString();
}

function historyQuery(filter: HistoryFilter): string {
  const values = new URLSearchParams();
  if (filter.cluster_id) values.set("cluster_id", filter.cluster_id);
  if (filter.namespace) values.set("namespace", filter.namespace);
  if (filter.resource_id) values.set("resource_id", filter.resource_id);
  if (filter.limit) values.set("limit", String(filter.limit));
  const encoded = values.toString();
  return encoded ? `?${encoded}` : "";
}

export function getLogsCatalog(context: TelemetryContext, signal?: AbortSignal): Promise<TelemetryCatalog> {
  return getJSON(`/api/v1/logs/catalog?${contextQuery(context)}`, { signal });
}

export function startLogQuery(input: StartLogQueryInput, signal?: AbortSignal): Promise<LogQuery> {
  return postJSON("/api/v1/logs/queries", input, { signal });
}

export function getLogQuery(id: string, signal?: AbortSignal): Promise<LogQuery> {
  return getJSON(`/api/v1/logs/queries/${encodeURIComponent(id)}`, { signal });
}

export async function getLogQueries(filter: HistoryFilter, signal?: AbortSignal): Promise<LogQuery[]> {
  const page = await getJSON<{ items: LogQuery[] }>(`/api/v1/logs/queries${historyQuery(filter)}`, { signal });
  return page.items;
}

export async function getLogEvidence(queryID: string, signal?: AbortSignal): Promise<TelemetryEvidence[]> {
  const page = await getJSON<{ items: TelemetryEvidence[] }>(
    `/api/v1/logs/queries/${encodeURIComponent(queryID)}/evidence`,
    { signal },
  );
  return page.items;
}

export function saveLogEvidence(queryID: string, itemIDs: string[]): Promise<TelemetryEvidence> {
  return postJSON(`/api/v1/logs/queries/${encodeURIComponent(queryID)}/evidence`, { item_ids: itemIDs });
}

export function getTracesCatalog(context: TelemetryContext, signal?: AbortSignal): Promise<TelemetryCatalog> {
  return getJSON(`/api/v1/traces/catalog?${contextQuery(context)}`, { signal });
}

export function startTraceSearch(input: StartTraceSearchInput, signal?: AbortSignal): Promise<TraceSearch> {
  return postJSON("/api/v1/traces/searches", input, { signal });
}

export function getTraceSearch(id: string, signal?: AbortSignal): Promise<TraceSearch> {
  return getJSON(`/api/v1/traces/searches/${encodeURIComponent(id)}`, { signal });
}

export async function getTraceSearches(filter: HistoryFilter, signal?: AbortSignal): Promise<TraceSearch[]> {
  const page = await getJSON<{ items: TraceSearch[] }>(`/api/v1/traces/searches${historyQuery(filter)}`, { signal });
  return page.items;
}

export async function getTraceEvidence(queryID: string, signal?: AbortSignal): Promise<TelemetryEvidence[]> {
  const page = await getJSON<{ items: TelemetryEvidence[] }>(
    `/api/v1/traces/searches/${encodeURIComponent(queryID)}/evidence`,
    { signal },
  );
  return page.items;
}

export function getTraceDetail(
  traceID: string,
  options: { search_id?: string; context?: TelemetryContext; from?: string; to?: string },
  signal?: AbortSignal,
): Promise<TraceDetail> {
  const values = new URLSearchParams();
  if (options.search_id) values.set("search_id", options.search_id);
  if (!options.search_id && options.context) {
    const context = options.context;
    values.set("cluster_id", context.cluster_id);
    values.set("namespace", context.namespace);
    values.set("resource_id", context.resource.id);
    values.set("resource_kind", context.resource.kind);
    values.set("resource_name", context.resource.name);
    if (options.from) values.set("from", options.from);
    if (options.to) values.set("to", options.to);
  }
  const encoded = values.toString();
  return getJSON(`/api/v1/traces/${encodeURIComponent(traceID)}${encoded ? `?${encoded}` : ""}`, { signal });
}

export function saveTraceEvidence(queryID: string, traceID: string, spanIDs: string[]): Promise<TelemetryEvidence> {
  return postJSON(
    `/api/v1/traces/searches/${encodeURIComponent(queryID)}/traces/${encodeURIComponent(traceID)}/evidence`,
    { item_ids: spanIDs },
  );
}

export function createTelemetryConsultation(input: {
  title: string;
  cluster_id: string;
  environment: string;
  namespaces: string[];
  resource_refs: TelemetryResourceReference[];
  filters?: Record<string, unknown>;
  from: string;
  to: string;
  query_definition_refs?: string[];
  query_execution_refs: string[];
  evidence_refs: string[];
}): Promise<Consultation> {
  return postJSON("/api/v1/agent/consultations", input);
}
