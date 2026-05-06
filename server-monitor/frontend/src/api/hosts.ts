import { getApiData } from "./client";
import type { DashboardOverview, Host, HostMetricsResponse } from "../types";

export interface HostsQuery {
  status?: "all" | "up" | "down";
  q?: string;
  sort?: "instance" | "cpu_desc" | "memory_desc";
  risk?: "all" | "high_cpu" | "high_memory";
  group?: number;
}

export async function fetchHosts(query: HostsQuery = {}): Promise<Host[]> {
  const params: Record<string, string> = {};

  if (query.status && query.status !== "all") {
    params.status = query.status;
  }
  if (query.q) {
    params.q = query.q;
  }
  if (query.sort && query.sort !== "instance") {
    params.sort = query.sort;
  }
  if (query.risk && query.risk !== "all") {
    params.risk = query.risk;
  }
  if (query.group) {
    params.group = String(query.group);
  }

  return (await getApiData<Host[]>("/api/v1/hosts", { params })) ?? [];
}

export interface HostMetricsQuery {
  range?: "15m" | "1h" | "6h" | "24h";
  mountpoint?: string;
}

export async function fetchHostMetrics(
  instance: string,
  query: HostMetricsQuery = {},
  signal?: AbortSignal,
): Promise<HostMetricsResponse> {
  const params: Record<string, string> = {};

  if (query.range) {
    params.range = query.range;
  }
  if (query.mountpoint) {
    params.mountpoint = query.mountpoint;
  }

  return (
    (await getApiData<HostMetricsResponse>(
      `/api/v1/hosts/${encodeURIComponent(instance)}/metrics`,
      { params, signal },
    )) ?? { metrics: {} }
  );
}

export async function fetchDashboardOverview(): Promise<DashboardOverview> {
  return (
    (await getApiData<DashboardOverview>("/api/v1/dashboard/overview")) ?? {
      total_hosts: 0,
      healthy_hosts: 0,
      down_hosts: 0,
      avg_cpu: 0,
      avg_memory: 0,
      active_alerts: 0,
    }
  );
}
