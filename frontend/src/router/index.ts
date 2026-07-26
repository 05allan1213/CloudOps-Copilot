import { createRouter, createWebHistory } from "vue-router";

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
