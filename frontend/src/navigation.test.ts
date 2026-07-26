import { describe, expect, it } from "vitest";

import { mobileMoreNavigation, mobilePrimaryNavigation, navigationGroups, primaryNavigation } from "./navigation";

describe("primary navigation", () => {
  it("exposes exactly ten grouped Workspaces", () => {
    expect(navigationGroups.map((group) => group.id)).toEqual(["observe", "operate", "system"]);
    expect(primaryNavigation).toHaveLength(10);
    expect(new Set(primaryNavigation.map((item) => item.index)).size).toBe(10);
  });

  it("keeps four stable mobile entries before More", () => {
    expect(mobilePrimaryNavigation.map((item) => item.index)).toEqual([
      "/overview", "/alerts", "/agent", "/incidents",
    ]);
    expect(mobileMoreNavigation.map((item) => item.index)).toEqual([
      "/infrastructure", "/monitoring", "/logs", "/traces", "/devops", "/settings",
    ]);
  });
});
