import type { LocationQuery, LocationQueryRaw, RouteLocationRaw } from "vue-router";

import type {
  InfrastructureContextLink,
  KubernetesResource,
  ResourceHealthState,
  ResourceLayer,
} from "../../api/infrastructure";

export type InfrastructureResourceType = "all" | ResourceLayer;

export const infrastructureResourceTypeItems: Array<{ label: string; value: InfrastructureResourceType }> = [
  { label: "全部", value: "all" },
  { label: "Namespace", value: "namespace" },
  { label: "Service", value: "service" },
  { label: "Workload", value: "workload" },
  { label: "Pod", value: "pod" },
  { label: "Node", value: "node" },
  { label: "Gateway", value: "gateway" },
];

const resourceTypeKinds: Record<Exclude<InfrastructureResourceType, "all">, readonly string[]> = {
  namespace: ["Namespace"],
  service: ["Service"],
  workload: ["Deployment", "StatefulSet", "DaemonSet"],
  pod: ["Pod"],
  node: ["Node"],
  gateway: ["Ingress", "Gateway"],
};

export function queryValues(value: LocationQuery[string]): string[] {
  if (Array.isArray(value)) return value.filter((item): item is string => typeof item === "string" && Boolean(item));
  return typeof value === "string" && value ? value.split(",").map((item) => item.trim()).filter(Boolean) : [];
}

export function kindsForResourceType(type: InfrastructureResourceType): string[] {
  return type === "all" ? [] : [...resourceTypeKinds[type]];
}

export function resourceTypeForKinds(kinds: readonly string[]): InfrastructureResourceType {
  if (!kinds.length) return "all";
  const normalized = [...new Set(kinds.map((kind) => kind.toLocaleLowerCase()))].sort();
  for (const [type, values] of Object.entries(resourceTypeKinds) as Array<[Exclude<InfrastructureResourceType, "all">, readonly string[]]>) {
    const expected = values.map((value) => value.toLocaleLowerCase()).sort();
    if (expected.length === normalized.length && expected.every((value, index) => value === normalized[index])) return type;
    if (normalized.length === 1 && expected.includes(normalized[0])) return type;
  }
  return "all";
}

const healthPriority: Record<ResourceHealthState, number> = {
  critical: 0,
  warning: 1,
  unknown: 2,
  healthy: 3,
};

export interface InfrastructureHealthSummary {
  total: number;
  healthy: number;
  warning: number;
  critical: number;
  unknown: number;
  attention: number;
}

export function summarizeInfrastructureHealth(resources: readonly KubernetesResource[]): InfrastructureHealthSummary {
  const summary: InfrastructureHealthSummary = {
    total: resources.length,
    healthy: 0,
    warning: 0,
    critical: 0,
    unknown: 0,
    attention: 0,
  };
  for (const resource of resources) summary[resource.health.state] += 1;
  summary.attention = summary.critical + summary.warning + summary.unknown;
  return summary;
}

export function sortResourcesByAttention(resources: readonly KubernetesResource[]): KubernetesResource[] {
  return [...resources].sort((left, right) => (
    healthPriority[left.health.state] - healthPriority[right.health.state]
    || left.kind.localeCompare(right.kind)
    || (left.namespace ?? "").localeCompare(right.namespace ?? "")
    || left.name.localeCompare(right.name)
  ));
}

export function canonicalResourceRef(resource: KubernetesResource): string {
  return [resource.kind, resource.namespace, resource.name].filter(Boolean).join("/");
}

export function resolveResourceSelection(
  resources: readonly KubernetesResource[],
  selection: string,
): KubernetesResource | undefined {
  const normalized = decodeURIComponent(selection).toLocaleLowerCase();
  return resources.find((resource) => (
    resource.id.toLocaleLowerCase() === normalized
    || canonicalResourceRef(resource).toLocaleLowerCase() === normalized
    || `${resource.kind}/${resource.name}`.toLocaleLowerCase() === normalized
  ));
}

const allowedContextPaths = new Set(["/monitoring", "/logs", "/traces", "/agent"]);

export function infrastructureContextLocation(link: InfrastructureContextLink): RouteLocationRaw | null {
  if (link.kind !== "internal" || link.target !== "current" || link.availability !== "available") return null;
  if (!link.href.startsWith("/") || /[\0\r\n\\]/.test(link.href)) return null;
  try {
    const parsed = new URL(link.href, "https://cloudops.invalid");
    if (parsed.origin !== "https://cloudops.invalid" || !allowedContextPaths.has(parsed.pathname)) return null;
    const query: LocationQueryRaw = {};
    let encodedLength = 0;
    for (const [key, value] of parsed.searchParams.entries()) {
      if (!/^[A-Za-z0-9_.-]{1,64}$/.test(key) || value.length > 512 || /[\0\r\n]/.test(value)) return null;
      encodedLength += encodeURIComponent(key).length + encodeURIComponent(value).length + 2;
      if (encodedLength > 2_048) return null;
      const current = query[key];
      if (current === undefined) query[key] = value;
      else query[key] = [...(Array.isArray(current) ? current : [current]), value];
    }
    return { path: parsed.pathname, query };
  } catch {
    return null;
  }
}
