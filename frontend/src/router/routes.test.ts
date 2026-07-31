import { describe, expect, it } from "vitest";

import { appRoutes, legacyAtlasLocation, normalizeAtlasQuery } from "./routes";

describe("CloudOps product routes", () => {
  it("registers all ten Workspace routes, additive Atlas, and supported deep links", () => {
    const paths = new Set(appRoutes.map((route) => route.path));
    for (const path of [
      "/overview", "/atlas", "/infrastructure", "/monitoring", "/alerts", "/logs",
      "/traces", "/agent", "/incidents", "/devops", "/settings",
      "/alerts/:alertId", "/incidents/:incidentId",
    ]) {
      expect(paths.has(path), `${path} route missing`).toBe(true);
    }
    expect(paths.has("/login")).toBe(false);
  });

  it("makes Overview the default product entry", () => {
    expect(appRoutes.find((route) => route.path === "/")?.redirect).toBe("/overview");
  });

  it("does not register removed legacy product routes", () => {
    const paths = new Set(appRoutes.map((route) => route.path));
    for (const path of ["/copilot", "/diagnosis", "/actions", "/hosts", "/k8s", "/settings/users", "/audit-logs"]) {
      expect(paths.has(path), `${path} must be removed`).toBe(false);
    }
  });

  it("uses native Alerts, Logs, Traces, Agent and DevOps Workspace components", () => {
    const agent = appRoutes.find((route) => route.path === "/agent");
    expect(agent?.meta?.fullBleed).toBe(true);
    const devops = appRoutes.find((route) => route.path === "/devops");
    expect(devops?.component).not.toBe(appRoutes.find((route) => route.path === "/settings")?.component);
    expect(devops?.meta?.fullBleed).not.toBe(true);
  });

  it("marks only Gate 4 routes as Nuxt UI owned", () => {
    const owners = Object.fromEntries(appRoutes
      .filter((route) => route.component)
      .map((route) => [route.path, route.meta?.uiOwner]));
    expect(owners["/overview"]).toBe("nuxt-ui");
    expect(owners["/atlas"]).toBe("nuxt-ui");
    expect(owners["/infrastructure"]).toBe("nuxt-ui");
    expect(owners["/:pathMatch(.*)*"]).toBe("nuxt-ui");
    expect(owners["/monitoring"]).toBe("legacy-element-plus");
  });

  it("keeps Atlas hidden from the primary Workspace navigation", () => {
    const atlas = appRoutes.find((route) => route.path === "/atlas");
    expect(atlas?.meta?.hidden).toBe(true);
    expect(atlas?.meta?.fullBleed).toBe(true);
  });
});

describe("legacy Atlas Query compatibility", () => {
  it("replaces legacy canvas and resource links with canonical Atlas locations", () => {
    expect(legacyAtlasLocation({
      view: "canvas",
      resource: "Pod/default/api-0",
      from: "2026-07-31T00:00:00Z",
    })).toEqual({
      name: "atlas",
      query: {
        resource: "Pod/default/api-0",
        from: "2026-07-31T00:00:00Z",
      },
      replace: true,
    });
  });

  it("retains structured mode while removing legacy view aliases", () => {
    expect(legacyAtlasLocation({ view: "structured", resource: "Service/default/api" })).toEqual({
      name: "atlas",
      query: { view: "structured", resource: "Service/default/api" },
      replace: true,
    });
    expect(normalizeAtlasQuery({ view: "atlas", resource: "Node/worker-1" })).toEqual({
      resource: "Node/worker-1",
    });
  });

  it("leaves ordinary Overview queries in place", () => {
    expect(legacyAtlasLocation({ range: "1h" })).toBeUndefined();
  });
});
