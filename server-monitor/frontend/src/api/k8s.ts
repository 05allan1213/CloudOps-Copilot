import { getApiData } from "./client";
import type {
  K8sClusterOverview,
  K8sNodeListResult,
  K8sNodeDetail,
  K8sNodeQuery,
  K8sPodListResult,
  K8sPodQuery,
  K8sDeploymentListResult,
  K8sDeploymentQuery,
  K8sServiceListResult,
  K8sServiceQuery,
  K8sEventListResult,
  K8sEventQuery,
  K8sConfigMapListResult,
  K8sConfigMapQuery,
  K8sIngressListResult,
  K8sIngressQuery,
  K8sPVListResult,
  K8sPVCListResult,
  K8sResourceQuotaListResult,
  K8sLimitRangeListResult,
  K8sHPAListResult,
  K8sDaemonSetListResult,
  K8sStatefulSetListResult,
  K8sJobListResult,
  K8sTopologyData,
  K8sPVQuery,
  K8sPVCQuery,
  K8sResourceQuotaQuery,
  K8sLimitRangeQuery,
  K8sHPAQuery,
  K8sDaemonSetQuery,
  K8sStatefulSetQuery,
  K8sJobQuery,
} from "../types/k8s";
import type { K8sNodeSummary, K8sLogSnippet } from "../types";

/** Fetch the K8s cluster overview including node/pod/deployment stats and recent events */
export async function fetchK8sOverview(): Promise<K8sClusterOverview> {
  return (
    (await getApiData<K8sClusterOverview>("/api/v1/k8s/overview")) ?? {
      nodes: { total: 0, ready: 0, not_ready: 0 },
      nodes_available: false,
      pods: { total: 0, running: 0, pending: 0, failed: 0, succeeded: 0 },
      deployments: { total: 0, available: 0, unavailable: 0 },
      recent_events: [],
      host_coverage: { total_nodes: 0, covered_nodes: 0, uncovered_nodes: 0 },
      truncated: false,
      collected_at: "",
    }
  );
}

/** Fetch a filtered list of K8s nodes */
export async function fetchK8sNodes(
  query: K8sNodeQuery = {},
): Promise<K8sNodeListResult> {
  const params: Record<string, string> = {};

  if (query.status) {
    params.status = query.status;
  }
  if (query.role) {
    params.role = query.role;
  }
  if (query.search) {
    params.search = query.search;
  }
  if (query.limit) {
    params.limit = String(query.limit);
  }

  return (
    (await getApiData<K8sNodeListResult>("/api/v1/k8s/nodes", { params })) ?? {
      items: [],
      total: 0,
    }
  );
}

/** Fetch detailed information for a single K8s node by name */
export async function fetchK8sNodeDetail(
  name: string,
  signal?: AbortSignal,
): Promise<K8sNodeDetail> {
  return (
    (await getApiData<K8sNodeDetail>(
      `/api/v1/k8s/nodes/${encodeURIComponent(name)}`,
      signal ? { signal } : {},
    )) ?? null!
  );
}

/** Look up a K8s node by the Prometheus instance label of the host */
export async function fetchK8sNodeByInstance(
  instance: string,
  signal?: AbortSignal,
): Promise<K8sNodeSummary | null> {
  return (
    (await getApiData<K8sNodeSummary | null>(
      `/api/v1/k8s/nodes/by-instance/${encodeURIComponent(instance)}`,
      signal ? { signal } : {},
    )) ?? null
  );
}

/** Fetch a filtered list of K8s pods */
export async function fetchK8sPods(
  query: K8sPodQuery = {},
): Promise<K8sPodListResult> {
  const params: Record<string, string> = {};

  if (query.cluster) {
    params.cluster = query.cluster;
  }

  if (query.namespace) {
    params.namespace = query.namespace;
  }
  if (query.phase) {
    params.phase = query.phase;
  }
  if (query.search) {
    params.search = query.search;
  }
  if (query.limit) {
    params.limit = String(query.limit);
  }

  return (
    (await getApiData<K8sPodListResult>("/api/v1/k8s/pods", { params })) ?? {
      items: [],
      total: 0,
    }
  );
}

/** Fetch a filtered list of K8s deployments */
export async function fetchK8sDeployments(
  query: K8sDeploymentQuery = {},
): Promise<K8sDeploymentListResult> {
  const params: Record<string, string> = {};

  if (query.cluster) {
    params.cluster = query.cluster;
  }

  if (query.namespace) {
    params.namespace = query.namespace;
  }
  if (query.search) {
    params.search = query.search;
  }
  if (query.limit) {
    params.limit = String(query.limit);
  }

  return (
    (await getApiData<K8sDeploymentListResult>("/api/v1/k8s/deployments", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

/** Fetch a filtered list of K8s services */
export async function fetchK8sServices(
  query: K8sServiceQuery = {},
): Promise<K8sServiceListResult> {
  const params: Record<string, string> = {};

  if (query.cluster) {
    params.cluster = query.cluster;
  }

  if (query.namespace) {
    params.namespace = query.namespace;
  }
  if (query.type) {
    params.type = query.type;
  }
  if (query.search) {
    params.search = query.search;
  }
  if (query.limit) {
    params.limit = String(query.limit);
  }

  return (
    (await getApiData<K8sServiceListResult>("/api/v1/k8s/services", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

/** Fetch a filtered list of K8s events */
export async function fetchK8sEvents(
  query: K8sEventQuery = {},
): Promise<K8sEventListResult> {
  const params: Record<string, string> = {};

  if (query.cluster) {
    params.cluster = query.cluster;
  }

  if (query.namespace) {
    params.namespace = query.namespace;
  }
  if (query.type) {
    params.type = query.type;
  }
  if (query.search) {
    params.search = query.search;
  }
  if (query.limit) {
    params.limit = String(query.limit);
  }

  return (
    (await getApiData<K8sEventListResult>("/api/v1/k8s/events", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

/** Fetch log lines for a specific container in a pod */
export async function fetchK8sPodLogs(
  namespace: string,
  name: string,
  container?: string,
  tailLines?: number,
): Promise<K8sLogSnippet> {
  const params: Record<string, string> = {};

  if (container) {
    params.container = container;
  }
  if (tailLines) {
    params.tail_lines = String(tailLines);
  }

  return (
    (await getApiData<K8sLogSnippet>(
      `/api/v1/k8s/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs`,
      { params },
    )) ?? { namespace, pod_name: name, lines: [], truncated: false }
  );
}

export async function fetchK8sConfigMaps(
  query: K8sConfigMapQuery = {},
): Promise<K8sConfigMapListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sConfigMapListResult>("/api/v1/k8s/configmaps", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sIngresses(
  query: K8sIngressQuery = {},
): Promise<K8sIngressListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sIngressListResult>("/api/v1/k8s/ingresses", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sPVs(
  query: K8sPVQuery = {},
): Promise<K8sPVListResult> {
  const params: Record<string, string> = {};
  if (query.status) params.status = query.status;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sPVListResult>("/api/v1/k8s/persistent-volumes", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sPVCs(
  query: K8sPVCQuery = {},
): Promise<K8sPVCListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.status) params.status = query.status;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sPVCListResult>(
      "/api/v1/k8s/persistent-volume-claims",
      { params },
    )) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sResourceQuotas(
  query: K8sResourceQuotaQuery = {},
): Promise<K8sResourceQuotaListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sResourceQuotaListResult>(
      "/api/v1/k8s/resource-quotas",
      { params },
    )) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sLimitRanges(
  query: K8sLimitRangeQuery = {},
): Promise<K8sLimitRangeListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sLimitRangeListResult>("/api/v1/k8s/limit-ranges", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sHPAs(
  query: K8sHPAQuery = {},
): Promise<K8sHPAListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sHPAListResult>("/api/v1/k8s/hpas", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

/** Fetch the raw YAML definition of a K8s resource (read-only) */
export async function fetchK8sResourceYAML(
  kind: string,
  namespace: string,
  name: string,
): Promise<string> {
  return (
    (await getApiData<string>(
      `/api/v1/k8s/resources/${encodeURIComponent(kind)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/yaml`,
    )) ?? ""
  );
}

export async function fetchK8sDaemonSets(
  query: K8sDaemonSetQuery = {},
): Promise<K8sDaemonSetListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sDaemonSetListResult>("/api/v1/k8s/daemonsets", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sStatefulSets(
  query: K8sStatefulSetQuery = {},
): Promise<K8sStatefulSetListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sStatefulSetListResult>("/api/v1/k8s/statefulsets", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sJobs(
  query: K8sJobQuery = {},
): Promise<K8sJobListResult> {
  const params: Record<string, string> = {};
  if (query.namespace) params.namespace = query.namespace;
  if (query.status) params.status = query.status;
  if (query.search) params.search = query.search;
  if (query.limit) params.limit = String(query.limit);
  return (
    (await getApiData<K8sJobListResult>("/api/v1/k8s/jobs", {
      params,
    })) ?? { items: [], total: 0 }
  );
}

export async function fetchK8sTopology(
  namespace?: string,
): Promise<K8sTopologyData> {
  const params: Record<string, string> = {};
  if (namespace) params.namespace = namespace;
  return (
    (await getApiData<K8sTopologyData>("/api/v1/k8s/topology", {
      params,
    })) ?? { nodes: [], edges: [] }
  );
}

export async function fetchK8sClusters(): Promise<string[]> {
  const data = await getApiData<Array<{ name: string }>>("/api/v1/k8s/clusters");
  return data?.map((c) => c.name) ?? ["default"];
}
