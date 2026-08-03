import { beforeEach, describe, expect, it, vi } from "vitest";

import { getJSON, postJSON } from "./client";
import {
  getLogEvidence,
  getTraceEvidence,
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
    vi.mocked(getJSON).mockReset();
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

  it("reads durable Evidence through the owning log and trace executions", async () => {
    const controller = new AbortController();
    vi.mocked(getJSON).mockResolvedValue({ items: [{ id: "evidence-1" }] });

    await expect(getLogEvidence("query/1", controller.signal)).resolves.toEqual([{ id: "evidence-1" }]);
    await expect(getTraceEvidence("query/1", controller.signal)).resolves.toEqual([{ id: "evidence-1" }]);

    expect(getJSON).toHaveBeenNthCalledWith(1, "/api/v1/logs/queries/query%2F1/evidence", { signal: controller.signal });
    expect(getJSON).toHaveBeenNthCalledWith(2, "/api/v1/traces/searches/query%2F1/evidence", { signal: controller.signal });
  });
});
