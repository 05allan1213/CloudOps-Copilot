import { describe, expect, it } from "vitest";

import { supportsIntentPrefetch } from "./routePrefetch";

describe("capability-aware route prefetch", () => {
  it("respects save-data, constrained networks, and constrained devices", () => {
    expect(supportsIntentPrefetch({ connection: { saveData: true } })).toBe(false);
    expect(supportsIntentPrefetch({ connection: { effectiveType: "2g" } })).toBe(false);
    expect(supportsIntentPrefetch({ deviceMemory: 2, hardwareConcurrency: 8 })).toBe(false);
    expect(supportsIntentPrefetch({ deviceMemory: 8, hardwareConcurrency: 2 })).toBe(false);
    expect(supportsIntentPrefetch({ connection: { effectiveType: "4g" }, deviceMemory: 8, hardwareConcurrency: 12 })).toBe(true);
  });
});
