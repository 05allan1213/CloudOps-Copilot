import { describe, expect, it } from "vitest";

import { contextLocation } from "./contextLink";

const scopeID = "123e4567-e89b-42d3-a456-426614174000";

describe("Context Link", () => {
  it("accepts exact internal Workspace paths", () => {
    expect(contextLocation({ workspace: "incidents", path: "/incidents/123", query: { tab: "evidence" }, operational_scope_id: scopeID, external: false })).toEqual({
      path: "/incidents/123",
      query: { tab: "evidence" },
    });
  });

  it("rejects cross-Workspace and external paths", () => {
    expect(contextLocation({ workspace: "alerts", path: "/settings", query: {}, operational_scope_id: scopeID, external: false })).toBeNull();
    expect(contextLocation({ workspace: "alerts", path: "/alerts", query: {}, operational_scope_id: scopeID, external: true })).toBeNull();
  });
});
