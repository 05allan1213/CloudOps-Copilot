import { onBeforeUnmount, reactive, ref } from "vue";
import type { LocationQueryRaw, Router } from "vue-router";

import { listIncidents } from "../../api/incidents";
import { isApiError } from "../../api/client";
import { loadStateForStatus, normalizeListQuery, serializeListQuery } from "../../models/incidents";
import type { IncidentListQuery, IncidentView, LoadState } from "../../types/incidents";

export function useIncidentList(router: Router, initialQuery: Record<string, unknown>) {
  const filters = reactive<IncidentListQuery>(normalizeListQuery(initialQuery));
  const items = ref<IncidentView[]>([]);
  const nextCursor = ref("");
  const state = ref<LoadState>("loading");
  const error = ref("");
  let requestIdentity = 0;
  let controller: AbortController | null = null;

  async function load(append = false) {
    const identity = ++requestIdentity;
    controller?.abort();
    controller = new AbortController();
    state.value = "loading";
    error.value = "";
    try {
      const query: IncidentListQuery = { ...filters };
      if (append && nextCursor.value) query.cursor = nextCursor.value;
      const result = await listIncidents(query, controller.signal);
      if (identity !== requestIdentity) return;
      items.value = append ? [...items.value, ...result.items] : result.items;
      nextCursor.value = result.next_cursor ?? "";
      state.value = items.value.length === 0 ? "empty" : "ready";
    } catch (cause) {
      if (identity !== requestIdentity || controller.signal.aborted) return;
      error.value = cause instanceof Error ? cause.message : "Failed to load incidents";
      state.value = loadStateForStatus(isApiError(cause) ? cause.status : null);
    }
  }

  async function syncURLAndLoad() {
    await router.replace({ query: serializeListQuery(filters) as LocationQueryRaw });
    nextCursor.value = "";
    await load(false);
  }

  function reset() {
    Object.assign(filters, { status: undefined, severity: undefined, service: undefined, limit: 50, cursor: undefined });
    return syncURLAndLoad();
  }

  onBeforeUnmount(() => controller?.abort());

  return { filters, items, nextCursor, state, error, load, loadMore: () => load(true), syncURLAndLoad, reset };
}
