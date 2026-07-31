import { describe, expect, it } from "vitest";

import {
  logRawValue,
  resolveTelemetryResourceID,
  traceServiceColor,
  traceSpanRawValue,
  waterfallPosition,
} from "./telemetry";

describe("telemetry workspace projections", () => {
  it("resolves canonical and legacy workload identities", () => {
    const resources = [{ id: "deployment/checkout/checkout-api", kind: "Deployment", namespace: "checkout", name: "checkout-api" }];
    expect(resolveTelemetryResourceID(resources, resources[0].id, "", "checkout")).toBe(resources[0].id);
    expect(resolveTelemetryResourceID(resources, "", "Deployment/checkout-api", "checkout")).toBe(resources[0].id);
    expect(resolveTelemetryResourceID(resources, "", "checkout-api", "checkout")).toBe(resources[0].id);
  });

  it("clamps waterfall bars to the visible trace interval", () => {
    expect(waterfallPosition("2026-07-26T00:00:00Z", 1000, "2026-07-26T00:00:00.250Z", 500)).toEqual({
      left: 25,
      width: 50,
    });
    expect(waterfallPosition("2026-07-26T00:00:00Z", 1000, "invalid", 1)).toEqual({
      left: 0,
      width: 0.35,
    });
  });

  it("keeps complete copy values and stable service colors", () => {
    const message = "first line\nsecond line with a complete identifier 0123456789";
    expect(logRawValue(message)).toBe(message);
    const span = {
      span_id: "span-1",
      service: "checkout-api",
      name: "GET /checkout",
      start_time: "2026-07-31T00:00:00Z",
      duration_ms: 12,
      status: "ok",
      critical_path: false,
      attributes: { "http.route": "/checkout" },
      events: [],
    };
    expect(traceSpanRawValue(span)).toContain('"http.route": "/checkout"');
    expect(traceServiceColor(span.service)).toBe(traceServiceColor(span.service));
  });
});
