import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "../stores/auth";
import { appRoutes } from "./routes";

export const router = createRouter({
  history: createWebHistory(),
  routes: appRoutes,
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  const isPublicRoute = Boolean(to.meta.public);

  if (isPublicRoute) {
    if (auth.isAuthenticated) {
      return { path: "/incidents" };
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

  return true;
});
