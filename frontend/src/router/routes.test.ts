import { describe, expect, it } from "vitest";

import { appRoutes } from "./routes";

describe("CloudOps product routes", () => {
  it("registers all ten Workspace routes and Alert and Incident deep links", () => {
    const paths = new Set(appRoutes.map((route) => route.path));
    for (const path of [
      "/overview", "/infrastructure", "/monitoring", "/alerts", "/logs",
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

  it("marks every current component route as legacy until its migration Gate", () => {
    const componentRoutes = appRoutes.filter((route) => route.component);
    expect(componentRoutes.length).toBeGreaterThan(0);
    expect(componentRoutes.every((route) => route.meta?.uiOwner === "legacy-element-plus")).toBe(true);
  });
});
