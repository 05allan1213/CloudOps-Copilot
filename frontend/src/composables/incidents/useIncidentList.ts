import { onBeforeUnmount, reactive, ref } from "vue";
import type { LocationQueryRaw, Router } from "vue-router";

import { listIncidents } from "../../api/incidents";
import { isApiError } from "../../api/client";
import { loadStateForStatus, normalizeListQuery, serializeListQuery } from "../../models/incidents";
import type { IncidentListQuery, IncidentView, LoadState } from "../../types/incidents";

export interface IncidentListError {
  message: string;
  status: number | null;
  code: string;
  requestID: string;
  traceID: string;
}

interface IncidentListCacheEntry {
  items: IncidentView[];
  nextCursor: string;
  lastUpdatedAt: string;
}

const listSessionCache = new Map<string, IncidentListCacheEntry>();

function cacheKey(query: IncidentListQuery): string {
  return JSON.stringify(serializeListQuery(query));
}

export function useIncidentList(router: Router, initialQuery: Record<string, unknown>) {
  const filters = reactive<IncidentListQuery>(normalizeListQuery(initialQuery));
  const cached = listSessionCache.get(cacheKey(filters));
  const items = ref<IncidentView[]>(cached?.items ?? []);
  const nextCursor = ref(cached?.nextCursor ?? "");
  const state = ref<LoadState>(cached ? (cached.items.length > 0 ? "ready" : "empty") : "loading");
  const error = ref<IncidentListError | null>(null);
  const loading = ref(false);
  const loadingMore = ref(false);
  const lastUpdatedAt = ref(cached?.lastUpdatedAt ?? "");
  const hydratedFromCache = Boolean(cached);
  let requestIdentity = 0;
  let controller: AbortController | null = null;

  async function load(append = false) {
    const identity = ++requestIdentity;
    controller?.abort();
    controller = new AbortController();
    loading.value = true;
    loadingMore.value = append;
    if (!append && items.value.length === 0) state.value = "loading";
    error.value = null;
    try {
      const query: IncidentListQuery = { ...filters };
      const requestedCursor = append ? nextCursor.value : (query.cursor ?? "");
      if (append && requestedCursor) query.cursor = requestedCursor;
      const result = await listIncidents(query, controller.signal);
      if (identity !== requestIdentity) return;
      items.value = append ? [...items.value, ...result.items] : result.items;
      nextCursor.value = result.next_cursor ?? "";
      if (append) {
        filters.cursor = requestedCursor || undefined;
        await router.replace({ query: serializeListQuery(filters) as LocationQueryRaw });
      }
      state.value = items.value.length === 0 ? "empty" : "ready";
      lastUpdatedAt.value = new Date().toISOString();
      listSessionCache.set(cacheKey(filters), {
        items: [...items.value],
        nextCursor: nextCursor.value,
        lastUpdatedAt: lastUpdatedAt.value,
      });
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

  onBeforeUnmount(() => controller?.abort());

  return {
    filters,
    items,
    nextCursor,
    state,
    error,
    loading,
    loadingMore,
    lastUpdatedAt,
    hydratedFromCache,
    load,
    loadMore: () => load(true),
    syncURLAndLoad,
    reset,
  };
}
