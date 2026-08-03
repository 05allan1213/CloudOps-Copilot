import { createSSRApp } from "vue";
import { renderToString } from "@vue/server-renderer";
import ui from "@nuxt/ui/vue-plugin";
import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it } from "vitest";
import NuxtUiProductionSmoke from "./nuxt-ui-production-smoke.fixture.vue";

describe("Nuxt UI production plugin", () => {
  it("renders representative general controls through the Vue plugin", async () => {
    const app = createSSRApp(NuxtUiProductionSmoke);
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/", component: NuxtUiProductionSmoke }],
    });
    app.use(ui).use(router);

    const html = await renderToString(app);
    expect(html).toContain("确认");
    expect(html).toContain("名称");
    expect(html).toContain("CloudOps");
    expect(html).toContain("api");
  });
});
