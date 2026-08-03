import { describe, expect, it } from "vitest";

import { buildLogsRouteQuery, parseLogsRoute } from "./logsRoute";

describe("Logs route codec", () => {
  it("accepts legacy workload input and emits canonical resource state", () => {
    const parsed = parseLogsRoute({
      workload: "Deployment/checkout-api",
      mode: "expert",
      levels: "error,invalid,warn",
      tail: "1",
      wrap: "1",
      query: "log-query-1",
      selected: "log-entry-1",
    });
    expect(parsed.legacyWorkload).toBe("Deployment/checkout-api");
    expect(parsed.levels).toEqual(["error", "warn"]);

    const query = buildLogsRouteQuery({ ...parsed, resource: "deployment/checkout/checkout-api" });
    expect(query.resource).toBe("deployment/checkout/checkout-api");
    expect(query).not.toHaveProperty("workload");
    expect(query).toMatchObject({
      mode: "expert",
      tail: "1",
      wrap: "1",
      query: "log-query-1",
      selected: "log-entry-1",
    });
  });
});
