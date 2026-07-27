import type { RouteRecordRaw } from "vue-router";

export const appRoutes: RouteRecordRaw[] = [
  { path: "/", redirect: "/overview", meta: { title: "总览", hidden: true } },
  { path: "/overview", name: "overview", component: () => import("../views/overview/OverviewView.vue"), meta: { title: "总览", workspace: "overview", fullBleed: true } },
  { path: "/infrastructure", name: "infrastructure", component: () => import("../views/infrastructure/InfrastructureView.vue"), meta: { title: "基础设施", workspace: "infrastructure", provider: "kubernetes" } },
  { path: "/monitoring", name: "monitoring", component: () => import("../views/monitoring/MonitoringView.vue"), meta: { title: "监控", workspace: "monitoring", provider: "prometheus" } },
  { path: "/alerts", name: "alerts", component: () => import("../views/alerts/AlertsView.vue"), meta: { title: "告警", workspace: "alerts", provider: "alertmanager" } },
  { path: "/alerts/:alertId", name: "alert-detail", component: () => import("../views/alerts/AlertDetailView.vue"), meta: { title: "告警详情", workspace: "alerts", provider: "alertmanager", hidden: true } },
  { path: "/logs", name: "logs", component: () => import("../views/logs/LogsView.vue"), meta: { title: "日志", workspace: "logs", provider: "elasticsearch" } },
  { path: "/traces", name: "traces", component: () => import("../views/traces/TracesView.vue"), meta: { title: "链路", workspace: "traces", provider: "tempo" } },
  { path: "/agent", name: "agent", component: () => import("../views/agent/AgentWorkspaceView.vue"), meta: { title: "Agent", workspace: "agent", provider: "llm", fullBleed: true } },
  { path: "/incidents", name: "incidents", component: () => import("../views/incidents/IncidentListView.vue"), meta: { title: "事件", workspace: "incidents" } },
  { path: "/incidents/:incidentId", name: "incident-detail", component: () => import("../views/incidents/IncidentDetailView.vue"), meta: { title: "事件详情", workspace: "incidents", hidden: true } },
  { path: "/devops", name: "devops", component: () => import("../views/devops/DevOpsWorkspaceView.vue"), meta: { title: "DevOps", workspace: "devops", fullBleed: true } },
  { path: "/settings", name: "settings", component: () => import("../views/settings/SettingsView.vue"), meta: { title: "设置", workspace: "settings" } },
  { path: "/:pathMatch(.*)*", name: "not-found", component: () => import("../pages/NotFoundPage.vue"), meta: { title: "页面不存在", hidden: true } },
];
