<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { CopyDocument } from "@element-plus/icons-vue";

const props = defineProps<{ label: string; value?: string }>();
const copyStatus = ref("");
let resetTimer: number | null = null;

onBeforeUnmount(() => {
  if (resetTimer !== null) window.clearTimeout(resetTimer);
});

async function copyValue() {
  if (!props.value || !navigator.clipboard) {
    copyStatus.value = "Copy unavailable";
    return;
  }
  try {
    await navigator.clipboard.writeText(props.value);
    copyStatus.value = "Copied";
    if (resetTimer !== null) window.clearTimeout(resetTimer);
    resetTimer = window.setTimeout(() => { copyStatus.value = ""; }, 1600);
  } catch {
    copyStatus.value = "Copy failed";
  }
}
</script>

<template>
  <div class="hash-value">
    <span>{{ label }}</span>
    <code translate="no">{{ value || "Not projected" }}</code>
    <button
      v-if="value"
      type="button"
      class="copy-button"
      :aria-label="`Copy ${label}`"
      :title="`Copy ${label}`"
      @click="copyValue"
    >
      <el-icon aria-hidden="true">
        <CopyDocument />
      </el-icon>
    </button>
    <small
      role="status"
      aria-live="polite"
    >{{ copyStatus }}</small>
  </div>
</template>

<style scoped>
.hash-value { display: grid; grid-template-columns: minmax(120px, .42fr) minmax(0, 1fr) 44px; align-items: center; gap: 10px; padding: 9px 0; border-bottom: 1px solid var(--co-border-default); }
.hash-value > span { color: var(--co-text-muted); font-size: 12px; font-weight: 600; }
code { min-width: 0; overflow-wrap: anywhere; font-size: 11px; }
.copy-button { display: grid; place-items: center; width: 44px; height: 44px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: transparent; cursor: pointer; }
.copy-button:hover { color: var(--co-action-primary); border-color: var(--co-action-primary); background: var(--co-bg-hover); }
small { grid-column: 2 / -1; min-height: 16px; color: var(--co-status-success-fg); }
@media (max-width: 640px) { .hash-value { grid-template-columns: minmax(0, 1fr) 44px; } .hash-value > span, code { grid-column: 1; } .copy-button { grid-column: 2; grid-row: 1 / 3; } small { grid-column: 1 / -1; } }
</style>
