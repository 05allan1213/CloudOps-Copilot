import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "../stores/auth";
import { appRoutes } from "./routes";

export const router = createRouter({
  history: createWebHistory(),
  routes: appRoutes,
  scrollBehavior(to, _from, savedPosition) {
    if (savedPosition) return savedPosition;
    if (to.hash) return { el: to.hash, top: 72 };
    return { top: 0 };
  },
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

let initialNavigationComplete = false;

router.afterEach((to, from) => {
  if (!initialNavigationComplete) {
    initialNavigationComplete = true;
    return;
  }
  if (to.path === from.path) return;
  const previousHeading = document.querySelector<HTMLElement>("#incident-content h1");
  void focusRouteHeading(previousHeading);
});

async function focusRouteHeading(previousHeading: HTMLElement | null) {
  const deadline = Date.now() + 1500;
  while (Date.now() < deadline) {
    const heading = document.querySelector<HTMLElement>("#incident-content h1");
    if (heading && heading !== previousHeading) {
      heading.setAttribute("tabindex", "-1");
      heading.focus({ preventScroll: true });
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 50));
  }
}
