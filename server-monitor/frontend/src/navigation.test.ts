import { describe, expect, it } from "vitest";

import { primaryNavigation } from "./navigation";

describe("primary navigation", () => {
  it("exposes Incident Workbench as the only product entry", () => {
    expect(primaryNavigation).toEqual([
      { index: "/incidents", title: "Incident Workbench", icon: "FirstAidKit", group: "incident" },
    ]);
  });
});
