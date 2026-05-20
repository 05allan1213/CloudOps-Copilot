import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "../stores/auth";
import { useMonitorStore } from "../stores/monitor";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/login",
      name: "login",
      component: () => import("../pages/LoginPage.vue"),
      meta: { public: true, title: "登录", hidden: true },
    },
    {
      path: "/",
      name: "overview",
      component: () => import("../pages/OverviewPage.vue"),
      meta: { title: "总览", icon: "Monitor", group: "monitor" },
    },
    {
      path: "/hosts",
      name: "hosts",
      component: () => import("../pages/HostsPage.vue"),
      meta: { title: "主机", icon: "Monitor", group: "monitor" },
    },
    {
      path: "/hosts/:instance",
      name: "host-detail",
      component: () => import("../pages/HostDetailPage.vue"),
      props: true,
      meta: { title: "主机详情", hidden: true },
    },
    {
      path: "/status",
      name: "status",
      component: () => import("../pages/StatusPage.vue"),
      meta: { title: "状态", icon: "CircleCheck", group: "monitor" },
    },
    {
      path: "/alerts",
      name: "alerts",
      component: () => import("../pages/AlertsPage.vue"),
      meta: { title: "当前告警", icon: "Bell", group: "alert" },
    },
    {
      path: "/alert-histories",
      name: "alert-histories",
      component: () => import("../pages/AlertHistoriesPage.vue"),
      meta: { title: "历史告警", icon: "Clock", group: "alert" },
    },
    {
      path: "/copilot",
      name: "copilot",
      component: () => import("../pages/CopilotPage.vue"),
      meta: { title: "Copilot", icon: "ChatDotRound", group: "ai", fullBleed: true },
    },
    {
      path: "/diagnosis",
      name: "diagnosis",
      component: () => import("../pages/DiagnosisListPage.vue"),
      meta: { title: "诊断", icon: "FirstAidKit", group: "ai" },
    },
    {
      path: "/diagnosis/:id",
      name: "diagnosis-detail",
      component: () => import("../pages/DiagnosisDetailPage.vue"),
      props: true,
      meta: { title: "诊断详情", hidden: true },
    },
    {
      path: "/actions",
      name: "actions",
      component: () => import("../pages/ActionsPage.vue"),
      meta: { admin: true, title: "动作", icon: "Operation", group: "admin" },
    },
    {
      path: "/actions/:id",
      name: "action-detail",
      component: () => import("../pages/ActionDetailPage.vue"),
      props: true,
      meta: { admin: true, title: "动作详情", hidden: true },
    },
    {
      path: "/audit-logs",
      name: "audit-logs",
      component: () => import("../pages/AuditLogsPage.vue"),
      meta: { admin: true, title: "审计日志", icon: "Document", group: "admin" },
    },
    {
      path: "/settings",
      name: "settings",
      component: () => import("../pages/SettingsPage.vue"),
      meta: { admin: true, title: "设置", icon: "Setting", group: "settings" },
    },
    {
      path: "/settings/alert-rules",
      name: "settings-alert-rules",
      component: () => import("../pages/AlertRulesPage.vue"),
      meta: { admin: true, title: "告警规则", icon: "AlarmClock", group: "settings" },
    },
    {
      path: "/settings/channels",
      name: "settings-channels",
      component: () => import("../pages/ChannelsPage.vue"),
      meta: { admin: true, title: "通知渠道", icon: "Message", group: "settings" },
    },
    {
      path: "/settings/users",
      name: "settings-users",
      component: () => import("../pages/UsersPage.vue"),
      meta: { admin: true, title: "用户管理", icon: "User", group: "settings" },
    },
    {
      path: "/k8s",
      name: "k8s-overview",
      component: () => import("../pages/K8sOverviewPage.vue"),
      meta: { title: "集群概览", icon: "Grid", group: "k8s" },
    },
    {
      path: "/k8s/nodes",
      name: "k8s-nodes",
      component: () => import("../pages/K8sNodesPage.vue"),
      meta: { title: "Nodes", icon: "Monitor", group: "k8s", nodesRequired: true },
    },
    {
      path: "/k8s/nodes/:name",
      name: "k8s-node-detail",
      component: () => import("../pages/K8sNodeDetailPage.vue"),
      props: true,
      meta: { title: "Node 详情", icon: "Monitor", group: "k8s", hidden: true, nodesRequired: true },
    },
    {
      path: "/k8s/workloads",
      name: "k8s-workloads",
      component: () => import("../pages/K8sWorkloadsPage.vue"),
      meta: { title: "Workloads", icon: "Box", group: "k8s" },
    },
    {
      path: "/k8s/services",
      name: "k8s-services",
      component: () => import("../pages/K8sServicesPage.vue"),
      meta: { title: "Services", icon: "Connection", group: "k8s" },
    },
    {
      path: "/k8s/events",
      name: "k8s-events",
      component: () => import("../pages/K8sEventsPage.vue"),
      meta: { title: "Events", icon: "Bell", group: "k8s" },
    },
    {
      path: "/k8s/configmaps",
      name: "k8s-configmaps",
      component: () => import("../pages/K8sConfigMapsPage.vue"),
      meta: { title: "ConfigMaps", icon: "Document", group: "k8s" },
    },
    {
      path: "/k8s/ingresses",
      name: "k8s-ingresses",
      component: () => import("../pages/K8sIngressesPage.vue"),
      meta: { title: "Ingress", icon: "Guide", group: "k8s" },
    },
    {
      path: "/k8s/storage",
      name: "k8s-storage",
      component: () => import("../pages/K8sStoragePage.vue"),
      meta: { title: "Storage", icon: "Coin", group: "k8s" },
    },
    {
      path: "/k8s/quotas",
      name: "k8s-quotas",
      component: () => import("../pages/K8sQuotasPage.vue"),
      meta: { title: "Quotas", icon: "DataLine", group: "k8s" },
    },
    {
      path: "/k8s/hpas",
      name: "k8s-hpas",
      component: () => import("../pages/K8sHPAPage.vue"),
      meta: { title: "HPA", icon: "Odometer", group: "k8s" },
    },
    {
      path: "/k8s/topology",
      name: "k8s-topology",
      component: () => import("../pages/K8sTopologyPage.vue"),
      meta: { title: "拓扑图", icon: "Share", group: "k8s" },
    },
    {
      path: "/:pathMatch(.*)*",
      name: "not-found",
      component: () => import("../pages/NotFoundPage.vue"),
      meta: { title: "404", hidden: true },
    },
  ],
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  const isPublicRoute = Boolean(to.meta.public);

  if (isPublicRoute) {
    if (auth.isAuthenticated) {
      return { path: "/" };
    }
    return true;
  }

  if (!auth.isAuthenticated) {
    return {
      path: "/login",
      query: { redirect: to.fullPath },
    };
  }

  if (!auth.user) {
    try {
      await auth.loadCurrentUser();
    } catch {
      return {
        path: "/login",
        query: { redirect: to.fullPath },
      };
    }
  }

  if (to.meta.admin && !auth.isAdmin) {
    return { path: "/" };
  }

  if (to.meta.nodesRequired) {
    const { k8sNodesEnabled } = useMonitorStore();
    if (!k8sNodesEnabled) {
      return { path: "/k8s" };
    }
  }

  return true;
});
