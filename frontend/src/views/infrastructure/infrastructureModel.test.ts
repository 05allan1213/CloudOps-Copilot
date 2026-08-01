import { describe, expect, it } from "vitest";

import type { InfrastructureContextLink } from "../../api/infrastructure";
import {
  canonicalResourceRef,
  infrastructureContextLocation,
  kindsForResourceType,
  queryValues,
  resolveResourceSelection,
  resourceTypeForKinds,
  sortResourcesByAttention,
  summarizeInfrastructureHealth,
} from "./infrastructureModel";
import type { KubernetesResource } from "../../api/infrastructure";

function resource(id: string, state: KubernetesResource["health"]["state"], overrides: Partial<KubernetesResource> = {}): KubernetesResource {
  return {
    id,
    api_version: "v1",
    kind: "Pod",
    layer: "pod",
    namespace: "ops",
    name: id,
    health: { state, summary: state },
    owner_references: [],
    selector: {},
    labels: {},
    endpoints: [],
    ports: [],
    conditions: [],
    addresses: [],
    links: [],
    ...overrides,
  };
}

function contextLink(overrides: Partial<InfrastructureContextLink> = {}): InfrastructureContextLink {
  return {
    kind: "internal",
    label: "查看监控",
    href: "/monitoring?resource=Pod%2Fdefault%2Fapi-0&from=2026-07-31T00%3A00%3A00Z",
    target: "current",
    availability: "available",
    ...overrides,
  };
}

describe("Infrastructure Query model", () => {
  it("decodes repeated or comma-separated Kind values", () => {
    expect(queryValues(["Deployment", "StatefulSet", null])).toEqual(["Deployment", "StatefulSet"]);
    expect(queryValues("Deployment, StatefulSet,,DaemonSet")).toEqual([
      "Deployment", "StatefulSet", "DaemonSet",
    ]);
  });

  it("maps resource tabs to typed Kubernetes Kind filters", () => {
    expect(kindsForResourceType("workload")).toEqual(["Deployment", "StatefulSet", "DaemonSet"]);
    expect(kindsForResourceType("all")).toEqual([]);
    expect(resourceTypeForKinds(["StatefulSet", "Deployment", "DaemonSet"])).toBe("workload");
    expect(resourceTypeForKinds(["Pod"])).toBe("pod");
    expect(resourceTypeForKinds(["CustomResource"])).toBe("all");
  });

  it("summarizes and orders real resources by operational attention", () => {
    const resources = [
      resource("healthy", "healthy"),
      resource("unknown", "unknown"),
      resource("critical", "critical"),
      resource("warning", "warning"),
    ];
    expect(summarizeInfrastructureHealth(resources)).toEqual({
      total: 4,
      healthy: 1,
      warning: 1,
      critical: 1,
      unknown: 1,
      attention: 3,
    });
    expect(sortResourcesByAttention(resources).map((item) => item.id)).toEqual([
      "critical", "warning", "unknown", "healthy",
    ]);
  });

  it("resolves internal IDs and legacy canonical resource references", () => {
    const pod = resource("resource-pod", "healthy", { name: "api-0" });
    const node = resource("resource-node", "healthy", { kind: "Node", layer: "node", namespace: undefined, name: "worker-1" });
    expect(canonicalResourceRef(pod)).toBe("Pod/ops/api-0");
    expect(resolveResourceSelection([pod, node], "resource-pod")).toBe(pod);
    expect(resolveResourceSelection([pod, node], "Pod/ops/api-0")).toBe(pod);
    expect(resolveResourceSelection([pod, node], "Node/worker-1")).toBe(node);
  });
});

describe("Infrastructure context links", () => {
  it("accepts only allowlisted current-route links and preserves bounded Query", () => {
    expect(infrastructureContextLocation(contextLink())).toEqual({
      path: "/monitoring",
      query: {
        resource: "Pod/default/api-0",
        from: "2026-07-31T00:00:00Z",
      },
    });
    expect(infrastructureContextLocation(contextLink({
      href: "/logs?level=error&level=warn",
      label: "查看日志",
    }))).toEqual({ path: "/logs", query: { level: ["error", "warn"] } });
  });

  it("rejects external, unavailable, operation, malformed, and unowned routes", () => {
    expect(infrastructureContextLocation(contextLink({ target: "external" }))).toBeNull();
    expect(infrastructureContextLocation(contextLink({ availability: "unavailable" }))).toBeNull();
    expect(infrastructureContextLocation(contextLink({ kind: "operation" }))).toBeNull();
    expect(infrastructureContextLocation(contextLink({ href: "https://example.com/monitoring" }))).toBeNull();
    expect(infrastructureContextLocation(contextLink({ href: "/settings" }))).toBeNull();
    expect(infrastructureContextLocation(contextLink({ href: "/logs\\next" }))).toBeNull();
  });

  it("rejects oversized Query values", () => {
    expect(infrastructureContextLocation(contextLink({ href: `/logs?query=${"x".repeat(513)}` }))).toBeNull();
  });
});
