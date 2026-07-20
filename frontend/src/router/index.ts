import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "../stores/auth";
import { appRoutes } from "./routes";

export const router = createRouter({
  history: createWebHistory(),
  routes: appRoutes,
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (to.name === "not-found" && auth.initialized) return true;
  try {
    await auth.loadSession();
    return true;
  } catch {
    return false;
  }
});
