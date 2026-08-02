import { effectScope } from "vue";
import { describe, expect, it, vi } from "vitest";

import { useLatestAsync } from "./useLatestAsync";

function deferred<T>() {
  let resolve = (_value: T) => undefined;
  let reject = (_reason: unknown) => undefined;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = (value) => { resolvePromise(value); return undefined; };
    reject = (reason) => { rejectPromise(reason); return undefined; };
  });
  return { promise, resolve, reject };
}

describe("latest async lifecycle", () => {
  it("suppresses stale results and keeps the latest request", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const lifecycle = useLatestAsync<string>();
    const firstRun = lifecycle.run(() => first.promise);
    const secondRun = lifecycle.run(() => second.promise);
    second.resolve("current");
    await secondRun;
    first.resolve("stale");
    await firstRun;
    expect(lifecycle.data.value).toBe("current");
    expect(lifecycle.loading.value).toBe(false);
  });

  it("preserves loaded content when a background refresh fails", async () => {
    const lifecycle = useLatestAsync<string>();
    await lifecycle.run(async () => "loaded");
    await lifecycle.run(async () => { throw new Error("refresh failed"); }, { background: true });
    expect(lifecycle.data.value).toBe("loaded");
    expect(lifecycle.error.value).toMatchObject({ message: "refresh failed" });
    expect(lifecycle.refreshing.value).toBe(false);
  });

  it("sets pending immediately but delays visible loading feedback", async () => {
    vi.useFakeTimers();
    const lifecycle = useLatestAsync<string>();
    const pending = deferred<string>();
    const run = lifecycle.run(() => pending.promise);
    expect(lifecycle.pending.value).toBe(true);
    expect(lifecycle.loading.value).toBe(false);
    await vi.advanceTimersByTimeAsync(120);
    expect(lifecycle.loading.value).toBe(true);
    pending.resolve("ready");
    await run;
    expect(lifecycle.pending.value).toBe(false);
    expect(lifecycle.loading.value).toBe(false);
    vi.useRealTimers();
  });

  it("aborts an active request when the owning scope is disposed", () => {
    const scope = effectScope();
    const aborted = vi.fn();
    scope.run(() => {
      const lifecycle = useLatestAsync<string>();
      void lifecycle.run(({ signal }) => new Promise<string>(() => signal.addEventListener("abort", aborted)));
    });
    scope.stop();
    expect(aborted).toHaveBeenCalledOnce();
  });
});
