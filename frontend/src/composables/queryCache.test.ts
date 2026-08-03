import { describe, expect, it, vi } from "vitest";

import {
  QueryCache,
  queryCacheKey,
  stableQueryIdentity,
  type QueryCacheIdentity,
} from "./queryCache";

const identity = (queryIdentity: string, scope = "scope-a"): QueryCacheIdentity => ({
  userIdentity: "local-owner",
  operationalScope: scope,
  domain: "incidents",
  queryIdentity,
  contractVersion: "v1",
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => { resolve = next; });
  return { promise, resolve };
}

describe("typed query cache", () => {
  it("builds stable, scope-isolated query identities", () => {
    expect(stableQueryIdentity({ status: "open", limit: 50 }))
      .toBe(stableQueryIdentity({ limit: 50, status: "open", omitted: undefined }));
    expect(queryCacheKey(identity("list"))).not.toBe(queryCacheKey(identity("list", "scope-b")));
  });

  it("deduplicates concurrent reads and returns a fresh cache hit", async () => {
    const cache = new QueryCache(8, () => 1_000);
    const pending = deferred<{ items: string[] }>();
    const loader = vi.fn(() => pending.promise);
    const first = cache.load(identity("list"), "operational", loader);
    const second = cache.load(identity("list"), "operational", loader);
    pending.resolve({ items: ["incident-1"] });

    await expect(first).resolves.toMatchObject({ source: "network" });
    await expect(second).resolves.toMatchObject({ source: "network" });
    await expect(cache.load(identity("list"), "operational", loader))
      .resolves.toMatchObject({ source: "fresh-cache", data: { items: ["incident-1"] } });
    expect(loader).toHaveBeenCalledOnce();
  });

  it("bypasses a fresh entry for an explicit refresh", async () => {
    const cache = new QueryCache(8, () => 1_000);
    const loader = vi.fn()
      .mockResolvedValueOnce({ version: 1 })
      .mockResolvedValueOnce({ version: 2 });
    await cache.load(identity("detail"), "operational", loader);

    await expect(cache.load(identity("detail"), "operational", loader, { force: true }))
      .resolves.toMatchObject({ source: "network", data: { version: 2 } });
    expect(loader).toHaveBeenCalledTimes(2);
  });

  it("serves labelled stale content while one background revalidation runs", async () => {
    let now = 10_000;
    const cache = new QueryCache(8, () => now);
    await cache.load(identity("list"), "realtime", async () => ({ version: 1 }));
    now += 5_001;
    const pending = deferred<{ version: number }>();
    const loader = vi.fn(() => pending.promise);

    const stale = await cache.load(identity("list"), "realtime", loader, { staleWhileRevalidate: true });
    expect(stale).toMatchObject({ source: "stale-cache", revalidating: true, data: { version: 1 } });
    expect(stale.entry.staleReason).toBe("ttl");
    expect(loader).toHaveBeenCalledOnce();
    pending.resolve({ version: 2 });
    await vi.waitFor(() => expect(cache.peek<{ version: number }>(identity("list"))?.data.version).toBe(2));
  });

  it("aborts obsolete work after its last consumer leaves", async () => {
    const cache = new QueryCache();
    const consumer = new AbortController();
    const loaderAborted = vi.fn();
    const result = cache.load(identity("detail"), "operational", (signal) => new Promise((_resolve, reject) => {
      signal.addEventListener("abort", () => {
        loaderAborted();
        reject(new DOMException("aborted", "AbortError"));
      });
    }), { signal: consumer.signal });
    consumer.abort();
    await expect(result).rejects.toMatchObject({ name: "AbortError" });
    await vi.waitFor(() => expect(loaderAborted).toHaveBeenCalledOnce());
  });

  it("starts a new flight when a replacement consumer arrives before an aborted loader settles", async () => {
    const cache = new QueryCache();
    const consumer = new AbortController();
    const releaseAbort = deferred<void>();
    const loader = vi.fn((signal: AbortSignal) => {
      if (loader.mock.calls.length > 1) return Promise.resolve({ version: 2 });
      return new Promise<{ version: number }>((_resolve, reject) => {
        signal.addEventListener("abort", () => {
          void releaseAbort.promise.then(() => reject(new DOMException("aborted", "AbortError")));
        }, { once: true });
      });
    });

    const obsolete = cache.load(identity("detail"), "operational", loader, { signal: consumer.signal });
    consumer.abort();
    const replacement = cache.load(identity("detail"), "operational", loader);
    releaseAbort.resolve();

    await expect(obsolete).rejects.toMatchObject({ name: "AbortError" });
    await expect(replacement).resolves.toMatchObject({ source: "network", data: { version: 2 } });
    expect(loader).toHaveBeenCalledTimes(2);
  });

  it("evicts least-recently-used entries but retains pinned work", async () => {
    let now = 0;
    const cache = new QueryCache(2, () => ++now);
    await cache.load(identity("pinned"), "history", async () => 1, { pinned: true });
    await cache.load(identity("second"), "history", async () => 2);
    await cache.load(identity("third"), "history", async () => 3);
    expect(cache.peek(identity("pinned"))?.data).toBe(1);
    expect(cache.peek(identity("second"))).toBeUndefined();
    expect(cache.peek(identity("third"))?.data).toBe(3);
  });

  it("invalidates only the requested operational scope", async () => {
    const cache = new QueryCache();
    await cache.load(identity("list", "scope-a"), "operational", async () => "a");
    await cache.load(identity("list", "scope-b"), "operational", async () => "b");
    cache.invalidate((entry) => entry.identity.operationalScope === "scope-a", "scope-changed");
    expect(cache.peek(identity("list", "scope-a"))?.staleReason).toBe("scope-changed");
    expect(cache.peek(identity("list", "scope-b"))?.staleReason).toBe("");
  });

  it("accepts a real projected update with explicit request metadata", () => {
    const cache = new QueryCache(8, () => 20_000);
    const entry = cache.put(identity("detail"), "operational", { revision: "revision-7" }, {
      requestIdentity: "stream-event-42",
      metadata: (value) => ({ revision: value.revision }),
    });

    expect(entry).toMatchObject({
      data: { revision: "revision-7" },
      requestIdentity: "stream-event-42",
      revision: "revision-7",
      staleReason: "",
    });
    expect(cache.peek(identity("detail"))?.data).toEqual({ revision: "revision-7" });
  });

  it("projects a real response to its canonical identity without changing provenance", async () => {
    const cache = new QueryCache(8, () => 20_000);
    const sourceIdentity = identity("resources-without-cluster");
    const targetIdentity = identity("resources-cloudops-local");
    const source = (await cache.load(sourceIdentity, "operational", async () => ({ items: ["pod-a"] }))).entry;

    const projected = cache.project<{ items: string[] }>(sourceIdentity, targetIdentity);

    expect(projected).toMatchObject({
      data: { items: ["pod-a"] },
      requestIdentity: source.requestIdentity,
      updatedAt: source.updatedAt,
      freshUntil: source.freshUntil,
      staleUntil: source.staleUntil,
    });
    expect(cache.peek(targetIdentity)?.data).toEqual({ items: ["pod-a"] });
  });
});
