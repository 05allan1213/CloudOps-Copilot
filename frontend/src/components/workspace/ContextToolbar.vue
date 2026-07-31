<script setup lang="ts">
withDefaults(defineProps<{
  label: string;
  tabbed?: boolean;
}>(), {
  tabbed: false,
});
</script>

<template>
  <section
    class="context-toolbar-composition"
    :class="{ 'is-tabbed': tabbed }"
    :aria-label="label"
  >
    <div
      v-if="tabbed && $slots.tabs"
      class="context-toolbar-tabs"
    >
      <slot name="tabs" />
    </div>
    <div class="context-toolbar-row">
      <div
        v-if="$slots.filters"
        class="context-toolbar-filters"
      >
        <slot name="filters" />
      </div>
      <div
        v-if="$slots.secondary"
        class="context-toolbar-secondary"
      >
        <slot name="secondary" />
      </div>
      <div
        v-if="$slots.primary"
        class="context-toolbar-primary"
      >
        <slot name="primary" />
      </div>
    </div>
  </section>
</template>

<style scoped>
.context-toolbar-composition {
  min-width: 0;
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.context-toolbar-tabs {
  min-width: 0;
  padding: 0 var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
  overflow-x: auto;
}

.context-toolbar-row {
  display: flex;
  min-width: 0;
  min-height: 56px;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--co-space-3);
  padding: var(--co-space-2) var(--co-space-3);
}

.context-toolbar-filters,
.context-toolbar-secondary,
.context-toolbar-primary {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--co-space-2);
}

.context-toolbar-filters { flex: 1 1 480px; }
.context-toolbar-secondary { margin-left: auto; }
.context-toolbar-primary { flex: 0 0 auto; }

@media (max-width: 1024px) {
  .context-toolbar-filters { flex-basis: 100%; }
  .context-toolbar-secondary { margin-left: 0; }
  .context-toolbar-primary { margin-left: auto; }
}
</style>
