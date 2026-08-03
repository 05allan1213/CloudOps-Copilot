import { AxiosError, AxiosHeaders, type AxiosAdapter } from "axios";
import { describe, expect, it } from "vitest";

import { apiErrorDetails, getJSON, postJSONWithMeta } from "./client";

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

    const response = await postJSONWithMeta("/api/v1/remediation-plans/plan/decisions", { decision: "approved" }, { adapter });
    expect(response).toMatchObject({
      status: 202,
      requestID: "request-202",
      traceID: "trace-202",
      idempotentReplay: true,
    });
  });

  it("preserves typed problem identity and next steps", async () => {
    const adapter: AxiosAdapter = async (config) => {
      throw new AxiosError(
        "Forbidden",
        AxiosError.ERR_BAD_REQUEST,
        config,
        {},
        {
          data: {
            type: "https://cloudops.local/problems/forbidden",
            title: "Forbidden",
            status: 403,
            detail: "Scope evidence.read is required",
            instance: "/api/v1/evidence",
            code: "EVIDENCE_PERMISSION_DENIED",
            request_id: "req-403",
            trace_id: "trace-403",
            next_steps: ["请求 Scope 限定的只读权限"],
          },
          status: 403,
          statusText: "Forbidden",
          headers: new AxiosHeaders({ "Idempotent-Replay": "true" }),
          config,
        },
      );
    };

    let captured: unknown;
    try {
      await getJSON("/api/v1/evidence", { adapter });
    } catch (error) {
      captured = error;
    }
    expect(apiErrorDetails(captured, "fallback")).toEqual({
      message: "Scope evidence.read is required",
      status: 403,
      code: "EVIDENCE_PERMISSION_DENIED",
      requestID: "req-403",
      traceID: "trace-403",
      idempotentReplay: true,
      nextSteps: ["请求 Scope 限定的只读权限"],
    });
  });

  it("does not invent replay identity for a non-API error", () => {
    expect(apiErrorDetails(new Error("browser offline"), "fallback")).toEqual({
      message: "browser offline",
      status: null,
      code: "REQUEST_FAILED",
      requestID: "",
      traceID: "",
      idempotentReplay: null,
      nextSteps: [],
    });
  });
});
