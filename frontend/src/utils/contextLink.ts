import type { RouteLocationRaw } from "vue-router";

import type { ContextLink } from "../api/notifications";

const workspacePrefixes: Record<string, string> = {
  overview: "/overview",
  infrastructure: "/infrastructure",
  monitoring: "/monitoring",
  alerts: "/alerts",
  logs: "/logs",
  traces: "/traces",
  agent: "/agent",
  incidents: "/incidents",
  devops: "/devops",
  settings: "/settings",
};

export function contextLocation(link: ContextLink): RouteLocationRaw | null {
  const prefix = workspacePrefixes[link.workspace];
  if (!prefix || link.external || (link.path !== prefix && !link.path.startsWith(`${prefix}/`))) return null;
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(link.operational_scope_id)) return null;
  for (const [key, value] of Object.entries(link.query)) {
    if (!key || key.length > 64 || value.length > 512 || /[\0\r\n]/.test(key + value)) return null;
  }
  return { path: link.path, query: link.query };
}
