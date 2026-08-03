import { describe, expect, it } from "vitest";

import { agentNavigation, navigationGroups, primaryNavigation, workspacePaths } from "./navigation";

describe("primary navigation", () => {
  it("keeps nine ordinary entries grouped and pins Agent outside the groups", () => {
    expect(navigationGroups.map((group) => group.id)).toEqual(["observe", "operate", "system"]);
    expect(primaryNavigation).toHaveLength(9);
    expect(primaryNavigation.map((item) => item.index)).not.toContain("/agent");
    expect(agentNavigation.index).toBe("/agent");
  });

  it("retains all ten Workspace paths without mobile navigation exports", () => {
    expect(new Set(workspacePaths)).toEqual(new Set([
      "/overview", "/infrastructure", "/monitoring", "/alerts", "/logs",
      "/traces", "/agent", "/incidents", "/devops", "/settings",
    ]));
    expect(workspacePaths).toHaveLength(10);
  });

  it("uses only the Lucide Iconify namespace", () => {
    expect([...primaryNavigation, agentNavigation].every((item) => item.icon.startsWith("i-lucide-"))).toBe(true);
  });
});
