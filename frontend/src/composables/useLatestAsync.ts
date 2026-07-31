import { getCurrentScope, onScopeDispose, ref, shallowRef } from "vue";

export interface LatestAsyncRunOptions {
  background?: boolean;
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
  const refreshing = ref(false);
  let sequence = 0;
  let controller: AbortController | null = null;

  async function run(
    task: (context: LatestAsyncContext) => Promise<T>,
    options: LatestAsyncRunOptions = {},
  ): Promise<T | undefined> {
    controller?.abort();
    controller = new AbortController();
    const currentSequence = ++sequence;
    const background = options.background === true && data.value !== null;
    error.value = null;
    loading.value = !background;
    refreshing.value = background;

    try {
      const result = await task({ signal: controller.signal, sequence: currentSequence });
      if (currentSequence !== sequence || controller.signal.aborted) return undefined;
      data.value = result;
      return result;
    } catch (reason) {
      if (currentSequence !== sequence || controller.signal.aborted || isAbortError(reason)) return undefined;
      error.value = reason;
      return undefined;
    } finally {
      if (currentSequence === sequence) {
        loading.value = false;
        refreshing.value = false;
      }
    }
  }

  function cancel() {
    sequence += 1;
    controller?.abort();
    controller = null;
    loading.value = false;
    refreshing.value = false;
  }

  if (getCurrentScope()) onScopeDispose(cancel);

  return {
    data,
    error,
    loading,
    refreshing,
    run,
    cancel,
  };
}
