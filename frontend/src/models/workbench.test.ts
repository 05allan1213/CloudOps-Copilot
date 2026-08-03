import { describe, expect, it } from "vitest";

import { formatDurationMS, formatJSON, safeExternalURL } from "./workbench";

describe("typed Workbench presentation helpers", () => {
  it("keeps absent and explicit null projections distinct", () => {
    expect(formatJSON(undefined)).toBe("未投影");
    expect(formatJSON(null)).toBe("不适用");
    expect(formatJSON({ status: "passed" })).toContain('"status": "passed"');
  });

  it("formats measured durations without inventing precision", () => {
    expect(formatDurationMS(250)).toBe("250 ms");
    expect(formatDurationMS(1_500)).toBe("1.5 s");
    expect(formatDurationMS(90_000)).toBe("1.5 min");
  });

  it("only permits HTTP deep links", () => {
    expect(safeExternalURL("https://github.com/example/repo/pull/1")).toContain("https://github.com/");
    expect(safeExternalURL("http://grafana.example.test/d/cloudops")).toContain("http://grafana.example.test/");
    expect(safeExternalURL("javascript:alert(1)")).toBe("");
    expect(safeExternalURL("data:text/html,unsafe")).toBe("");
    expect(safeExternalURL("not a url")).toBe("");
  });
});
