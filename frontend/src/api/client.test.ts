import { AxiosHeaders, type AxiosAdapter } from "axios";
import { describe, expect, it } from "vitest";

import { postJSONWithMeta } from "./client";

describe("command response metadata", () => {
  it("captures request, trace, replay, and HTTP status headers", async () => {
    const adapter: AxiosAdapter = async (config) => ({
      data: { id: "11111111-1111-4111-8111-111111111111", command: "remediation_plan.decide", status: "accepted" },
      status: 202,
      statusText: "Accepted",
      headers: new AxiosHeaders({
        "X-Request-ID": "request-202",
        "X-Trace-ID": "trace-202",
        "Idempotent-Replay": "true",
      }),
      config,
    });

    const response = await postJSONWithMeta("/api/v3/remediation-plans/plan/decisions", { decision: "approved" }, { adapter });
    expect(response).toMatchObject({
      status: 202,
      requestID: "request-202",
      traceID: "trace-202",
      idempotentReplay: true,
    });
  });
});
