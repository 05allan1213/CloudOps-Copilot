import { describe, expect, it } from "vitest";

import { virtualWindow, waterfallPosition } from "./telemetry";

describe("telemetry workspace projections", () => {
  it("renders only a bounded virtual row window", () => {
    expect(virtualWindow(1000, 2000, 500, 50, 4)).toEqual({
      start: 36,
      end: 54,
      offset: 1800,
      totalHeight: 50000,
    });
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
});
