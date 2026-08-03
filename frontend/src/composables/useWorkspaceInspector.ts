import { computed, nextTick, onScopeDispose, shallowRef, toValue, watch, type MaybeRefOrGetter } from "vue";
import {
  useRoute,
  useRouter,
  type LocationQuery,
  type LocationQueryRaw,
  type RouteLocationRaw,
} from "vue-router";

import { firstQueryValue } from "./useWorkspaceQuery";

export function queryWithInspectorSelection(
  current: LocationQuery,
  selectedKey: string,
  selectedID: string,
  legacySelectedKeys: readonly string[] = [],
): LocationQueryRaw {
  const next: LocationQueryRaw = { ...current };
  delete next[selectedKey];
  for (const key of legacySelectedKeys) delete next[key];
  if (selectedID) next[selectedKey] = selectedID;
  return next;
}

export function queryWithoutInspectorSelection(
  current: LocationQuery,
  selectedKey: string,
  legacySelectedKeys: readonly string[] = [],
): LocationQueryRaw {
  return queryWithInspectorSelection(current, selectedKey, "", legacySelectedKeys);
}

export interface WorkspaceInspectorOptions {
  selectedKey?: string;
  legacySelectedKeys?: readonly string[];
  scrollElement?: MaybeRefOrGetter<HTMLElement | null>;
  resolveTrigger?: (selectedID: string) => HTMLElement | null;
}

export function useWorkspaceInspector(options: WorkspaceInspectorOptions = {}) {
  const route = useRoute();
  const router = useRouter();
  const selectedKey = options.selectedKey ?? "selected";
  const legacySelectedKeys = options.legacySelectedKeys ?? [];
  const triggerElement = shallowRef<HTMLElement | null>(null);
  const selectedID = computed(() => {
    const keys = [selectedKey, ...legacySelectedKeys];
    return keys.map((key) => firstQueryValue(route.query[key])).find(Boolean) ?? "";
  });
  let openedFromList = false;
  let pendingRestoreID = "";
  let savedWindowScrollY: number | null = null;
  let savedElementScrollTop: number | null = null;

  function captureContext(trigger?: HTMLElement | null) {
    if (trigger) triggerElement.value = trigger;
    if (typeof window !== "undefined") savedWindowScrollY = window.scrollY;
    const element = options.scrollElement ? toValue(options.scrollElement) : null;
    savedElementScrollTop = element?.scrollTop ?? null;
  }

  async function restoreContext(selected: string) {
    await nextTick();
    const scrollElement = options.scrollElement ? toValue(options.scrollElement) : null;
    if (scrollElement && savedElementScrollTop !== null) scrollElement.scrollTop = savedElementScrollTop;
    if (typeof window !== "undefined" && savedWindowScrollY !== null) {
      window.scrollTo({ top: savedWindowScrollY, behavior: "auto" });
    }
    const trigger = triggerElement.value?.isConnected
      ? triggerElement.value
      : options.resolveTrigger?.(selected) ?? null;
    trigger?.focus({ preventScroll: true });
    pendingRestoreID = "";
  }

  async function open(selected: string, trigger?: HTMLElement | null) {
    if (!selected) return;
    const switching = Boolean(selectedID.value);
    if (!switching) {
      captureContext(trigger);
      openedFromList = true;
    } else if (trigger) {
      triggerElement.value = trigger;
    }
    const location = {
      path: route.path,
      query: queryWithInspectorSelection(route.query, selectedKey, selected, legacySelectedKeys),
      hash: route.hash,
    };
    if (switching) await router.replace(location);
    else await router.push(location);
  }

  async function close() {
    if (!selectedID.value) return;
    pendingRestoreID = selectedID.value;
    if (openedFromList) {
      openedFromList = false;
      router.back();
      return;
    }
    await router.replace({
      path: route.path,
      query: queryWithoutInspectorSelection(route.query, selectedKey, legacySelectedKeys),
      hash: route.hash,
    });
    await restoreContext(pendingRestoreID);
  }

  function openFull(location: RouteLocationRaw) {
    return router.push(location);
  }

  async function refreshTrigger(selected: string) {
    if (!options.resolveTrigger) return;
    await nextTick();
    if (selectedID.value !== selected) return;
    const resolved = options.resolveTrigger(selected);
    if (resolved) triggerElement.value = resolved;
  }

  const stopWatching = watch(selectedID, (current, previous) => {
    if (current) {
      void refreshTrigger(current);
      return;
    }
    if (!current && previous) void restoreContext(pendingRestoreID || previous);
  });
  onScopeDispose(stopWatching);

  return {
    selectedID,
    triggerElement,
    open,
    close,
    openFull,
  };
}
