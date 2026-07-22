<script setup lang="ts">
import { computed } from "vue";

import { formatJSON } from "../../models/workbench";

const props = defineProps<{ title: string; value?: unknown }>();
const formatted = computed(() => formatJSON(props.value));
</script>

<template>
  <details class="json-snapshot">
    <summary>{{ title }}</summary>
    <pre><code>{{ formatted }}</code></pre>
  </details>
</template>

<style scoped>
.json-snapshot { border-top: 1px solid var(--co-border-default); }
summary { min-height: 44px; padding: 11px 0; color: var(--co-text-secondary); font-weight: 600; cursor: pointer; }
summary:focus-visible { outline: 2px solid var(--co-focus-ring); outline-offset: 3px; }
pre { max-height: 420px; margin: 0 0 12px; padding: 14px; overflow: auto; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); color: var(--co-text-primary); background: var(--co-bg-canvas); white-space: pre-wrap; overflow-wrap: anywhere; }
code { font-size: 11px; line-height: 1.55; }
</style>
