import { createRouter, createWebHistory } from "vue-router";

import { appRoutes } from "./routes";
import { appScrollBehavior } from "./scrollBehavior";

export const router = createRouter({
  history: createWebHistory(),
  routes: appRoutes,
  scrollBehavior: appScrollBehavior,
});

router.afterEach((to) => {
  const title = typeof to.meta.title === "string" ? to.meta.title : "CloudOps";
  document.title = `${title} | CloudOps`;
});
