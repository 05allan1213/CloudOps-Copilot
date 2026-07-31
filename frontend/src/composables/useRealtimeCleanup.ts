import { getCurrentScope, onScopeDispose } from "vue";

export type RealtimeDisposer = () => void;

export function useRealtimeCleanup() {
  const disposers = new Set<RealtimeDisposer>();
  let disposed = false;

  function register(disposer: RealtimeDisposer): RealtimeDisposer {
    if (disposed) {
      disposer();
      return () => undefined;
    }
    disposers.add(disposer);
    return () => disposers.delete(disposer);
  }

  function dispose() {
    if (disposed) return;
    disposed = true;
    const failures: unknown[] = [];
    for (const disposer of disposers) {
      try {
        disposer();
      } catch (error) {
        failures.push(error);
      }
    }
    disposers.clear();
    if (failures.length) throw failures[0];
  }

  if (getCurrentScope()) onScopeDispose(dispose);

  return { register, dispose };
}
