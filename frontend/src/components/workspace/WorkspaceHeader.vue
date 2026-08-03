<script setup lang="ts">
defineProps<{
  title: string;
  description?: string;
  eyebrow?: string;
  headingId?: string;
}>();
</script>

<template>
  <header class="workspace-header-composition">
    <div class="workspace-heading-copy">
      <span
        v-if="eyebrow"
        class="workspace-eyebrow"
      >{{ eyebrow }}</span>
      <h1
        :id="headingId ?? 'workspace-title'"
        tabindex="-1"
      >
        {{ title }}
      </h1>
      <p v-if="description">
        {{ description }}
      </p>
      <div
        v-if="$slots.context"
        class="workspace-heading-context"
      >
        <slot name="context" />
      </div>
    </div>
    <div
      v-if="$slots.actions"
      class="workspace-heading-actions"
    >
      <slot name="actions" />
    </div>
  </header>
</template>

<style scoped>
.workspace-header-composition {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-4);
  padding-bottom: var(--co-space-4);
}

.workspace-heading-copy { min-width: 0; }
.workspace-heading-copy h1 { margin: 0; font-size: 20px; line-height: 1.3; }
.workspace-heading-copy p {
  max-width: 90ch;
  margin: var(--co-space-1) 0 0;
  color: var(--co-text-secondary);
  font-size: 13px;
  line-height: 1.55;
}

.workspace-eyebrow {
  display: block;
  margin-bottom: var(--co-space-1);
  color: var(--co-text-muted);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
}

.workspace-heading-context,
.workspace-heading-actions {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--co-space-2);
}

.workspace-heading-context { margin-top: var(--co-space-3); }
.workspace-heading-actions { flex: 0 0 auto; justify-content: flex-end; }

@media (max-width: 1024px) {
  .workspace-header-composition { flex-direction: column; }
  .workspace-heading-actions { width: 100%; justify-content: flex-start; }
}
</style>
