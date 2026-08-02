import { getCurrentScope, onScopeDispose, ref, shallowRef } from "vue";

export interface LatestAsyncRunOptions {
  background?: boolean;
  loadingDelayMs?: number;
  source?: "network" | "fresh-cache" | "stale-cache";
  staleReason?: string;
  updatedAt?: number;
  requestIdentity?: string;
}

export interface LatestAsyncContext {
  signal: AbortSignal;
  sequence: number;
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export function useLatestAsync<T>() {
  const data = shallowRef<T | null>(null);
  const error = shallowRef<unknown>(null);
  const loading = ref(false);
  const pending = ref(false);
  const refreshing = ref(false);
  const source = ref<LatestAsyncRunOptions["source"]>("network");
  const staleReason = ref("");
  const updatedAt = ref(0);
  const requestIdentity = ref("");
  let sequence = 0;
  let controller: AbortController | null = null;
  let loadingTimer: ReturnType<typeof setTimeout> | undefined;

  function clearLoadingTimer() {
    if (loadingTimer !== undefined) clearTimeout(loadingTimer);
    loadingTimer = undefined;
  }

  async function run(
    task: (context: LatestAsyncContext) => Promise<T>,
    options: LatestAsyncRunOptions = {},
  ): Promise<T | undefined> {
    controller?.abort();
    clearLoadingTimer();
    controller = new AbortController();
    const currentSequence = ++sequence;
    const background = options.background === true && data.value !== null;
    error.value = null;
    pending.value = true;
    loading.value = false;
    refreshing.value = background;
    source.value = options.source ?? "network";
    staleReason.value = options.staleReason ?? "";
    updatedAt.value = options.updatedAt ?? updatedAt.value;
    requestIdentity.value = options.requestIdentity ?? `request-${currentSequence}`;
    if (!background) {
      loadingTimer = setTimeout(() => {
        loadingTimer = undefined;
        if (currentSequence === sequence) loading.value = true;
      }, Math.max(0, options.loadingDelayMs ?? 120));
    }

    try {
      const result = await task({ signal: controller.signal, sequence: currentSequence });
      if (currentSequence !== sequence || controller.signal.aborted) return undefined;
      data.value = result;
      updatedAt.value = Date.now();
      staleReason.value = "";
      return result;
    } catch (reason) {
      if (currentSequence !== sequence || controller.signal.aborted || isAbortError(reason)) return undefined;
      error.value = reason;
      return undefined;
    } finally {
      if (currentSequence === sequence) {
        clearLoadingTimer();
        pending.value = false;
        loading.value = false;
        refreshing.value = false;
      }
    }
  }

  function cancel() {
    sequence += 1;
    controller?.abort();
    controller = null;
    clearLoadingTimer();
    pending.value = false;
    loading.value = false;
    refreshing.value = false;
  }

  if (getCurrentScope()) onScopeDispose(cancel);

  return {
    data,
    error,
    loading,
    pending,
    refreshing,
    source,
    staleReason,
    updatedAt,
    requestIdentity,
    run,
    cancel,
  };
}
