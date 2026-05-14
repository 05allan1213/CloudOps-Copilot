import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "../stores/auth";

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
      meta: { title: "Copilot", icon: "ChatDotRound", group: "ai" },
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

  return true;
});
