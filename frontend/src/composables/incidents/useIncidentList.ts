import { computed, onBeforeUnmount, reactive, ref } from "vue";
import type { LocationQueryRaw, Router } from "vue-router";

import { listIncidents } from "../../api/incidents";
import { isApiError } from "../../api/client";
import { loadStateForStatus, normalizeListQuery, serializeListQuery } from "../../models/incidents";
import type { IncidentDTO, IncidentListQuery, LoadState } from "../../types/incidents";

export function useIncidentList(router: Router, initialQuery: Record<string, unknown>) {
  const filters = reactive<IncidentListQuery>(normalizeListQuery(initialQuery));
  const items = ref<IncidentDTO[]>([]);
  const total = ref(0);
  const state = ref<LoadState>("loading");
  const error = ref("");
  let requestIdentity = 0;
  let controller: AbortController | null = null;

  const page = computed({
    get: () => filters.page ?? 1,
    set: (value: number) => { filters.page = value; },
  });

  async function load() {
    const identity = ++requestIdentity;
    controller?.abort();
    controller = new AbortController();
    state.value = "loading";
    error.value = "";
    try {
      const result = await listIncidents({ ...filters }, controller.signal);
      if (identity !== requestIdentity) return;
      items.value = result.items;
      total.value = result.total;
      state.value = result.items.length === 0 ? "empty" : "ready";
    } catch (cause) {
      if (identity !== requestIdentity || controller.signal.aborted) return;
      error.value = cause instanceof Error ? cause.message : "Failed to load incidents";
      state.value = loadStateForStatus(isApiError(cause) ? cause.status : null);
    }
  }

  async function syncURLAndLoad(resetPage = false) {
    if (resetPage) filters.page = 1;
    await router.replace({ query: serializeListQuery(filters) as LocationQueryRaw });
    await load();
  }

  function restoreFromURL(query: Record<string, unknown>) {
    Object.assign(filters, normalizeListQuery(query));
  }

  function reset() {
    Object.assign(filters, { status: undefined, severity: undefined, service: undefined, environment: undefined, namespace: undefined, workload: undefined, created_from: undefined, created_to: undefined, q: undefined, page: 1, page_size: 20 });
    return syncURLAndLoad();
  }

  onBeforeUnmount(() => controller?.abort());

  return { filters, items, total, state, error, page, load, syncURLAndLoad, restoreFromURL, reset };
}
