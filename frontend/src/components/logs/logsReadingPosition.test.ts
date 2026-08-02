import { beforeEach, describe, expect, it } from "vitest";

import {
  appendedLogCount,
  readLogReadingPosition,
  rememberLogReadingPosition,
  resetLogReadingPositionsForTests,
} from "./logsReadingPosition";

describe("Logs reading position", () => {
  beforeEach(resetLogReadingPositionsForTests);

  it("recognizes only a stable append as new live content", () => {
    expect(appendedLogCount([{ id: "a" }, { id: "b" }], [{ id: "a" }, { id: "b" }, { id: "c" }])).toBe(1);
    expect(appendedLogCount([{ id: "a" }], [{ id: "replacement" }, { id: "b" }])).toBe(0);
  });

  it("restores numeric scroll state without retaining log content", () => {
    rememberLogReadingPosition("query-1", 742.5);
    expect(readLogReadingPosition("query-1")).toBe(742.5);
    expect(readLogReadingPosition("unknown")).toBe(0);
  });
});
