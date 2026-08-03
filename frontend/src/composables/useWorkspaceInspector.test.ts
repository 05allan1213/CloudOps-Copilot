import { describe, expect, it } from "vitest";

import {
  queryWithInspectorSelection,
  queryWithoutInspectorSelection,
} from "./useWorkspaceInspector";

describe("Workspace Inspector Query ownership", () => {
  it("canonicalizes a legacy selection while retaining list context", () => {
    expect(queryWithInspectorSelection({
      status: "open",
      page: "4",
      incident: "legacy-id",
    }, "selected", "inc-42", ["incident"])).toEqual({
      status: "open",
      page: "4",
      selected: "inc-42",
    });
  });

  it("closes only the Inspector and retains filters, pagination and time", () => {
    expect(queryWithoutInspectorSelection({
      selected: "inc-42",
      incident: "legacy-id",
      status: "open",
      page: "4",
      from: "2026-07-31T00:00:00Z",
      to: "2026-07-31T01:00:00Z",
    }, "selected", ["incident"])).toEqual({
      status: "open",
      page: "4",
      from: "2026-07-31T00:00:00Z",
      to: "2026-07-31T01:00:00Z",
    });
  });
});
