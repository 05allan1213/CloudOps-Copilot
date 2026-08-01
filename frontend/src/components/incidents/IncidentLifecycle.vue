<script setup lang="ts">
export type IncidentLifecycleState = "complete" | "current" | "blocked" | "pending";

export interface IncidentLifecycleStep {
  id: string;
  label: string;
  description: string;
  icon: string;
  state: IncidentLifecycleState;
}

defineProps<{
  steps: readonly IncidentLifecycleStep[];
  label?: string;
}>();
</script>

<template>
  <ol
    class="incident-lifecycle"
    :aria-label="label ?? 'Incident 生命周期'"
  >
    <li
      v-for="step in steps"
      :key="step.id"
      :class="`is-${step.state}`"
      :aria-current="step.state === 'current' ? 'step' : undefined"
    >
      <span class="lifecycle-marker">
        <UIcon
          :name="step.icon"
          aria-hidden="true"
        />
      </span>
      <span class="lifecycle-copy">
        <strong>{{ step.label }}</strong>
        <small>{{ step.description }}</small>
      </span>
    </li>
  </ol>
</template>

<style scoped>
.incident-lifecycle {
  display: grid;
  grid-template-columns: repeat(7, minmax(92px, 1fr));
  min-width: 0;
  margin: 0;
  padding: var(--co-space-3) 0;
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: var(--co-bg-surface);
  list-style: none;
}

.incident-lifecycle li {
  position: relative;
  display: grid;
  min-width: 0;
  grid-template-rows: auto 1fr;
  justify-items: center;
  gap: var(--co-space-2);
  padding: 0 var(--co-space-2);
  text-align: center;
}

.incident-lifecycle li:not(:last-child)::after {
  position: absolute;
  top: 14px;
  right: -50%;
  width: 100%;
  height: 1px;
  background: var(--co-border-default);
  content: "";
}

.lifecycle-marker {
  z-index: 1;
  display: grid;
  width: 29px;
  height: 29px;
  place-items: center;
  border: 1px solid var(--co-status-neutral-border);
  border-radius: 50%;
  color: var(--co-status-neutral-fg);
  background: var(--co-bg-surface);
}

.lifecycle-copy { display: grid; min-width: 0; gap: 2px; }
.lifecycle-copy strong { color: var(--co-text-secondary); font-size: 11px; }
.lifecycle-copy small { color: var(--co-text-muted); font-size: 9px; line-height: 1.35; overflow-wrap: anywhere; }
.is-complete .lifecycle-marker { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.is-current .lifecycle-marker { border-color: var(--co-action-primary); color: var(--co-action-primary); background: var(--co-bg-active); box-shadow: 0 0 0 3px color-mix(in srgb, var(--co-action-primary) 12%, transparent); }
.is-current .lifecycle-copy strong { color: var(--co-text-primary); }
.is-blocked .lifecycle-marker { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }

@media (max-width: 1180px) {
  .incident-lifecycle { grid-template-columns: repeat(7, minmax(76px, 1fr)); overflow-x: auto; }
  .incident-lifecycle li { min-width: 76px; }
  .lifecycle-copy small { display: none; }
}

@media (prefers-reduced-motion: reduce) {
  .incident-lifecycle { scroll-behavior: auto; }
}
</style>
