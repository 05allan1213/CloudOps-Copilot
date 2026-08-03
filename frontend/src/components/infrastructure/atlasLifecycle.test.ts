import { describe, expect, it, vi } from "vitest";

import { createAtlasFrameLifecycle, type AtlasFrameScheduler } from "./atlasLifecycle";

function schedulerHarness() {
  let nextHandle = 0;
  const callbacks = new Map<number, FrameRequestCallback>();
  const scheduler: AtlasFrameScheduler = {
    request(callback) {
      nextHandle += 1;
      callbacks.set(nextHandle, callback);
      return nextHandle;
    },
    cancel(handle) {
      callbacks.delete(handle);
    },
  };
  return {
    scheduler,
    callbacks,
    flush(handle: number) {
      const callback = callbacks.get(handle);
      callbacks.delete(handle);
      callback?.(0);
    },
  };
}

describe("Atlas frame lifecycle", () => {
  it("coalesces frame requests and draws once", () => {
    const harness = schedulerHarness();
    const draw = vi.fn();
    const lifecycle = createAtlasFrameLifecycle(draw, harness.scheduler);

    lifecycle.request();
    lifecycle.request();
    expect(harness.callbacks.size).toBe(1);
    harness.flush(1);
    expect(draw).toHaveBeenCalledTimes(1);
  });

  it("pauses while hidden and resumes with one fresh frame", () => {
    const harness = schedulerHarness();
    const draw = vi.fn();
    const lifecycle = createAtlasFrameLifecycle(draw, harness.scheduler);

    lifecycle.request();
    lifecycle.setVisible(false);
    expect(harness.callbacks.size).toBe(0);
    lifecycle.request();
    expect(harness.callbacks.size).toBe(0);
    lifecycle.setVisible(true);
    expect(harness.callbacks.size).toBe(1);
    harness.flush(2);
    expect(draw).toHaveBeenCalledTimes(1);
  });

  it("cancels pending work and ignores requests after dispose", () => {
    const harness = schedulerHarness();
    const draw = vi.fn();
    const lifecycle = createAtlasFrameLifecycle(draw, harness.scheduler);

    lifecycle.request();
    lifecycle.dispose();
    lifecycle.dispose();
    lifecycle.request();
    expect(harness.callbacks.size).toBe(0);
    expect(draw).not.toHaveBeenCalled();
  });
});
