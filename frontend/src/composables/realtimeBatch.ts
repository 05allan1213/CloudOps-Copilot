export interface RealtimeBatchOptions<T> {
  flush: (items: readonly T[]) => void;
  maximumItems?: number;
  schedule?: (callback: () => void) => number;
  cancel?: (handle: number) => void;
  compact?: (items: readonly T[]) => T;
}

export interface RealtimeBatch<T> {
  enqueue(item: T): void;
  flush(): void;
  pause(): void;
  resume(): void;
  dispose(flushPending?: boolean): void;
  pendingCount(): number;
}

const defaultSchedule = (callback: () => void): number => {
  if (typeof requestAnimationFrame === "function") return requestAnimationFrame(callback);
  return setTimeout(callback, 16) as unknown as number;
};

const defaultCancel = (handle: number) => {
  if (typeof cancelAnimationFrame === "function") cancelAnimationFrame(handle);
  else clearTimeout(handle);
};

export function createRealtimeBatch<T>(options: RealtimeBatchOptions<T>): RealtimeBatch<T> {
  const maximumItems = Math.max(2, options.maximumItems ?? 256);
  const schedule = options.schedule ?? defaultSchedule;
  const cancel = options.cancel ?? defaultCancel;
  let queue: T[] = [];
  let scheduled: number | null = null;
  let paused = false;
  let disposed = false;

  const scheduleFlush = () => {
    if (disposed || paused || scheduled !== null || queue.length === 0) return;
    scheduled = schedule(() => {
      scheduled = null;
      flush();
    });
  };

  const enqueue = (item: T) => {
    if (disposed) return;
    queue.push(item);
    if (queue.length > maximumItems && options.compact) {
      queue = [options.compact(queue)];
    } else if (queue.length > maximumItems && !paused) {
      flush();
    }
    scheduleFlush();
  };

  const flush = () => {
    if (disposed || paused || queue.length === 0) return;
    if (scheduled !== null) {
      cancel(scheduled);
      scheduled = null;
    }
    const items = queue;
    queue = [];
    options.flush(items);
  };

  const pause = () => {
    paused = true;
    if (scheduled !== null) {
      cancel(scheduled);
      scheduled = null;
    }
  };

  const resume = () => {
    if (disposed) return;
    paused = false;
    scheduleFlush();
  };

  const dispose = (flushPending = false) => {
    if (disposed) return;
    if (flushPending) {
      paused = false;
      flush();
    }
    disposed = true;
    if (scheduled !== null) cancel(scheduled);
    scheduled = null;
    queue = [];
  };

  return { enqueue, flush, pause, resume, dispose, pendingCount: () => queue.length };
}
