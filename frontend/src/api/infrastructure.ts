import { queryCache, queryIdentityFor } from "../composables/queryCache";
import { apiURL, getJSON } from "./client";
import type { OperationalScope } from "./platform";

export type KubernetesProviderState = "available" | "partial" | "unavailable" | "disabled";
export type ResourceHealthState = "healthy" | "warning" | "critical" | "unknown";
export type ResourceLayer = "namespace" | "service" | "workload" | "pod" | "node" | "gateway";

export interface ProviderSource {
  provider: "kubernetes";
  cluster_id: string;
  identity: string;
  server_version?: string;
  collected_at: string;
}

export interface Freshness {
  state: "fresh" | "stale" | "unavailable";
  fresh_until: string;
  age_seconds: number;
}

export interface ResourceReference {
  id?: string;
  kind: string;
  namespace?: string;
  name: string;
}

export interface ResourceEndpoint {
  address: string;
  ready?: boolean | null;
  target_id?: string;
  target_ref?: string;
}

export interface ResourcePort {
  name?: string;
  protocol: string;
  port: number;
  target_port?: string;
}

export interface ResourceCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  last_transition_time?: string;
}

export interface WorkloadStatus {
  desired_replicas: number;
  updated_replicas: number;
  ready_replicas: number;
  available_replicas: number;
  observed_generation: number;
}

export interface InfrastructureContextLink {
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

export interface KubernetesResource {
  id: string;
  source_uid?: string;
  api_version: string;
  kind: string;
  layer: ResourceLayer;
  namespace?: string;
  name: string;
  resource_version?: string;
  generation?: number;
  status?: string;
  workload?: WorkloadStatus;
  health: { state: ResourceHealthState; summary: string };
  owner_references: ResourceReference[];
  selector: Record<string, string>;
  labels: Record<string, string>;
  endpoints: ResourceEndpoint[];
  ports: ResourcePort[];
  conditions: ResourceCondition[];
  node_name?: string;
  addresses: string[];
  created_at?: string;
  links: InfrastructureContextLink[];
}

export interface TopologyEdge {
  id: string;
  source_id: string;
  target_id: string;
  relation: "contains" | "owns" | "selects" | "routes_to" | "scheduled_on" | "backend_ref";
  source_fact: string;
}

export interface ProviderIssue {
  namespace?: string;
  operation: string;
  code: string;
  detail: string;
}

export interface TopologySnapshot {
  id?: string;
  content_hash?: string;
  configuration_revision_id: string;
  scope: OperationalScope;
  provider_state: KubernetesProviderState;
  provider_detail: string;
  source: ProviderSource;
  freshness: Freshness;
  nodes: KubernetesResource[];
  edges: TopologyEdge[];
  issues: ProviderIssue[];
  partial: boolean;
  truncated: boolean;
  collected_at: string;
}

export interface ResourcePage {
  snapshot_id?: string;
  scope: OperationalScope;
  provider_state: KubernetesProviderState;
  source: ProviderSource;
  freshness: Freshness;
  items: KubernetesResource[];
  next_cursor?: string;
  partial: boolean;
  truncated: boolean;
  collected_at: string;
}

export interface ResourceDetail {
  snapshot_id: string;
  scope: OperationalScope;
  provider_state: KubernetesProviderState;
  source: ProviderSource;
  freshness: Freshness;
  resource: KubernetesResource;
  related: KubernetesResource[];
  edges: TopologyEdge[];
  partial: boolean;
  collected_at: string;
}

export interface KubernetesEvent {
  id: string;
  type?: string;
  reason?: string;
  message?: string;
  count?: number;
  resource_kind: string;
  resource_name: string;
  namespace?: string;
  observed_at: string;
  collected_at: string;
}

export interface EventPage {
  snapshot_id: string;
  scope: OperationalScope;
  provider_state: KubernetesProviderState;
  source: ProviderSource;
  resource_id: string;
  items: KubernetesEvent[];
  partial: boolean;
  truncated: boolean;
  collected_at: string;
}

export interface TopologyRefreshEvent {
  cursor: string;
  snapshot_id?: string;
  content_hash?: string;
  provider_state: KubernetesProviderState;
  collected_at: string;
}

export interface InfrastructureQuery {
  cluster?: string;
  namespace?: string;
  kind?: string[];
  search?: string;
  cursor?: string;
  limit?: number;
  from?: string;
  to?: string;
}

function queryString(query: InfrastructureQuery = {}): string {
  const values = new URLSearchParams();
  if (query.cluster) values.set("cluster", query.cluster);
  if (query.namespace) values.set("namespace", query.namespace);
  for (const kind of query.kind ?? []) values.append("kind", kind);
  if (query.search) values.set("search", query.search);
  if (query.cursor) values.set("cursor", query.cursor);
  if (query.limit) values.set("limit", String(query.limit));
  if (query.from) values.set("from", query.from);
  if (query.to) values.set("to", query.to);
  const encoded = values.toString();
  return encoded ? `?${encoded}` : "";
}

function infrastructureReadIdentity(url: string) {
  return queryIdentityFor("infrastructure", { url });
}

export function projectResolvedInfrastructureScope(
  query: InfrastructureQuery,
  resolvedCluster: string,
): number {
  if (!resolvedCluster || query.cluster) return 0;
  const canonicalQuery = { ...query, cluster: resolvedCluster };
  const paths = ["/api/v1/topology", "/api/v1/resources"];
  return paths.reduce((count, path) => {
    const sourceURL = `${path}${queryString(query)}`;
    const canonicalURL = `${path}${queryString(canonicalQuery)}`;
    return count + (queryCache.project(
      infrastructureReadIdentity(sourceURL),
      infrastructureReadIdentity(canonicalURL),
    ) ? 1 : 0);
  }, 0);
}

export function getTopology(query: InfrastructureQuery = {}, signal?: AbortSignal): Promise<TopologySnapshot> {
  return getJSON(`/api/v1/topology${queryString(query)}`, { signal });
}

export function openTopologyEventStream(
  query: InfrastructureQuery,
  onEvent: (event: TopologyRefreshEvent) => void,
  onError?: (event?: Event) => void,
  onOpen?: () => void,
): () => void {
  const streamQuery: InfrastructureQuery = {
    cluster: query.cluster,
    namespace: query.namespace,
    from: query.from,
    to: query.to,
  };
  const source = new EventSource(apiURL(`/api/v1/topology/events${queryString(streamQuery)}`));
  source.addEventListener("topology.refresh", (event) => {
    try {
      const message = event as MessageEvent<string>;
      const payload = JSON.parse(message.data) as Omit<TopologyRefreshEvent, "cursor">;
      const cursor = message.lastEventId.trim();
      if (!cursor || !payload.provider_state || !payload.collected_at) throw new Error("Invalid topology refresh event");
      onEvent({ cursor, ...payload });
    } catch {
      onError?.();
    }
  });
  source.onopen = () => onOpen?.();
  source.onerror = (event) => onError?.(event);
  return () => source.close();
}

export function getResources(query: InfrastructureQuery = {}, signal?: AbortSignal): Promise<ResourcePage> {
  return getJSON(`/api/v1/resources${queryString(query)}`, { signal });
}

export function getResource(id: string, query: InfrastructureQuery = {}, signal?: AbortSignal): Promise<ResourceDetail> {
  return getJSON(`/api/v1/resources/${encodeURIComponent(id)}${queryString(query)}`, { signal });
}

export function getResourceEvents(id: string, query: InfrastructureQuery = {}, signal?: AbortSignal): Promise<EventPage> {
  return getJSON(`/api/v1/resources/${encodeURIComponent(id)}/events${queryString(query)}`, { signal });
}
