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

  it("uses native Alerts, Logs, Traces and Agent Workspace components", () => {
    const placeholder = appRoutes.find((route) => route.path === "/devops")?.component;
    expect(appRoutes.find((route) => route.path === "/alerts")?.component).not.toBe(placeholder);
    expect(appRoutes.find((route) => route.path === "/alerts/:alertId")?.component).not.toBe(placeholder);
    expect(appRoutes.find((route) => route.path === "/logs")?.component).not.toBe(placeholder);
    expect(appRoutes.find((route) => route.path === "/traces")?.component).not.toBe(placeholder);
    const agent = appRoutes.find((route) => route.path === "/agent");
    expect(agent?.component).not.toBe(placeholder);
    expect(agent?.meta?.fullBleed).toBe(true);
  });
});
