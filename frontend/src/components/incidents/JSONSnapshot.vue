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
.json-snapshot { border-top: 1px solid var(--cloudops-border-color); }
summary { min-height: 44px; padding: 11px 0; color: var(--el-text-color-regular); font-weight: 600; cursor: pointer; }
summary:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 3px; }
pre { max-height: 420px; margin: 0 0 12px; padding: 14px; overflow: auto; border: 1px solid var(--cloudops-border-color); border-radius: 7px; color: var(--el-text-color-primary); background: var(--el-fill-color-lighter); white-space: pre-wrap; overflow-wrap: anywhere; }
code { font-size: 11px; line-height: 1.55; }
</style>
