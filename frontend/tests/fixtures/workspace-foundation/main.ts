import ui from "@nuxt/ui/vue-plugin";
import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";

import { initializeTheme } from "../../../src/composables/useTheme";
import "../../../src/styles/app.css";
import WorkspaceFoundationFixture from "./WorkspaceFoundationFixture.vue";

initializeTheme();

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/workspace" },
    { path: "/workspace", name: "workspace", component: WorkspaceFoundationFixture },
    { path: "/workspace/full/:id", name: "workspace-full", component: WorkspaceFoundationFixture },
    { path: "/:pathMatch(.*)*", redirect: "/workspace" },
  ],
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition;
    if (to.path === from.path) return false;
    return { top: 0 };
  },
});

createApp(WorkspaceFoundationFixture).use(router).use(ui).mount("#app");
