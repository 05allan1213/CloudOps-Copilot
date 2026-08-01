import { effectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import { COPY_FEEDBACK_DURATION_MS, useCopyFeedback } from "./useCopyFeedback";

describe("copy feedback", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("reports success and returns to idle without leaking a timer", async () => {
    vi.useFakeTimers();
    const writer = vi.fn().mockResolvedValue(undefined);
    const scope = effectScope();
    const feedback = scope.run(() => useCopyFeedback({ writer }))!;

    await expect(feedback.copy("exact-value")).resolves.toBe(true);
    expect(writer).toHaveBeenCalledWith("exact-value");
    expect(feedback.copied.value).toBe(true);

    await vi.advanceTimersByTimeAsync(COPY_FEEDBACK_DURATION_MS);
    expect(feedback.state.value).toBe("idle");
    scope.stop();
  });

  it("keeps a failed clipboard write explicit", async () => {
    const scope = effectScope();
    const feedback = scope.run(() => useCopyFeedback({
      writer: vi.fn().mockRejectedValue(new Error("denied")),
    }))!;

    await expect(feedback.copy("exact-value")).resolves.toBe(false);
    expect(feedback.failed.value).toBe(true);
    scope.stop();
  });
});
