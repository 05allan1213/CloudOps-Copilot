import { describe, expect, it } from "vitest";

import { formatDuration, formatIncidentTime } from "./incidentTime";

describe("Incident UTC and duration formatting", () => {
  it("normalizes offset timestamps to the documented UTC strategy", () => {
    expect(formatIncidentTime("2026-07-15T08:00:00+08:00")).toContain("00:00:00");
    expect(formatIncidentTime(undefined)).toBe("未知");
  });

  it("formats duration boundaries without inferring lifecycle time", () => {
    expect(formatDuration(59)).toBe("59s");
    expect(formatDuration(60)).toBe("1m 0s");
    expect(formatDuration(3661)).toBe("1h 1m");
  });
});
