import { beforeEach, describe, expect, it } from "vitest";

import {
  consumeWorkspaceRouteEntrance,
  resetWorkspaceMotionForTests,
  WORKSPACE_ROUTE_MOTION_MS,
} from "./workspaceMotion";

describe("workspace motion coordination", () => {
  beforeEach(resetWorkspaceMotionForTests);

  it("runs the staged entrance once per route session", () => {
    expect(consumeWorkspaceRouteEntrance("/monitoring")).toBe(true);
    expect(consumeWorkspaceRouteEntrance("/monitoring")).toBe(false);
    expect(consumeWorkspaceRouteEntrance("/logs")).toBe(true);
    expect(WORKSPACE_ROUTE_MOTION_MS).toBeGreaterThanOrEqual(300);
    expect(WORKSPACE_ROUTE_MOTION_MS).toBeLessThanOrEqual(450);
  });
});
