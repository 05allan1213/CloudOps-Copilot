import { createRouter, createWebHistory } from "vue-router";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/incidents" },
    { path: "/incidents", name: "incidents", component: () => import("./views/IncidentLab.vue"), meta: { title: "事件" } },
    { path: "/incidents/:incidentId", name: "incident-detail", component: () => import("./views/IncidentLab.vue"), meta: { title: "事件详情" } },
    { path: "/settings", name: "settings", component: () => import("./views/SettingsLab.vue"), meta: { title: "设置" } },
    { path: "/monitoring", name: "monitoring", component: () => import("./views/MonitoringLab.vue"), meta: { title: "监控" } },
    { path: "/atlas", name: "atlas", component: () => import("./views/AtlasLab.vue"), meta: { title: "Operations Atlas" } },
    { path: "/agent", name: "agent", component: () => import("./views/AgentLab.vue"), meta: { title: "Agent" } },
    { path: "/states", name: "states", component: () => import("./views/StateLab.vue"), meta: { title: "异常状态" } },
    { path: "/scale", name: "scale", component: () => import("./views/ScaleLab.vue"), meta: { title: "大数据边界" } },
    { path: "/:pathMatch(.*)*", redirect: "/incidents?compat=not-found" },
  ],
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition;
    if (to.hash) return { el: to.hash, behavior: "smooth" };
    if (to.path === from.path) return false;
    return { top: 0 };
  },
});

router.afterEach((to) => {
  document.title = `${String(to.meta.title ?? "CloudOps")} | CloudOps 前置原型`;
});
