<script setup lang="ts" generic="T">
export type DenseListSeverity = "critical" | "warning" | "info" | "neutral" | "success";

const props = withDefaults(defineProps<{
  items: readonly T[];
  itemKey: (item: T, index: number) => string;
  label: string;
  selectedKey?: string;
  empty?: string;
  severity?: (item: T) => DenseListSeverity;
  disabled?: (item: T) => boolean;
}>(), {
  selectedKey: "",
  empty: "当前条件下没有结果",
  severity: undefined,
  disabled: undefined,
});

const emit = defineEmits<{
  select: [item: T, trigger: HTMLElement | null];
}>();

function selectItem(item: T, event: MouseEvent) {
  if (props.disabled?.(item)) return;
  emit("select", item, event.currentTarget instanceof HTMLElement ? event.currentTarget : null);
}
</script>

<template>
  <section
    class="workspace-dense-list"
    :aria-label="label"
  >
    <ul v-if="items.length">
      <li
        v-for="(item, index) in items"
        :key="itemKey(item, index)"
        :class="`workspace-dense-list-item--${severity?.(item) ?? 'neutral'}`"
      >
        <UButton
          color="neutral"
          variant="ghost"
          block
          class="workspace-dense-list-control"
          :class="{ 'is-selected': selectedKey === itemKey(item, index) }"
          :disabled="disabled?.(item)"
          :aria-pressed="selectedKey === itemKey(item, index)"
          @click="selectItem(item, $event)"
        >
          <span
            v-if="$slots.leading"
            class="workspace-dense-list-leading"
          >
            <slot
              name="leading"
              :item="item"
              :index="index"
            />
          </span>
          <span class="workspace-dense-list-copy">
            <strong><slot
              name="title"
              :item="item"
              :index="index"
            /></strong>
            <small><slot
              name="description"
              :item="item"
              :index="index"
            /></small>
          </span>
          <span
            v-if="$slots.meta"
            class="workspace-dense-list-meta"
          >
            <slot
              name="meta"
              :item="item"
              :index="index"
            />
          </span>
          <span
            v-if="$slots.trailing"
            class="workspace-dense-list-trailing"
          >
            <slot
              name="trailing"
              :item="item"
              :index="index"
            />
          </span>
        </UButton>
      </li>
    </ul>
    <p
      v-else
      class="workspace-dense-list-empty"
    >
      {{ empty }}
    </p>
  </section>
</template>

<style scoped>
.workspace-dense-list {
  min-width: 0;
  overflow: visible;
}
.workspace-dense-list ul { display: grid; margin: 0; padding: 0; gap: var(--co-space-2); list-style: none; }
.workspace-dense-list li {
  position: relative;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--co-border-subtle);
  border-radius: var(--co-radius-panel);
  background: color-mix(in srgb, var(--co-bg-surface) 88%, var(--co-bg-canvas));
  box-shadow: var(--co-shadow-row);
  transition: border-color var(--co-motion-fast) var(--co-ease-out), background var(--co-motion-fast) var(--co-ease-out), transform var(--co-motion-fast) var(--co-ease-out);
}
.workspace-dense-list li:hover {
  z-index: 1;
  border-color: var(--co-border-strong);
  background: var(--co-bg-hover);
  transform: translateY(-1px);
}
.workspace-dense-list-control {
  position: relative;
  min-height: var(--co-dense-list-row-min-height);
  justify-content: stretch;
  gap: var(--co-space-3);
  padding: var(--co-space-3) var(--co-space-4);
  border-radius: inherit;
  text-align: left;
}
.workspace-dense-list-control::before {
  position: absolute;
  top: 50%;
  left: 8px;
  width: var(--co-severity-marker-width);
  height: 28px;
  border-radius: var(--co-radius-pill);
  background: transparent;
  content: "";
  transform: translateY(-50%);
}
.workspace-dense-list-item--critical .workspace-dense-list-control::before { background: var(--co-status-critical-fg); }
.workspace-dense-list-item--warning .workspace-dense-list-control::before { background: var(--co-status-warning-fg); }
.workspace-dense-list-item--info .workspace-dense-list-control::before { background: var(--co-status-info-fg); }
.workspace-dense-list-item--success .workspace-dense-list-control::before { background: var(--co-status-success-fg); }
.workspace-dense-list-control.is-selected { border-radius: var(--co-radius-panel); background: var(--co-bg-active); box-shadow: inset 0 0 0 1px var(--co-action-primary); }
.workspace-dense-list-leading,
.workspace-dense-list-trailing { display: flex; min-width: 0; flex: 0 0 auto; align-items: center; }
.workspace-dense-list-copy { display: grid; min-width: 0; flex: 1 1 auto; gap: 2px; }
.workspace-dense-list-copy strong,
.workspace-dense-list-copy small { min-width: 0; overflow-wrap: anywhere; }
.workspace-dense-list-copy strong { color: var(--co-text-primary); font-size: 12px; line-height: 1.35; }
.workspace-dense-list-copy small { color: var(--co-text-muted); font-size: 11px; line-height: 1.4; }
.workspace-dense-list-meta { min-width: 0; flex: 0 1 auto; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 10px; overflow-wrap: anywhere; }
.workspace-dense-list-empty { min-height: var(--co-dense-list-row-min-height); margin: 0; padding: var(--co-space-4); color: var(--co-text-muted); font-size: 12px; }

@media (max-width: 1024px) {
  .workspace-dense-list-control { flex-wrap: wrap; }
  .workspace-dense-list-meta { width: 100%; padding-left: calc(var(--co-status-icon-size) + var(--co-space-3)); }
}

@media (prefers-reduced-motion: reduce) {
  .workspace-dense-list li { transition: none; }
  .workspace-dense-list li:hover { transform: none; }
}
</style>
