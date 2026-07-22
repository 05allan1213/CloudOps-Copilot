<script setup lang="ts">
import { computed } from "vue";

import type { LoadState } from "../../types/incidents";

interface SectionProblem {
  message: string;
  requestID?: string;
  traceID?: string;
}

const props = withDefaults(defineProps<{
  id: string;
  title: string;
  state: LoadState;
  error?: string | SectionProblem | null;
  emptyText?: string;
  refreshing?: boolean;
  loadingMore?: boolean;
  projectionNote?: string;
  retryLabel?: string;
  retryable?: boolean;
}>(), {
  error: null,
  emptyText: "No persisted facts are available.",
  refreshing: false,
  loadingMore: false,
  projectionNote: "",
  retryLabel: "Retry",
  retryable: false,
});

const emit = defineEmits<{
  retry: [];
}>();

const errorMessage = computed(() => typeof props.error === "string" ? props.error : props.error?.message || "");
const requestID = computed(() => typeof props.error === "string" ? "" : props.error?.requestID || "");
const traceID = computed(() => typeof props.error === "string" ? "" : props.error?.traceID || "");
</script>

<template>
  <section
    :id="id"
    class="incident-section"
    :aria-labelledby="`${id}-title`"
    :aria-busy="state === 'loading' || refreshing || loadingMore"
  >
    <div class="incident-section__heading">
      <div class="section-heading-copy">
        <h3 :id="`${id}-title`">
          {{ title }}
        </h3>
        <span
          v-if="refreshing"
          class="refreshing-label"
          role="status"
          aria-live="polite"
        >
          Updating…
        </span>
      </div>
      <slot name="heading" />
    </div>

    <p
      v-if="projectionNote"
      class="projection-note"
    >
      {{ projectionNote }}
    </p>

    <div
      v-if="state === 'loading' && !refreshing"
      class="section-skeleton"
      role="status"
      aria-live="polite"
      :aria-label="`Loading ${title}`"
    >
      <span />
      <span />
      <span />
    </div>
    <div
      v-else-if="state === 'forbidden'"
      class="section-message section-message--warning"
      role="alert"
    >
      <strong>Viewer access was denied.</strong>
      <span>Use an identity with Incident viewer access, then retry this projection.</span>
      <button
        v-if="retryable"
        type="button"
        class="section-retry"
        @click="emit('retry')"
      >
        {{ retryLabel }}
      </button>
    </div>
    <div
      v-else-if="state === 'not_found'"
      class="section-message"
      role="status"
    >
      <strong>Projection not found.</strong>
      <span>{{ emptyText }}</span>
    </div>
    <div
      v-else-if="state === 'unavailable'"
      class="section-message section-message--warning"
      role="alert"
    >
      <strong>Projection unavailable.</strong>
      <span>{{ errorMessage || "The API dependency did not return this persisted projection." }}</span>
      <button
        v-if="retryable"
        type="button"
        class="section-retry"
        @click="emit('retry')"
      >
        {{ retryLabel }}
      </button>
    </div>
    <div
      v-else-if="state === 'error'"
      class="section-message section-message--error"
      role="alert"
    >
      <strong>Projection request failed.</strong>
      <span>{{ errorMessage || "Failed to load this section." }}</span>
      <small v-if="requestID || traceID">
        {{ requestID ? `Request ${requestID}` : "" }}{{ requestID && traceID ? " · " : "" }}{{ traceID ? `Trace ${traceID}` : "" }}
      </small>
      <button
        v-if="retryable"
        type="button"
        class="section-retry"
        @click="emit('retry')"
      >
        {{ retryLabel }}
      </button>
    </div>
    <div
      v-else-if="state === 'empty'"
      class="section-message"
      role="status"
    >
      <strong>{{ emptyText }}</strong>
      <span>No rows were returned for the current Incident cycle.</span>
    </div>
    <template v-else>
      <div
        v-if="errorMessage"
        class="stale-message"
        role="status"
      >
        The last refresh did not replace the visible projection: {{ errorMessage }}
      </div>
      <slot />
    </template>
  </section>
</template>

<style scoped>
.incident-section {
  display: grid;
  min-width: 0;
  gap: var(--co-space-4);
  scroll-margin-top: var(--co-space-6);
  padding: var(--co-space-6) 0;
  border-top: 1px solid var(--co-border-default);
}

.incident-section__heading,
.section-heading-copy {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: var(--co-space-3);
}

.incident-section__heading {
  justify-content: space-between;
}

.incident-section h3 {
  margin: 0;
  color: var(--co-text-primary);
  font-size: 18px;
}

.refreshing-label {
  color: var(--co-action-primary);
  font-size: 12px;
  font-weight: 700;
}

.projection-note,
.stale-message {
  margin: 0;
  color: var(--co-text-muted);
  font-size: 12px;
}

.stale-message {
  padding: var(--co-space-3);
  border-left: 3px solid var(--co-status-warning-fg);
  color: var(--co-status-warning-fg);
  background: var(--co-status-warning-bg);
}

.section-skeleton {
  display: grid;
  gap: var(--co-space-3);
}

.section-skeleton span {
  display: block;
  height: 42px;
  border-radius: var(--co-radius-control);
  background: linear-gradient(90deg, var(--co-bg-subtle), var(--co-bg-hover), var(--co-bg-subtle));
  background-size: 220% 100%;
  animation: section-shimmer 1.4s ease-in-out infinite;
}

.section-message {
  display: grid;
  gap: var(--co-space-1);
  padding: var(--co-space-4) 0;
  color: var(--co-text-secondary);
}

.section-message--warning { color: var(--co-status-warning-fg); }
.section-message--error { color: var(--co-status-critical-fg); }
.section-message small { overflow-wrap: anywhere; color: var(--co-text-muted); }

.section-retry {
  width: fit-content;
  min-height: 40px;
  margin-top: var(--co-space-2);
  padding: 0 var(--co-space-3);
  border: 1px solid currentcolor;
  border-radius: var(--co-radius-control);
  color: inherit;
  background: transparent;
  cursor: pointer;
}

.section-retry:hover { background: var(--co-bg-hover); }

@keyframes section-shimmer {
  to { background-position-x: -220%; }
}

@media (prefers-reduced-motion: reduce) {
  .section-skeleton span { animation: none; }
}

@media (max-width: 640px) {
  .incident-section { padding-block: var(--co-space-5); }
  .incident-section__heading { align-items: flex-start; }
  .section-heading-copy { align-items: flex-start; flex-direction: column; gap: 2px; }
}
</style>
