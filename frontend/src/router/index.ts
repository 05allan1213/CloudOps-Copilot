import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "../stores/auth";
import { appRoutes } from "./routes";

const mainScrollPositions = new Map<string, number>();

export const router = createRouter({
  history: createWebHistory(),
  routes: appRoutes,
  scrollBehavior(to, _from, savedPosition) {
    if (savedPosition) return savedPosition;
    if (to.hash) return { el: to.hash, top: 72 };
    return { top: 0 };
  },
});

router.beforeEach(async (to, from) => {
  const main = document.querySelector<HTMLElement>("#incident-content");
  if (main && from.fullPath) mainScrollPositions.set(from.fullPath, main.scrollTop);
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
let navigationEffectIdentity = 0;

router.afterEach((to, from) => {
  const identity = ++navigationEffectIdentity;
  if (!initialNavigationComplete) {
    initialNavigationComplete = true;
    return;
  }
  if (to.path === from.path) return;
  const savedScrollTop = mainScrollPositions.get(to.fullPath);
  if (savedScrollTop !== undefined) {
    void restoreMainScroll(savedScrollTop, identity);
    return;
  }
  const previousHeading = document.querySelector<HTMLElement>("#incident-content h1");
  void focusRouteHeading(previousHeading, identity);
});

async function restoreMainScroll(scrollTop: number, identity: number) {
  await new Promise((resolve) => window.setTimeout(resolve, 350));
  if (identity !== navigationEffectIdentity) return;
  const main = document.querySelector<HTMLElement>("#incident-content");
  if (main) main.scrollTop = scrollTop;
}

async function focusRouteHeading(previousHeading: HTMLElement | null, identity: number) {
  const deadline = Date.now() + 1500;
  while (Date.now() < deadline && identity === navigationEffectIdentity) {
    const heading = document.querySelector<HTMLElement>("#incident-content h1");
    if (heading && heading !== previousHeading) {
      heading.setAttribute("tabindex", "-1");
      heading.focus({ preventScroll: true });
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 50));
  }
}
