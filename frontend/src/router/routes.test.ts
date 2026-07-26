import { describe, expect, it } from "vitest";

import { appRoutes } from "./routes";

describe("CloudOps product routes", () => {
  it("registers all ten Workspace routes and Incident deep links", () => {
    const paths = new Set(appRoutes.map((route) => route.path));
    for (const path of [
      "/overview", "/infrastructure", "/monitoring", "/alerts", "/logs",
      "/traces", "/agent", "/incidents", "/devops", "/settings",
      "/incidents/:incidentId",
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
});
