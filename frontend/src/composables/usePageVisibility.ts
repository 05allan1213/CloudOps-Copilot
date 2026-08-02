import { getCurrentScope, onScopeDispose, readonly, ref } from "vue";

export interface VisibilitySource {
  readonly hidden: boolean;
  addEventListener(type: "visibilitychange", listener: () => void): void;
  removeEventListener(type: "visibilitychange", listener: () => void): void;
}

export function usePageVisibility(source?: VisibilitySource) {
  const resolved = source ?? (typeof document === "undefined" ? undefined : document);
  const visible = ref(!resolved?.hidden);
  const hiddenAt = ref<number | null>(visible.value ? null : Date.now());
  const lastHiddenDurationMs = ref(0);

  const update = () => {
    const nextVisible = !resolved?.hidden;
    if (nextVisible === visible.value) return;
    visible.value = nextVisible;
    if (!nextVisible) hiddenAt.value = Date.now();
    else {
      lastHiddenDurationMs.value = hiddenAt.value === null ? 0 : Math.max(0, Date.now() - hiddenAt.value);
      hiddenAt.value = null;
    }
  };

  resolved?.addEventListener("visibilitychange", update);
  const dispose = () => resolved?.removeEventListener("visibilitychange", update);
  if (getCurrentScope()) onScopeDispose(dispose);

  return {
    visible: readonly(visible),
    hiddenAt: readonly(hiddenAt),
    lastHiddenDurationMs: readonly(lastHiddenDurationMs),
    dispose,
  };
}
