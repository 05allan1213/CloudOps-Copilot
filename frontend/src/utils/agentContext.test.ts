import { describe, expect, it } from "vitest";

import { shouldStopGlobalAgent } from "./agentContext";

describe("global Agent lifecycle", () => {
  it("stays idle when the overlay is closed outside the Agent Workspace", () => {
    expect(shouldStopGlobalAgent(false, "/incidents")).toBe(true);
    expect(shouldStopGlobalAgent(false, "/overview")).toBe(true);
  });

  it("keeps the shared stream owner alive for an open overlay or the full Workspace", () => {
    expect(shouldStopGlobalAgent(true, "/incidents")).toBe(false);
    expect(shouldStopGlobalAgent(false, "/agent")).toBe(false);
  });
});
