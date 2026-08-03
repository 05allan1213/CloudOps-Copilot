type RouteLoader = () => Promise<unknown>;

export interface PrefetchNavigator {
  connection?: { saveData?: boolean; effectiveType?: string };
  deviceMemory?: number;
  hardwareConcurrency?: number;
}

const routeLoaders: Record<string, RouteLoader> = {
  "/overview": () => import("../views/overview/OverviewView.vue"),
  "/infrastructure": () => import("../views/infrastructure/InfrastructureView.vue"),
  "/monitoring": () => import("../views/monitoring/MonitoringView.vue"),
  "/alerts": () => import("../views/alerts/AlertsView.vue"),
  "/logs": () => import("../views/logs/LogsView.vue"),
  "/traces": () => import("../views/traces/TracesView.vue"),
  "/incidents": () => import("../views/incidents/IncidentListView.vue"),
  "/devops": () => import("../views/devops/DevOpsWorkspaceView.vue"),
  "/settings": () => import("../views/settings/SettingsView.vue"),
};

const prefetchedRoutes = new Set<string>();

export function supportsIntentPrefetch(source?: PrefetchNavigator): boolean {
  const candidate = source ?? (typeof navigator === "undefined" ? undefined : navigator as PrefetchNavigator);
  if (!candidate || candidate.connection?.saveData) return false;
  if (["slow-2g", "2g"].includes(candidate.connection?.effectiveType ?? "")) return false;
  if (candidate.deviceMemory !== undefined && candidate.deviceMemory < 4) return false;
  if (candidate.hardwareConcurrency !== undefined && candidate.hardwareConcurrency < 4) return false;
  return true;
}

export function prefetchRouteOnIntent(path: string, source?: PrefetchNavigator): Promise<boolean> {
  const normalized = path.split(/[?#]/, 1)[0] ?? path;
  const loader = routeLoaders[normalized];
  if (!loader || prefetchedRoutes.has(normalized) || !supportsIntentPrefetch(source)) return Promise.resolve(false);
  prefetchedRoutes.add(normalized);
  return loader().then(
    () => true,
    () => {
      prefetchedRoutes.delete(normalized);
      return false;
    },
  );
}
