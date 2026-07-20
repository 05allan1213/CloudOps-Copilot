import { describe, expect, it } from "vitest";

import { appRoutes } from "./routes";

describe("V3 product routes", () => {
  it("registers only Incident Workbench and not-found routes", () => {
    const paths = new Set(appRoutes.map((route) => route.path));
    for (const path of ["/", "/incidents", "/incidents/:incidentId", "/:pathMatch(.*)*"]) {
      expect(paths.has(path), `${path} route missing`).toBe(true);
    }
    expect(paths.size).toBe(4);
    expect(paths.has("/login")).toBe(false);
  });

  it("makes the V3 Incident Workbench the default product entry", () => {
	const root = appRoutes.find((route) => route.path === "/");
	expect(root?.redirect).toBe("/incidents");
  });

  it("does not register removed legacy product routes", () => {
    const paths = new Set(appRoutes.map((route) => route.path));
    for (const path of ["/copilot", "/diagnosis", "/actions", "/hosts", "/k8s", "/settings/alert-rules", "/settings/channels", "/settings/users", "/audit-logs"]) {
      expect(paths.has(path), `${path} must be removed`).toBe(false);
    }
  });
});
