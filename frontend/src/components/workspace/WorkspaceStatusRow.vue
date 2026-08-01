<script setup lang="ts">
import type { PresentationColor } from "./workspacePresentation";

withDefaults(defineProps<{
  title: string;
  description?: string;
  icon?: string;
  badge?: string;
  tone?: PresentationColor;
  busy?: boolean;
  role?: "alert" | "status";
}>(), {
  description: "",
  icon: "i-lucide-info",
  badge: "",
  tone: "neutral",
  busy: false,
  role: "status",
});
</script>

<template>
  <section
    class="workspace-status-row"
    :class="`workspace-status-row--${tone}`"
    :role="role"
    :aria-live="role === 'alert' ? 'assertive' : 'polite'"
    :aria-busy="busy"
  >
    <span
      class="workspace-status-row-icon"
      aria-hidden="true"
    >
      <UIcon
        :name="busy ? 'i-lucide-loader-circle' : icon"
        :class="{ 'is-spinning': busy }"
      />
    </span>
    <div class="workspace-status-row-copy">
      <strong>{{ title }}</strong>
      <p v-if="description">
        {{ description }}
      </p>
    </div>
    <UBadge
      v-if="badge"
      :color="tone"
      variant="soft"
      :label="badge"
    />
    <div
      v-if="$slots.meta"
      class="workspace-status-row-meta"
    >
      <slot name="meta" />
    </div>
    <div
      v-if="$slots.actions"
      class="workspace-status-row-actions"
    >
      <slot name="actions" />
    </div>
  </section>
</template>

<style scoped>
.workspace-status-row {
  display: grid;
  min-width: 0;
  min-height: var(--co-status-row-min-height);
  grid-template-columns: auto minmax(0, 1fr) auto auto auto;
  align-items: center;
  gap: var(--co-space-3);
  padding: var(--co-space-2) var(--co-space-3);
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.workspace-status-row-icon {
  display: grid;
  width: var(--co-status-icon-size);
  height: var(--co-status-icon-size);
  place-items: center;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-status-neutral-fg);
  background: var(--co-bg-floating);
}

.workspace-status-row--error .workspace-status-row-icon { color: var(--co-status-critical-fg); }
.workspace-status-row--warning .workspace-status-row-icon { color: var(--co-status-warning-fg); }
.workspace-status-row--success .workspace-status-row-icon { color: var(--co-status-success-fg); }
.workspace-status-row--info .workspace-status-row-icon { color: var(--co-status-info-fg); }
.workspace-status-row-copy { min-width: 0; }
.workspace-status-row-copy strong { display: block; font-size: 12px; }
.workspace-status-row-copy p { margin: 2px 0 0; color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
.workspace-status-row-meta { min-width: 0; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; overflow-wrap: anywhere; }
.workspace-status-row-actions { display: flex; min-width: 0; align-items: center; gap: var(--co-space-1); }
.is-spinning { animation: workspace-status-spin var(--co-spinner-duration) linear infinite; }

@keyframes workspace-status-spin { to { transform: rotate(360deg); } }

@media (max-width: 1024px) {
  .workspace-status-row { grid-template-columns: auto minmax(0, 1fr) auto; }
  .workspace-status-row-meta { grid-column: 2 / -1; }
  .workspace-status-row-actions { grid-column: 2 / -1; justify-content: flex-end; }
}

@media (prefers-reduced-motion: reduce) {
  .is-spinning { animation: none; }
}
</style>
