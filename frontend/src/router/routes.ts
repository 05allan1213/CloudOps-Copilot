import type { LocationQuery, LocationQueryRaw, RouteLocationRaw, RouteRecordRaw } from "vue-router";

function firstQueryValue(value: LocationQuery[string]): string {
  if (Array.isArray(value)) return value.find((item): item is string => typeof item === "string") ?? "";
  return typeof value === "string" ? value : "";
}

export function normalizeAtlasQuery(query: LocationQuery): LocationQueryRaw {
  const normalized: LocationQueryRaw = { ...query };
  const view = firstQueryValue(query.view);
  if (view === "structured") normalized.view = "structured";
  else delete normalized.view;
  return normalized;
}

export function legacyAtlasLocation(query: LocationQuery): RouteLocationRaw | undefined {
  const view = firstQueryValue(query.view);
  const resource = firstQueryValue(query.resource);
  if (!resource && view !== "atlas" && view !== "canvas" && view !== "structured") return undefined;
  return { name: "atlas", query: normalizeAtlasQuery(query), replace: true };
}

function canonicalAtlasLocation(query: LocationQuery): RouteLocationRaw | undefined {
  const view = firstQueryValue(query.view);
  if (!view || view === "structured") return undefined;
  return { name: "atlas", query: normalizeAtlasQuery(query), replace: true };
}

export const appRoutes: RouteRecordRaw[] = [
  { path: "/", redirect: "/overview", meta: { title: "总览", hidden: true } },
  { path: "/overview", name: "overview", component: () => import("../views/overview/OverviewView.vue"), beforeEnter: (to) => legacyAtlasLocation(to.query), meta: { title: "总览", workspace: "overview", uiOwner: "nuxt-ui" } },
  { path: "/atlas", name: "atlas", component: () => import("../views/atlas/AtlasView.vue"), beforeEnter: (to) => canonicalAtlasLocation(to.query), meta: { title: "Operations Atlas", workspace: "overview", provider: "kubernetes", fullBleed: true, hidden: true, uiOwner: "nuxt-ui" } },
  { path: "/infrastructure", name: "infrastructure", component: () => import("../views/infrastructure/InfrastructureView.vue"), meta: { title: "基础设施", workspace: "infrastructure", provider: "kubernetes", uiOwner: "nuxt-ui" } },
  { path: "/monitoring", name: "monitoring", component: () => import("../views/monitoring/MonitoringView.vue"), meta: { title: "监控", workspace: "monitoring", provider: "prometheus", uiOwner: "legacy-element-plus" } },
  { path: "/alerts", name: "alerts", component: () => import("../views/alerts/AlertsView.vue"), meta: { title: "告警", workspace: "alerts", provider: "alertmanager", uiOwner: "legacy-element-plus" } },
  { path: "/alerts/:alertId", name: "alert-detail", component: () => import("../views/alerts/AlertDetailView.vue"), meta: { title: "告警详情", workspace: "alerts", provider: "alertmanager", hidden: true, uiOwner: "legacy-element-plus" } },
  { path: "/logs", name: "logs", component: () => import("../views/logs/LogsView.vue"), meta: { title: "日志", workspace: "logs", provider: "elasticsearch", uiOwner: "legacy-element-plus" } },
  { path: "/traces", name: "traces", component: () => import("../views/traces/TracesView.vue"), meta: { title: "链路", workspace: "traces", provider: "tempo", uiOwner: "legacy-element-plus" } },
  { path: "/agent", name: "agent", component: () => import("../views/agent/AgentWorkspaceView.vue"), meta: { title: "Agent", workspace: "agent", provider: "llm", fullBleed: true, uiOwner: "legacy-element-plus" } },
  { path: "/incidents", name: "incidents", component: () => import("../views/incidents/IncidentListView.vue"), meta: { title: "事件", workspace: "incidents", uiOwner: "legacy-element-plus" } },
  { path: "/incidents/:incidentId", name: "incident-detail", component: () => import("../views/incidents/IncidentDetailView.vue"), meta: { title: "事件详情", workspace: "incidents", hidden: true, uiOwner: "legacy-element-plus" } },
  { path: "/devops", name: "devops", component: () => import("../views/devops/DevOpsWorkspaceView.vue"), meta: { title: "DevOps", workspace: "devops", uiOwner: "legacy-element-plus" } },
  { path: "/settings", name: "settings", component: () => import("../views/settings/SettingsView.vue"), meta: { title: "设置", workspace: "settings", uiOwner: "legacy-element-plus" } },
  { path: "/:pathMatch(.*)*", name: "not-found", component: () => import("../pages/NotFoundPage.vue"), meta: { title: "页面不存在", hidden: true, uiOwner: "nuxt-ui" } },
];
