import { describe, expect, it, vi } from "vitest";

import { createRealtimeBatch } from "./realtimeBatch";

describe("bounded realtime rendering", () => {
  it("batches visual work without dropping source chunks", () => {
    let scheduled: (() => void) | undefined;
    const rendered: string[] = [];
    const batch = createRealtimeBatch<string>({
      maximumItems: 3,
      schedule: (callback) => { scheduled = callback; return 1; },
      cancel: vi.fn(),
      compact: (items) => items.join(""),
      flush: (items) => rendered.push(items.join("")),
    });
    for (const chunk of ["a", "b", "c", "d", "e"]) batch.enqueue(chunk);
    expect(batch.pendingCount()).toBe(2);
    scheduled?.();
    expect(rendered).toEqual(["abcde"]);
  });

  it("keeps data queued while hidden and flushes once on resume", () => {
    const rendered: number[][] = [];
    let scheduled: (() => void) | undefined;
    const batch = createRealtimeBatch<number>({
      schedule: (callback) => { scheduled = callback; return 1; },
      cancel: vi.fn(),
      flush: (items) => rendered.push([...items]),
    });
    batch.pause();
    batch.enqueue(1);
    batch.enqueue(2);
    expect(scheduled).toBeUndefined();
    batch.resume();
    scheduled?.();
    expect(rendered).toEqual([[1, 2]]);
  });

  it("flushes an uncompacted queue at its bound without dropping items", () => {
    const rendered: number[][] = [];
    const batch = createRealtimeBatch<number>({
      maximumItems: 2,
      schedule: () => 1,
      cancel: vi.fn(),
      flush: (items) => rendered.push([...items]),
    });
    batch.enqueue(1);
    batch.enqueue(2);
    batch.enqueue(3);
    expect(rendered).toEqual([[1, 2, 3]]);
    expect(batch.pendingCount()).toBe(0);
  });
});
