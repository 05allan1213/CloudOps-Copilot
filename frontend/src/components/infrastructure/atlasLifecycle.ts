export interface AtlasFrameScheduler {
  request(callback: FrameRequestCallback): number;
  cancel(handle: number): void;
}

export interface AtlasFrameLifecycle {
  request(): void;
  setVisible(visible: boolean): void;
  dispose(): void;
}

export function createAtlasFrameLifecycle(
  draw: () => void,
  scheduler: AtlasFrameScheduler = {
    request: (callback) => window.requestAnimationFrame(callback),
    cancel: (handle) => window.cancelAnimationFrame(handle),
  },
): AtlasFrameLifecycle {
  let visible = true;
  let disposed = false;
  let pending = 0;

  function request() {
    if (disposed || !visible || pending) return;
    pending = scheduler.request(() => {
      pending = 0;
      if (!disposed && visible) draw();
    });
  }

  function setVisible(nextVisible: boolean) {
    if (disposed || visible === nextVisible) return;
    visible = nextVisible;
    if (!visible && pending) {
      scheduler.cancel(pending);
      pending = 0;
    }
    if (visible) request();
  }

  function dispose() {
    if (disposed) return;
    disposed = true;
    if (pending) scheduler.cancel(pending);
    pending = 0;
  }

  return { request, setVisible, dispose };
}
