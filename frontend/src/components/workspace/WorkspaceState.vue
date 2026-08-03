<script setup lang="ts">
import { computed } from "vue";

import {
  workspaceStateDefinition,
  type WorkspaceStateKind,
} from "./workspacePresentation";

const props = withDefaults(defineProps<{
  kind: WorkspaceStateKind;
  title?: string;
  description?: string;
  code?: string;
  requestID?: string;
  traceID?: string;
  idempotentReplay?: boolean | null;
  nextSteps?: readonly string[];
}>(), {
  title: "",
  description: "",
  code: "",
  requestID: "",
  traceID: "",
  idempotentReplay: null,
  nextSteps: () => [],
});

const definition = computed(() => workspaceStateDefinition(props.kind));
const hasIdentity = computed(() => Boolean(
  props.code || props.requestID || props.traceID || props.idempotentReplay !== null,
));
</script>

<template>
  <section
    v-if="kind === 'loading'"
    class="workspace-state workspace-state-loading"
    role="status"
    aria-live="polite"
  >
    <div class="workspace-state-heading">
      <UIcon
        :name="definition.icon"
        class="workspace-state-spinner"
        aria-hidden="true"
      />
      <div>
        <strong>{{ title || definition.title }}</strong>
        <span>{{ description || definition.description }}</span>
      </div>
    </div>
    <USkeleton
      v-for="index in 3"
      :key="index"
      class="workspace-state-skeleton"
    />
  </section>

  <UAlert
    v-else
    class="workspace-state"
    :class="`workspace-state-${kind}`"
    :color="definition.color"
    variant="soft"
    :icon="definition.icon"
    :title="title || definition.title"
    :role="definition.role"
    :aria-live="definition.role === 'alert' ? 'assertive' : 'polite'"
  >
    <template #description>
      <div class="workspace-state-description">
        <p>{{ description || definition.description }}</p>
        <dl v-if="hasIdentity">
          <div v-if="code">
            <dt>Code</dt><dd>{{ code }}</dd>
          </div>
          <div v-if="requestID">
            <dt>Request ID</dt><dd>{{ requestID }}</dd>
          </div>
          <div v-if="traceID">
            <dt>Trace ID</dt><dd>{{ traceID }}</dd>
          </div>
          <div v-if="idempotentReplay !== null">
            <dt>Idempotent replay</dt><dd>{{ idempotentReplay ? "YES" : "NO" }}</dd>
          </div>
        </dl>
        <div v-if="nextSteps?.length">
          <strong>建议下一步</strong>
          <ol>
            <li
              v-for="step in nextSteps"
              :key="step"
            >
              {{ step }}
            </li>
          </ol>
        </div>
      </div>
    </template>
    <template
      v-if="$slots.actions"
      #actions
    >
      <slot name="actions" />
    </template>
  </UAlert>
</template>

<style scoped>
.workspace-state { min-width: 0; }
.workspace-state-loading {
  display: grid;
  gap: var(--co-space-3);
  padding: var(--co-space-4);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: var(--co-bg-surface);
}

.workspace-state-heading {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: var(--co-space-3);
}

.workspace-state-heading strong,
.workspace-state-heading span { display: block; }
.workspace-state-heading span { margin-top: var(--co-space-1); color: var(--co-text-muted); font-size: 12px; }
.workspace-state-spinner { color: var(--co-action-primary); animation: workspace-state-spin var(--co-spinner-duration) linear infinite; }
.workspace-state-skeleton { height: var(--co-table-row-height); border-radius: var(--co-radius-control); }
.workspace-state-description { display: grid; min-width: 0; gap: var(--co-space-3); }
.workspace-state-description p { margin: 0; overflow-wrap: anywhere; }
.workspace-state-description dl { display: grid; margin: 0; gap: var(--co-space-1); }
.workspace-state-description dl div { display: grid; grid-template-columns: 132px minmax(0, 1fr); gap: var(--co-space-2); }
.workspace-state-description dt {
  min-width: 0;
  color: var(--co-text-muted);
  overflow-wrap: anywhere;
}
.workspace-state-description dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  font-family: var(--co-font-mono);
}

.workspace-state-description ol { margin: var(--co-space-1) 0 0; padding-left: var(--co-space-5); }
.workspace-state-stale { border-left: var(--co-severity-marker-width) solid var(--co-status-inconclusive-fg); }

@keyframes workspace-state-spin { to { transform: rotate(360deg); } }

@media (prefers-reduced-motion: reduce) {
  .workspace-state-spinner { animation: none; }
}
</style>
