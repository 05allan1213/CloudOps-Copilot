<script setup lang="ts">
import type { LoadState } from "../../types/incidents";

withDefaults(defineProps<{ id: string; title: string; state: LoadState; error?: string; emptyText?: string }>(), {
  error: "",
  emptyText: "No persisted facts are available.",
});
</script>

<template>
  <section
    :id="id"
    class="incident-section"
    :aria-labelledby="`${id}-title`"
  >
    <div class="incident-section__heading">
      <h2 :id="`${id}-title`">
        {{ title }}
      </h2>
      <slot name="heading" />
    </div>
    <div
      v-if="state === 'loading'"
      role="status"
      aria-live="polite"
      class="section-message"
    >
      Loading {{ title }}…
    </div>
    <div
      v-else-if="state === 'forbidden'"
      role="status"
      class="section-message section-message--warning"
    >
      Viewer access was denied by the V3 role policy.
    </div>
    <div
      v-else-if="state === 'not_found'"
      role="status"
      class="section-message"
    >
      Not found
    </div>
    <div
      v-else-if="state === 'unavailable'"
      role="status"
      class="section-message section-message--warning"
    >
      Unavailable{{ error ? `: ${error}` : "" }}
    </div>
    <div
      v-else-if="state === 'error'"
      role="alert"
      class="section-message section-message--error"
    >
      {{ error || "Failed to load this section." }}
    </div>
    <div
      v-else-if="state === 'empty'"
      role="status"
      class="section-message"
    >
      {{ emptyText }}
    </div>
    <slot v-else />
  </section>
</template>

<style scoped>
.incident-section { scroll-margin-top: 20px; border: 1px solid var(--cloudops-border-color); border-radius: 10px; background: var(--cloudops-bg-card); padding: 20px; }
.incident-section__heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 16px; }
.incident-section h2 { margin: 0; font-size: 18px; }
.section-message { color: var(--el-text-color-secondary); padding: 18px 0; }
.section-message--warning { color: var(--el-color-warning-dark-2); }
.section-message--error { color: var(--el-color-danger); }
</style>
