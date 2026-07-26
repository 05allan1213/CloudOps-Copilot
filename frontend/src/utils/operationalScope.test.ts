import { describe, expect, it } from "vitest";

import { queryForScopeChange } from "./operationalScope";

describe("Operational Scope route context", () => {
  it("clears resource-bound context and keeps unrelated navigation state", () => {
    expect(queryForScopeChange({
      cluster: "cluster-a",
      namespace: "ops",
      resource: "k8s_old",
      cursor: "opaque",
      kind: "Pod",
      search: "api",
      from: "2026-07-26T00:00:00Z",
      to: "2026-07-26T01:00:00Z",
      tab: "evidence",
    }, "cluster-b")).toEqual({ cluster: "cluster-b", tab: "evidence" });
  });
});
