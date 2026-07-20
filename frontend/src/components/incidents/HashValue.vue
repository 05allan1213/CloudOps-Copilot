<script setup lang="ts">
import { ref } from "vue";
import { CopyDocument } from "@element-plus/icons-vue";

const props = defineProps<{ label: string; value?: string }>();
const copied = ref(false);

async function copyValue() {
  if (!props.value || !navigator.clipboard) return;
  try {
    await navigator.clipboard.writeText(props.value);
    copied.value = true;
    window.setTimeout(() => { copied.value = false; }, 1600);
  } catch {
    copied.value = false;
  }
}
</script>

<template>
  <div class="hash-value">
    <span>{{ label }}</span>
    <code>{{ value || "Not projected" }}</code>
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
    <small aria-live="polite">{{ copied ? "Copied" : "" }}</small>
  </div>
</template>

<style scoped>
.hash-value { display: grid; grid-template-columns: minmax(120px, .42fr) minmax(0, 1fr) 44px; align-items: center; gap: 10px; padding: 9px 0; border-bottom: 1px solid var(--cloudops-border-color); }
.hash-value > span { color: var(--el-text-color-secondary); font-size: 12px; font-weight: 600; }
code { min-width: 0; overflow-wrap: anywhere; font-size: 11px; }
.copy-button { display: grid; place-items: center; width: 44px; height: 44px; border: 1px solid var(--cloudops-border-color); border-radius: 7px; color: var(--el-text-color-regular); background: transparent; cursor: pointer; }
.copy-button:hover, .copy-button:focus-visible { color: var(--el-color-primary); border-color: var(--el-color-primary); outline: 2px solid color-mix(in srgb, var(--el-color-primary) 35%, transparent); outline-offset: 2px; }
small { grid-column: 2 / -1; min-height: 16px; color: var(--el-color-success); }
@media (max-width: 640px) { .hash-value { grid-template-columns: minmax(0, 1fr) 44px; } .hash-value > span, code { grid-column: 1; } .copy-button { grid-column: 2; grid-row: 1 / 3; } small { grid-column: 1 / -1; } }
</style>
