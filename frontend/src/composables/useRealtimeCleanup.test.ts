import { effectScope } from "vue";
import { describe, expect, it, vi } from "vitest";

import { useRealtimeCleanup } from "./useRealtimeCleanup";

describe("Realtime cleanup ownership", () => {
  it("disposes every registered connection exactly once", () => {
    const first = vi.fn();
    const second = vi.fn();
    const cleanup = useRealtimeCleanup();
    cleanup.register(first);
    cleanup.register(second);
    cleanup.dispose();
    cleanup.dispose();
    expect(first).toHaveBeenCalledOnce();
    expect(second).toHaveBeenCalledOnce();
  });

  it("tears down registered resources with the component scope", () => {
    const disposer = vi.fn();
    const scope = effectScope();
    scope.run(() => useRealtimeCleanup().register(disposer));
    scope.stop();
    expect(disposer).toHaveBeenCalledOnce();
  });

  it("attempts every disposer before surfacing cleanup failures", () => {
    const failure = new Error("close failed");
    const second = vi.fn();
    const cleanup = useRealtimeCleanup();
    cleanup.register(() => { throw failure; });
    cleanup.register(second);
    expect(() => cleanup.dispose()).toThrow(failure);
    expect(second).toHaveBeenCalledOnce();
  });
});
