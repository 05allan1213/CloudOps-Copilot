import type { RouteRecordRaw } from "vue-router";

const workspacePlaceholder = () => import("../views/workspaces/WorkspaceStatusView.vue");

export const appRoutes: RouteRecordRaw[] = [
  { path: "/", redirect: "/overview", meta: { title: "总览", hidden: true } },
  { path: "/overview", name: "overview", component: () => import("../views/overview/OverviewView.vue"), meta: { title: "总览", workspace: "overview", fullBleed: true } },
  { path: "/infrastructure", name: "infrastructure", component: () => import("../views/infrastructure/InfrastructureView.vue"), meta: { title: "基础设施", workspace: "infrastructure", provider: "kubernetes" } },
  { path: "/monitoring", name: "monitoring", component: workspacePlaceholder, meta: { title: "监控", workspace: "monitoring", provider: "prometheus" } },
  { path: "/alerts", name: "alerts", component: workspacePlaceholder, meta: { title: "告警", workspace: "alerts", provider: "alertmanager" } },
  { path: "/logs", name: "logs", component: workspacePlaceholder, meta: { title: "日志", workspace: "logs", provider: "elasticsearch" } },
  { path: "/traces", name: "traces", component: workspacePlaceholder, meta: { title: "链路", workspace: "traces", provider: "tempo" } },
  { path: "/agent", name: "agent", component: workspacePlaceholder, meta: { title: "Agent", workspace: "agent", provider: "llm" } },
  { path: "/incidents", name: "incidents", component: () => import("../views/incidents/IncidentListView.vue"), meta: { title: "事件", workspace: "incidents" } },
  { path: "/incidents/:incidentId", name: "incident-detail", component: () => import("../views/incidents/IncidentDetailView.vue"), meta: { title: "事件详情", workspace: "incidents", hidden: true } },
  { path: "/devops", name: "devops", component: workspacePlaceholder, meta: { title: "DevOps", workspace: "devops", provider: "github" } },
  { path: "/settings", name: "settings", component: () => import("../views/settings/SettingsView.vue"), meta: { title: "设置", workspace: "settings" } },
  { path: "/:pathMatch(.*)*", name: "not-found", component: () => import("../pages/NotFoundPage.vue"), meta: { title: "页面不存在", hidden: true } },
];
