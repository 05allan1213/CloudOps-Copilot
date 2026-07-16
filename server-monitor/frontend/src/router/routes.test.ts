import { describe, expect, it } from "vitest";

import { appRoutes } from "./routes";

describe("frontend route compatibility", () => {
  it("keeps workbench and legacy deep links resolvable", () => {
    const paths = new Set(appRoutes.map((route) => route.path));
    for (const path of [
      "/incidents",
      "/incidents/:incidentId",
      "/alerts",
      "/alert-histories",
      "/copilot",
      "/diagnosis",
      "/diagnosis/:id",
      "/actions",
      "/actions/:id",
      "/audit-logs",
      "/k8s",
      "/k8s/workloads",
    ]) {
      expect(paths.has(path), `${path} route missing`).toBe(true);
    }
  });

  it("retains administrator protection for legacy write and audit pages", () => {
    for (const name of ["actions", "action-detail", "audit-logs"]) {
      const route = appRoutes.find((candidate) => candidate.name === name);
      expect(route?.meta?.admin, `${name} must remain admin-only`).toBe(true);
      expect(route?.meta?.legacy, `${name} must remain explicitly legacy`).toBe(true);
    }
  });
});
