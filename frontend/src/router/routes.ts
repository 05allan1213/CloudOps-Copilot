import type { RouteRecordRaw } from "vue-router";

export const appRoutes: RouteRecordRaw[] = [
  { path: "/", redirect: "/incidents", meta: { title: "Incident Workbench", hidden: true } },
  { path: "/incidents", name: "incidents", component: () => import("../views/incidents/IncidentListView.vue"), meta: { title: "Incident Workbench", icon: "FirstAidKit", group: "incident" } },
  { path: "/incidents/:incidentId", name: "incident-detail", component: () => import("../views/incidents/IncidentDetailView.vue"), meta: { title: "Incident Detail", hidden: true } },
  { path: "/:pathMatch(.*)*", name: "not-found", component: () => import("../pages/NotFoundPage.vue"), meta: { title: "404", hidden: true } },
];
