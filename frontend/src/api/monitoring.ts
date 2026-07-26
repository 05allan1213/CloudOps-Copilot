import { getJSON, postJSON } from "./client";
import type { OperationalScope } from "./platform";

export type QueryMode = "guided" | "expert";
export type ExecutionActor = "owner" | "agent";
export type ExecutionStatus = "pending" | "running" | "succeeded" | "failed" | "cancelled";
export type MonitoringProviderState = "available" | "partial" | "unavailable" | "disabled";
export type AuthorizationMode = "run_once" | "definition";

export interface MonitoringResourceReference {
  id: string;
  kind: string;
  namespace: string;
  name: string;
}

export interface QueryBounds {
  max_lookback_seconds: number;
  timeout_ms: number;
  max_response_bytes: number;
  max_series: number;
  max_samples: number;
  concurrency_limit: number;
  step_seconds: number;
}

export interface ProviderSource {
  provider: "prometheus";
  identity: string;
  server_version?: string;
  collected_at: string;
}

export interface MonitoringContextLink {
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

export interface CatalogEntry {
  key: string;
  title: string;
  description: string;
  unit: string;
  query: string;
}

export interface MonitoringCatalog {
  configuration_revision_id: string;
  scope: OperationalScope;
  resource: MonitoringResourceReference;
  provider_state: MonitoringProviderState;
  provider_detail: string;
  source: ProviderSource;
  metric_names: string[];
  queries: CatalogEntry[];
  bounds: QueryBounds;
  partial: boolean;
  collected_at: string;
}

export interface QueryPoint {
  timestamp: string;
  value: number;
}

export interface QuerySeries {
  labels: Record<string, string>;
  points: QueryPoint[];
}

export interface QueryResult {
  result_type: string;
  series: QuerySeries[];
}

export interface ExecutionEvent {
  id: string;
  sequence: number;
  type: string;
  actor: string;
  detail?: string;
  created_at: string;
}

export interface QueryExecution {
  id: string;
  configuration_revision_id: string;
  query_definition_id?: string;
  query_authorization_id?: string;
  actor: ExecutionActor;
  provider: "prometheus";
  mode: QueryMode;
  catalog_key?: string;
  query: string;
  query_hash: string;
  scope: OperationalScope;
  resource: MonitoringResourceReference;
  time_range: { from: string; to: string };
  bounds: QueryBounds;
  status: ExecutionStatus;
  source: ProviderSource;
  result?: QueryResult;
  result_expired: boolean;
  series_count: number;
  sample_count: number;
  response_bytes: number;
  partial: boolean;
  truncated: boolean;
  error_code?: string;
  error_detail?: string;
  links: MonitoringContextLink[];
  events: ExecutionEvent[];
  created_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface QueryDefinition {
  id: string;
  definition_key: string;
  revision: number;
  configuration_revision_id: string;
  provider: "prometheus";
  mode: QueryMode;
  catalog_key?: string;
  title: string;
  description?: string;
  query: string;
  query_hash: string;
  scope: OperationalScope;
  resource: MonitoringResourceReference;
  max_lookback_seconds: number;
  max_series: number;
  max_samples: number;
  content_hash: string;
  created_by: string;
  created_at: string;
  links: MonitoringContextLink[];
}

export interface QueryAuthorization {
  id: string;
  configuration_revision_id: string;
  mode: AuthorizationMode;
  query_definition_id?: string;
  provider: "prometheus";
  query_mode: QueryMode;
  catalog_key?: string;
  query: string;
  query_hash: string;
  scope: OperationalScope;
  resource: MonitoringResourceReference;
  max_lookback_seconds: number;
  max_series: number;
  max_samples: number;
  consumed_execution_id?: string;
  revoked_at?: string;
  created_by: string;
  created_at: string;
}

export interface MonitoringContext {
  cluster_id: string;
  namespace: string;
  resource: MonitoringResourceReference;
}

export interface StartQueryInput extends MonitoringContext {
  mode: QueryMode;
  catalog_key?: string;
  query?: string;
  from: string;
  to: string;
  step_seconds: number;
  query_definition_id?: string;
}

export interface ExecutionFilter {
  cluster_id?: string;
  namespace?: string;
  resource_id?: string;
  limit?: number;
}

function catalogQuery(context: MonitoringContext): string {
  const query = new URLSearchParams({
    cluster_id: context.cluster_id,
    namespace: context.namespace,
    resource_id: context.resource.id,
    resource_kind: context.resource.kind,
    resource_name: context.resource.name,
  });
  return query.toString();
}

function historyQuery(filter: ExecutionFilter): string {
  const query = new URLSearchParams();
  if (filter.cluster_id) query.set("cluster_id", filter.cluster_id);
  if (filter.namespace) query.set("namespace", filter.namespace);
  if (filter.resource_id) query.set("resource_id", filter.resource_id);
  if (filter.limit) query.set("limit", String(filter.limit));
  const encoded = query.toString();
  return encoded ? `?${encoded}` : "";
}

export function getMonitoringCatalog(context: MonitoringContext, signal?: AbortSignal): Promise<MonitoringCatalog> {
  return getJSON(`/api/v1/monitoring/catalog?${catalogQuery(context)}`, { signal });
}

export function startMonitoringQuery(input: StartQueryInput): Promise<QueryExecution> {
  return postJSON("/api/v1/monitoring/queries", input);
}

export function getMonitoringQuery(id: string, signal?: AbortSignal): Promise<QueryExecution> {
  return getJSON(`/api/v1/monitoring/queries/${encodeURIComponent(id)}`, { signal });
}

export async function getMonitoringQueries(filter: ExecutionFilter = {}, signal?: AbortSignal): Promise<QueryExecution[]> {
  const page = await getJSON<{ items: QueryExecution[] }>(`/api/v1/monitoring/queries${historyQuery(filter)}`, { signal });
  return page.items;
}

export function cancelMonitoringQuery(id: string): Promise<QueryExecution> {
  return postJSON(`/api/v1/monitoring/queries/${encodeURIComponent(id)}/cancel`);
}

export function saveQueryDefinition(input: {
  query_execution_id: string;
  previous_query_definition_id?: string;
  title: string;
  description?: string;
}): Promise<QueryDefinition> {
  return postJSON("/api/v1/monitoring/query-definitions", input);
}

export async function getQueryDefinitions(limit = 50, signal?: AbortSignal): Promise<QueryDefinition[]> {
  const page = await getJSON<{ items: QueryDefinition[] }>(`/api/v1/monitoring/query-definitions?limit=${limit}`, { signal });
  return page.items;
}

export function createQueryAuthorization(input: {
  mode: AuthorizationMode;
  query_execution_id?: string;
  query_definition_id?: string;
}): Promise<QueryAuthorization> {
  return postJSON("/api/v1/monitoring/query-authorizations", input);
}

export async function getQueryAuthorizations(limit = 50, signal?: AbortSignal): Promise<QueryAuthorization[]> {
  const page = await getJSON<{ items: QueryAuthorization[] }>(`/api/v1/monitoring/query-authorizations?limit=${limit}`, { signal });
  return page.items;
}

export async function revokeQueryAuthorization(id: string): Promise<void> {
  await postJSON(`/api/v1/monitoring/query-authorizations/${encodeURIComponent(id)}/revoke`);
}
