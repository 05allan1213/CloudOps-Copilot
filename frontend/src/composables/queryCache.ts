export const QUERY_CACHE_UPDATED_EVENT = "cloudops:query-cache-updated";

export type QueryCachePolicyName =
  | "realtime"
  | "telemetry"
  | "operational"
  | "metadata"
  | "history";

export interface QueryCachePolicy {
  freshMs: number;
  maxStaleMs: number;
}

export const queryCachePolicies: Readonly<Record<QueryCachePolicyName, QueryCachePolicy>> = {
  realtime: { freshMs: 5_000, maxStaleMs: 30_000 },
  telemetry: { freshMs: 10_000, maxStaleMs: 120_000 },
  operational: { freshMs: 30_000, maxStaleMs: 5 * 60_000 },
  metadata: { freshMs: 2 * 60_000, maxStaleMs: 15 * 60_000 },
  history: { freshMs: 5 * 60_000, maxStaleMs: 60 * 60_000 },
};

export interface QueryCacheIdentity {
  userIdentity: string;
  operationalScope: string;
  domain: string;
  queryIdentity: string;
  contractVersion: string;
}

export type QueryCacheStaleReason =
  | "ttl"
  | "manual"
  | "mutation"
  | "scope-changed"
  | "realtime-disconnected";

export interface QueryCacheEntry<T> {
  key: string;
  identity: QueryCacheIdentity;
  policy: QueryCachePolicyName;
  data: T;
  updatedAt: number;
  freshUntil: number;
  staleUntil: number;
  staleReason: QueryCacheStaleReason | "";
  requestIdentity: string;
  revision?: string;
  cursor?: string;
  pinned: boolean;
  lastAccessedAt: number;
}

export interface QueryCacheLoadOptions<T> {
  signal?: AbortSignal;
  force?: boolean;
  staleWhileRevalidate?: boolean;
  pinned?: boolean;
  metadata?: (value: T) => { revision?: string; cursor?: string };
}

export interface QueryCachePutOptions<T> {
  pinned?: boolean;
  staleReason?: QueryCacheStaleReason | "";
  requestIdentity?: string;
  metadata?: (value: T) => { revision?: string; cursor?: string };
}

export interface QueryCacheContext {
  userIdentity: string;
  operationalScope: string;
  contractVersion: string;
}

export interface QueryCacheLoadResult<T> {
  data: T;
  entry: QueryCacheEntry<T>;
  source: "network" | "fresh-cache" | "stale-cache";
  revalidating: boolean;
}

interface InFlightQuery<T> {
  controller: AbortController;
  consumers: Set<symbol>;
  keepAlive: boolean;
  promise: Promise<QueryCacheEntry<T>>;
}

function stableValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stableValue);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .filter(([, item]) => item !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, item]) => [key, stableValue(item)]),
  );
}

export function stableQueryIdentity(value: unknown): string {
  return JSON.stringify(stableValue(value));
}

export function queryCacheKey(identity: QueryCacheIdentity): string {
  return [
    identity.contractVersion,
    identity.userIdentity,
    identity.operationalScope,
    identity.domain,
    identity.queryIdentity,
  ].map((value) => `${value.length}:${value}`).join("|");
}

function abortError(): DOMException {
  return new DOMException("The request was aborted", "AbortError");
}

function dispatchCacheUpdate(entry: QueryCacheEntry<unknown>, source: "network" | "invalidation") {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(QUERY_CACHE_UPDATED_EVENT, {
    detail: {
      key: entry.key,
      domain: entry.identity.domain,
      operationalScope: entry.identity.operationalScope,
      updatedAt: entry.updatedAt,
      source,
    },
  }));
}

export class QueryCache {
  private readonly entries = new Map<string, QueryCacheEntry<unknown>>();
  private readonly inFlight = new Map<string, InFlightQuery<unknown>>();
  private requestSequence = 0;

  constructor(
    private readonly maximumEntries = 96,
    private readonly now: () => number = Date.now,
  ) {}

  get size(): number {
    return this.entries.size;
  }

  peek<T>(identity: QueryCacheIdentity): QueryCacheEntry<T> | undefined {
    const entry = this.entries.get(queryCacheKey(identity)) as QueryCacheEntry<T> | undefined;
    if (!entry) return undefined;
    entry.lastAccessedAt = this.now();
    if (entry.freshUntil <= entry.lastAccessedAt && !entry.staleReason) entry.staleReason = "ttl";
    return entry;
  }

  isFresh(entry: QueryCacheEntry<unknown>): boolean {
    return entry.freshUntil > this.now() && !entry.staleReason;
  }

  isUsableStale(entry: QueryCacheEntry<unknown>): boolean {
    return entry.staleUntil > this.now();
  }

  put<T>(
    identity: QueryCacheIdentity,
    policyName: QueryCachePolicyName,
    data: T,
    options: QueryCachePutOptions<T> = {},
  ): QueryCacheEntry<T> {
    const updatedAt = this.now();
    const policy = queryCachePolicies[policyName];
    const metadata = options.metadata?.(data) ?? {};
    const entry: QueryCacheEntry<T> = {
      key: queryCacheKey(identity),
      identity: { ...identity },
      policy: policyName,
      data,
      updatedAt,
      freshUntil: options.staleReason ? 0 : updatedAt + policy.freshMs,
      staleUntil: updatedAt + policy.maxStaleMs,
      staleReason: options.staleReason ?? "",
      requestIdentity: options.requestIdentity ?? `projection-${++this.requestSequence}`,
      revision: metadata.revision,
      cursor: metadata.cursor,
      pinned: options.pinned === true,
      lastAccessedAt: updatedAt,
    };
    this.entries.set(entry.key, entry as QueryCacheEntry<unknown>);
    this.evict();
    dispatchCacheUpdate(entry as QueryCacheEntry<unknown>, "network");
    return entry;
  }

  project<T>(
    sourceIdentity: QueryCacheIdentity,
    targetIdentity: QueryCacheIdentity,
  ): QueryCacheEntry<T> | undefined {
    const source = this.peek<T>(sourceIdentity);
    if (!source) return undefined;
    const targetKey = queryCacheKey(targetIdentity);
    if (targetKey === source.key) return source;
    const existing = this.entries.get(targetKey) as QueryCacheEntry<T> | undefined;
    if (existing && existing.updatedAt >= source.updatedAt) return existing;
    const projected: QueryCacheEntry<T> = {
      ...source,
      key: targetKey,
      identity: { ...targetIdentity },
      lastAccessedAt: this.now(),
    };
    this.entries.set(targetKey, projected as QueryCacheEntry<unknown>);
    this.evict();
    dispatchCacheUpdate(projected as QueryCacheEntry<unknown>, "network");
    return projected;
  }

  hasStaleDomain(domain: string): boolean {
    for (const entry of this.entries.values()) {
      if (entry.identity.domain === domain && !this.isFresh(entry)) return true;
    }
    return false;
  }

  async load<T>(
    identity: QueryCacheIdentity,
    policyName: QueryCachePolicyName,
    loader: (signal: AbortSignal) => Promise<T>,
    options: QueryCacheLoadOptions<T> = {},
  ): Promise<QueryCacheLoadResult<T>> {
    if (options.signal?.aborted) throw abortError();
    const cached = this.peek<T>(identity);
    if (!options.force && cached && this.isFresh(cached)) {
      return { data: cached.data, entry: cached, source: "fresh-cache", revalidating: false };
    }
    if (!options.force && cached && this.isUsableStale(cached) && options.staleWhileRevalidate) {
      void this.fetch(identity, policyName, loader, { ...options, signal: undefined }, true).catch(() => undefined);
      return { data: cached.data, entry: cached, source: "stale-cache", revalidating: true };
    }
    const entry = await this.fetch(identity, policyName, loader, options, false);
    return { data: entry.data, entry, source: "network", revalidating: false };
  }

  invalidate(
    predicate: (entry: QueryCacheEntry<unknown>) => boolean,
    reason: QueryCacheStaleReason = "manual",
  ): number {
    let count = 0;
    for (const entry of this.entries.values()) {
      if (!predicate(entry)) continue;
      entry.freshUntil = 0;
      entry.staleReason = reason;
      count += 1;
      dispatchCacheUpdate(entry, "invalidation");
    }
    return count;
  }

  remove(predicate: (entry: QueryCacheEntry<unknown>) => boolean): number {
    let count = 0;
    for (const [key, entry] of this.entries) {
      if (!predicate(entry)) continue;
      this.entries.delete(key);
      this.abortFlight(key);
      count += 1;
    }
    return count;
  }

  clear(reason: QueryCacheStaleReason = "manual") {
    for (const entry of this.entries.values()) {
      entry.staleReason = reason;
      dispatchCacheUpdate(entry, "invalidation");
    }
    this.entries.clear();
    for (const key of this.inFlight.keys()) this.abortFlight(key);
  }

  private fetch<T>(
    identity: QueryCacheIdentity,
    policyName: QueryCachePolicyName,
    loader: (signal: AbortSignal) => Promise<T>,
    options: QueryCacheLoadOptions<T>,
    keepAlive: boolean,
  ): Promise<QueryCacheEntry<T>> {
    const key = queryCacheKey(identity);
    let flight = this.inFlight.get(key) as InFlightQuery<T> | undefined;
    if (flight?.controller.signal.aborted) {
      if (this.inFlight.get(key)?.promise === flight.promise) this.inFlight.delete(key);
      flight = undefined;
    }
    if (!flight) {
      const controller = new AbortController();
      const requestIdentity = `read-${++this.requestSequence}`;
      const promise = loader(controller.signal).then((data) => {
        return this.put(identity, policyName, data, {
          pinned: options.pinned,
          requestIdentity,
          metadata: options.metadata,
        });
      }).finally(() => {
        const active = this.inFlight.get(key);
        if (active?.promise === promise) this.inFlight.delete(key);
      });
      flight = { controller, consumers: new Set(), keepAlive, promise };
      this.inFlight.set(key, flight as InFlightQuery<unknown>);
    } else if (keepAlive) {
      flight.keepAlive = true;
    }
    return this.subscribe(flight, options.signal);
  }

  private subscribe<T>(flight: InFlightQuery<T>, signal?: AbortSignal): Promise<QueryCacheEntry<T>> {
    if (signal?.aborted) return Promise.reject(abortError());
    const token = Symbol("query-consumer");
    flight.consumers.add(token);
    return new Promise<QueryCacheEntry<T>>((resolve, reject) => {
      let settled = false;
      const release = () => {
        if (settled) return;
        settled = true;
        signal?.removeEventListener("abort", onAbort);
        flight.consumers.delete(token);
        if (!flight.keepAlive && flight.consumers.size === 0) flight.controller.abort();
      };
      const onAbort = () => {
        release();
        reject(abortError());
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      flight.promise.then(
        (entry) => { release(); resolve(entry); },
        (error) => { release(); reject(error); },
      );
    });
  }

  private abortFlight(key: string) {
    const flight = this.inFlight.get(key);
    flight?.controller.abort();
    this.inFlight.delete(key);
  }

  private evict() {
    if (this.entries.size <= this.maximumEntries) return;
    const candidates = [...this.entries.values()]
      .filter((entry) => !entry.pinned)
      .sort((left, right) => left.lastAccessedAt - right.lastAccessedAt);
    while (this.entries.size > this.maximumEntries && candidates.length) {
      const entry = candidates.shift();
      if (entry) this.entries.delete(entry.key);
    }
  }
}

export const queryCache = new QueryCache();

let activeQueryCacheContext: QueryCacheContext = {
  userIdentity: "local-owner",
  operationalScope: "unscoped",
  contractVersion: "v1",
};

export function currentQueryCacheContext(): QueryCacheContext {
  return { ...activeQueryCacheContext };
}

export function queryIdentityFor(
  domain: string,
  queryIdentity: unknown,
  context: QueryCacheContext = activeQueryCacheContext,
): QueryCacheIdentity {
  return {
    ...context,
    domain,
    queryIdentity: stableQueryIdentity(queryIdentity),
  };
}

export function setQueryCacheContext(next: Partial<QueryCacheContext>): QueryCacheContext {
  const previous = activeQueryCacheContext;
  activeQueryCacheContext = {
    userIdentity: next.userIdentity?.trim() || previous.userIdentity,
    operationalScope: next.operationalScope?.trim() || previous.operationalScope,
    contractVersion: next.contractVersion?.trim() || previous.contractVersion,
  };
  if (previous.userIdentity !== activeQueryCacheContext.userIdentity) {
    queryCache.remove((entry) => entry.identity.userIdentity === previous.userIdentity);
  } else if (previous.operationalScope !== activeQueryCacheContext.operationalScope) {
    queryCache.remove((entry) => (
      entry.identity.userIdentity === previous.userIdentity
      && entry.identity.operationalScope === previous.operationalScope
    ));
  }
  return currentQueryCacheContext();
}

export function invalidateQueryDomain(
  domains: string | readonly string[],
  reason: QueryCacheStaleReason = "manual",
): number {
  const requested = new Set(typeof domains === "string" ? [domains] : domains);
  return queryCache.invalidate((entry) => requested.has(entry.identity.domain), reason);
}
