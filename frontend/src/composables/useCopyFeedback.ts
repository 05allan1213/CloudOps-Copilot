import { computed, getCurrentScope, onScopeDispose, ref } from "vue";

export const COPY_FEEDBACK_DURATION_MS = 1_200;

export type CopyFeedbackState = "idle" | "copied" | "failed";
export type ClipboardWriter = (value: string) => Promise<void>;

export async function writeClipboardText(value: string): Promise<void> {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return;
    } catch {
      // Fall through to the same-origin DOM fallback used by local previews.
    }
  }

  if (typeof document === "undefined") throw new Error("Clipboard is unavailable");
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.append(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("Clipboard write failed");
}

export function useCopyFeedback(options: {
  duration?: number;
  writer?: ClipboardWriter;
} = {}) {
  const state = ref<CopyFeedbackState>("idle");
  const duration = options.duration ?? COPY_FEEDBACK_DURATION_MS;
  const writer = options.writer ?? writeClipboardText;
  let resetTimer: ReturnType<typeof setTimeout> | undefined;

  function reset() {
    if (resetTimer !== undefined) clearTimeout(resetTimer);
    resetTimer = undefined;
    state.value = "idle";
  }

  async function copy(value: string): Promise<boolean> {
    if (!value) {
      state.value = "failed";
      return false;
    }
    try {
      await writer(value);
      state.value = "copied";
    } catch {
      state.value = "failed";
    }
    if (resetTimer !== undefined) clearTimeout(resetTimer);
    resetTimer = setTimeout(reset, duration);
    return state.value === "copied";
  }

  if (getCurrentScope()) onScopeDispose(reset);

  return {
    state,
    copied: computed(() => state.value === "copied"),
    failed: computed(() => state.value === "failed"),
    copy,
    reset,
  };
}
