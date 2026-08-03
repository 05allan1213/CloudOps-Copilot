import { describe, expect, it } from "vitest";

import { buildTracesRouteQuery, parseTracesRoute } from "./tracesRoute";

describe("Traces route codec", () => {
  it("keeps canonical full-detail state while dropping legacy workload output", () => {
    const parsed = parseTracesRoute({
      workload: "checkout-api",
      mode: "expert",
      status: "error",
      min_duration_ms: "12.5",
      search: "trace-search-1",
      trace_id: "trace-1",
      evidence_query: "trace-detail-execution-1",
    });
    expect(parsed.legacyWorkload).toBe("checkout-api");
    expect(parsed.minDurationMS).toBe(12.5);

    const query = buildTracesRouteQuery({ ...parsed, resource: "deployment/checkout/checkout-api" });
    expect(query).toMatchObject({
      resource: "deployment/checkout/checkout-api",
      search: "trace-search-1",
      trace_id: "trace-1",
      evidence_query: "trace-detail-execution-1",
    });
    expect(query).not.toHaveProperty("workload");
  });
});
