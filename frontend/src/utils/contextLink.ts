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
  if (!prefix || link.external || !safeInternalPath(link.path)) return null;
  if (link.path !== prefix && !link.path.startsWith(`${prefix}/`)) return null;
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(link.operational_scope_id)) return null;
  let queryLength = 0;
  for (const [key, value] of Object.entries(link.query)) {
    if (!/^[A-Za-z0-9_.-]{1,64}$/.test(key) || value.length > 512 || /[\0\r\n]/.test(value)) return null;
    queryLength += encodeURIComponent(key).length + encodeURIComponent(value).length + 2;
    if (queryLength > 2048) return null;
  }
  return { path: link.path, query: link.query };
}

function safeInternalPath(path: string): boolean {
  if (!path.startsWith("/") || /[\0\r\n\\]/.test(path)) return false;
  try {
    const parsed = new URL(path, "https://cloudops.invalid");
    return parsed.origin === "https://cloudops.invalid"
      && parsed.pathname === path
      && !parsed.search
      && !parsed.hash;
  } catch {
    return false;
  }
}
