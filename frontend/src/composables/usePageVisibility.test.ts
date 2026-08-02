import { effectScope } from "vue";
import { describe, expect, it, vi } from "vitest";

import { usePageVisibility, type VisibilitySource } from "./usePageVisibility";

class FakeVisibilitySource implements VisibilitySource {
  hidden = false;
  listener: (() => void) | undefined;
  addEventListener(_type: "visibilitychange", listener: () => void) { this.listener = listener; }
  removeEventListener(_type: "visibilitychange", listener: () => void) {
    if (this.listener === listener) this.listener = undefined;
  }
  setHidden(value: boolean) { this.hidden = value; this.listener?.(); }
}

describe("page visibility lifecycle", () => {
  it("reports hidden duration and removes its listener with the owning scope", () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000);
    const source = new FakeVisibilitySource();
    const scope = effectScope();
    const visibility = scope.run(() => usePageVisibility(source))!;
    source.setHidden(true);
    vi.setSystemTime(3_400);
    source.setHidden(false);
    expect(visibility.visible.value).toBe(true);
    expect(visibility.lastHiddenDurationMs.value).toBe(2_400);
    scope.stop();
    expect(source.listener).toBeUndefined();
    vi.useRealTimers();
  });
});
