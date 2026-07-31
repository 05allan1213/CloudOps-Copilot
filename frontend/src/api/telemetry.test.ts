import { beforeEach, describe, expect, it, vi } from "vitest";

import { postJSON } from "./client";
import {
  startLogQuery,
  startTraceSearch,
  type StartLogQueryInput,
  type StartTraceSearchInput,
} from "./telemetry";

vi.mock("./client", () => ({
  getJSON: vi.fn(),
  postJSON: vi.fn(),
}));

describe("telemetry request cancellation", () => {
  beforeEach(() => {
    vi.mocked(postJSON).mockReset();
  });

  it("forwards AbortSignal to log and trace start requests", () => {
    const controller = new AbortController();
    const logInput = {} as StartLogQueryInput;
    const traceInput = {} as StartTraceSearchInput;

    void startLogQuery(logInput, controller.signal);
    void startTraceSearch(traceInput, controller.signal);

    expect(postJSON).toHaveBeenNthCalledWith(1, "/api/v1/logs/queries", logInput, { signal: controller.signal });
    expect(postJSON).toHaveBeenNthCalledWith(2, "/api/v1/traces/searches", traceInput, { signal: controller.signal });
  });
});
