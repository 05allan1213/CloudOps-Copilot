import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "../stores/auth";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/login",
      name: "login",
      component: () => import("../pages/LoginPage.vue"),
      meta: { public: true },
    },
    {
      path: "/",
      name: "overview",
      component: () => import("../pages/OverviewPage.vue"),
    },
    {
      path: "/hosts",
      name: "hosts",
      component: () => import("../pages/HostsPage.vue"),
    },
    {
      path: "/hosts/:instance",
      name: "host-detail",
      component: () => import("../pages/HostDetailPage.vue"),
      props: true,
    },
    {
      path: "/status",
      name: "status",
      component: () => import("../pages/StatusPage.vue"),
    },
    {
      path: "/alerts",
      name: "alerts",
      component: () => import("../pages/AlertsPage.vue"),
    },
    {
      path: "/alert-histories",
      name: "alert-histories",
      component: () => import("../pages/AlertHistoriesPage.vue"),
    },
    {
      path: "/settings",
      name: "settings",
      component: () => import("../pages/SettingsPage.vue"),
      meta: { admin: true },
    },
    {
      path: "/settings/alert-rules",
      name: "settings-alert-rules",
      component: () => import("../pages/AlertRulesPage.vue"),
      meta: { admin: true },
    },
    {
      path: "/settings/channels",
      name: "settings-channels",
      component: () => import("../pages/ChannelsPage.vue"),
      meta: { admin: true },
    },
    {
      path: "/settings/users",
      name: "settings-users",
      component: () => import("../pages/UsersPage.vue"),
      meta: { admin: true },
    },
    {
      path: "/:pathMatch(.*)*",
      redirect: "/",
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
