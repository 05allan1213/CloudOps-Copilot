import { onBeforeUnmount, reactive, ref, watch } from "vue";
import type { LocationQueryRaw, Router } from "vue-router";

import { incidentListQueryIdentity, listIncidentsWithCache } from "../../api/incidents";
import { isApiError } from "../../api/client";
import {
  QUERY_CACHE_UPDATED_EVENT,
  queryCache,
  queryCacheKey,
  type QueryCacheEntry,
} from "../queryCache";
import {
  loadStateForStatus,
  normalizeListQuery,
  serializeListQuery,
  toIncidentListAPIQuery,
} from "../../models/incidents";
import type { CollectionResponse, IncidentListQuery, IncidentView, LoadState } from "../../types/incidents";

export interface IncidentListError {
  message: string;
  status: number | null;
  code: string;
  requestID: string;
  traceID: string;
}

const incidentListQueryKeys = [
  "status",
  "severity",
  "service",
  "attention",
  "resource",
  "alert",
  "from",
  "to",
  "limit",
  "cursor",
  "sort",
  "direction",
  "selected",
] as const satisfies readonly (keyof IncidentListQuery)[];

export function incidentListAPIIdentity(query: IncidentListQuery): string {
  return JSON.stringify(serializeListQuery(toIncidentListAPIQuery(query)));
}

export function applyIncidentListRouteQuery(target: IncidentListQuery, next: IncidentListQuery) {
  for (const key of incidentListQueryKeys) {
    Object.assign(target, { [key]: next[key] });
  }
}

export function useIncidentList(router: Router, initialQuery: Record<string, unknown>) {
  const filters = reactive<IncidentListQuery>(normalizeListQuery(initialQuery));
  const initialIdentity = incidentListQueryIdentity(filters);
  const cached = queryCache.peek<CollectionResponse<IncidentView>>(initialIdentity);
  const items = ref<IncidentView[]>(cached?.data.items ?? []);
  const nextCursor = ref(cached?.data.next_cursor ?? "");
  const state = ref<LoadState>(cached ? (cached.data.items.length > 0 ? "ready" : "empty") : "loading");
  const error = ref<IncidentListError | null>(null);
  const loading = ref(false);
  const loadingMore = ref(false);
  const lastUpdatedAt = ref(cached ? new Date(cached.updatedAt).toISOString() : "");
  const cacheSource = ref<"network" | "fresh-cache" | "stale-cache">(cached ? "fresh-cache" : "network");
  const staleReason = ref(cached?.staleReason ?? "");
  const cacheRequestIdentity = ref(cached?.requestIdentity ?? "");
  const hydratedFromCache = Boolean(cached);
  let requestIdentity = 0;
  let controller: AbortController | null = null;
  let activeCacheKey = queryCacheKey(initialIdentity);

  function applyCacheEntry(entry: QueryCacheEntry<CollectionResponse<IncidentView>>, source = cacheSource.value) {
    items.value = entry.data.items;
    nextCursor.value = entry.data.next_cursor ?? "";
    state.value = items.value.length === 0 ? "empty" : "ready";
    lastUpdatedAt.value = new Date(entry.updatedAt).toISOString();
    cacheSource.value = source;
    staleReason.value = entry.staleReason;
    cacheRequestIdentity.value = entry.requestIdentity;
  }

  function receiveCacheUpdate(event: Event) {
    const detail = (event as CustomEvent<{ key?: string }>).detail;
    if (!detail?.key || detail.key !== activeCacheKey || loadingMore.value) return;
    const entry = queryCache.peek<CollectionResponse<IncidentView>>(incidentListQueryIdentity(filters));
    if (entry) applyCacheEntry(entry, "network");
  }

  if (typeof window !== "undefined") window.addEventListener(QUERY_CACHE_UPDATED_EVENT, receiveCacheUpdate);

  async function load(append = false, force = false) {
    const identity = ++requestIdentity;
    controller?.abort();
    controller = new AbortController();
    loading.value = true;
    loadingMore.value = append;
    if (!append && items.value.length === 0) state.value = "loading";
    error.value = null;
    try {
      const query = toIncidentListAPIQuery(filters);
      const requestedCursor = append ? nextCursor.value : (query.cursor ?? "");
      if (append && requestedCursor) query.cursor = requestedCursor;
      const result = await listIncidentsWithCache(query, controller.signal, force);
      if (identity !== requestIdentity) return;
      if (append) {
        items.value = [...items.value, ...result.data.items];
        nextCursor.value = result.data.next_cursor ?? "";
      } else {
        activeCacheKey = result.entry.key;
        applyCacheEntry(result.entry, result.source);
      }
      if (append) {
        filters.cursor = requestedCursor || undefined;
        await router.replace({ query: serializeListQuery(filters) as LocationQueryRaw });
      }
      state.value = items.value.length === 0 ? "empty" : "ready";
      if (append) lastUpdatedAt.value = new Date(result.entry.updatedAt).toISOString();
    } catch (cause) {
      if (identity !== requestIdentity || controller.signal.aborted) return;
      const apiError = isApiError(cause) ? cause : null;
      error.value = {
        message: cause instanceof Error ? cause.message : "Failed to load incidents",
        status: apiError?.status ?? null,
        code: apiError?.code ?? "",
        requestID: apiError?.requestID ?? "",
        traceID: apiError?.traceID ?? "",
      };
      state.value = items.value.length > 0
        ? "ready"
        : loadStateForStatus(apiError?.status ?? null);
    } finally {
      if (identity === requestIdentity) {
        loading.value = false;
        loadingMore.value = false;
      }
    }
  }

  async function syncURLAndLoad() {
    filters.service = filters.service?.trim() || undefined;
    filters.resource = filters.resource?.trim() || undefined;
    filters.alert = filters.alert?.trim() || undefined;
    filters.cursor = undefined;
    items.value = [];
    nextCursor.value = "";
    lastUpdatedAt.value = "";
    state.value = "loading";
    await router.replace({ query: serializeListQuery(filters) as LocationQueryRaw });
    await load(false);
  }

  function updatePresentation(sort: IncidentListQuery["sort"], direction: IncidentListQuery["direction"]) {
    filters.sort = sort;
    filters.direction = direction;
    return router.replace({ query: serializeListQuery(filters) as LocationQueryRaw });
  }

  watch(
    () => router.currentRoute.value.query,
    (query) => {
      const next = normalizeListQuery(query);
      const apiChanged = incidentListAPIIdentity(filters) !== incidentListAPIIdentity(next);
      applyIncidentListRouteQuery(filters, next);
      if (apiChanged) void load(false);
    },
    { deep: true },
  );

  function reset() {
    Object.assign(filters, {
      status: undefined,
      severity: undefined,
      service: undefined,
      attention: undefined,
      resource: undefined,
      alert: undefined,
      from: undefined,
      to: undefined,
      limit: 50,
      cursor: undefined,
    });
    return syncURLAndLoad();
  }

  onBeforeUnmount(() => {
    controller?.abort();
    if (typeof window !== "undefined") window.removeEventListener(QUERY_CACHE_UPDATED_EVENT, receiveCacheUpdate);
  });

  return {
    filters,
    items,
    nextCursor,
    state,
    error,
    loading,
    loadingMore,
    lastUpdatedAt,
    cacheSource,
    staleReason,
    cacheRequestIdentity,
    hydratedFromCache,
    load,
    refresh: () => load(false, true),
    loadMore: () => load(true),
    syncURLAndLoad,
    updatePresentation,
    reset,
  };
}
