<script setup lang="ts">
import { computed } from "vue";

import type { CommandFeedback as CommandFeedbackState } from "../../models/commands";

const props = defineProps<{
  feedback: CommandFeedbackState | null;
  pending?: boolean;
}>();

const emit = defineEmits<{
  retry: [];
  refresh: [];
}>();

const stateLabel = computed(() => {
  if (!props.feedback) return "";
  if (props.feedback.state === "accepted") return props.feedback.idempotentReplay ? "Accepted · Replay" : `Accepted${props.feedback.httpStatus ? ` · ${props.feedback.httpStatus}` : ""}`;
  if (props.feedback.state === "submitting") return "Submitting";
  if (props.feedback.state === "forbidden") return "Forbidden";
  if (props.feedback.state === "conflict") return "Conflict";
  if (props.feedback.state === "invalid") return "Invalid Request";
  if (props.feedback.state === "unavailable") return "Unavailable";
  return "Failed";
});

const role = computed(() => {
  if (!props.feedback) return "status";
  return ["forbidden", "conflict", "invalid", "error"].includes(props.feedback.state) ? "alert" : "status";
});
</script>

<template>
  <section
    v-if="feedback"
    class="command-feedback"
    :class="`command-feedback--${feedback.state}`"
    :role="role"
    aria-live="polite"
  >
    <div class="feedback-heading">
      <strong>{{ feedback.action }} · {{ stateLabel }}</strong>
      <span v-if="feedback.code">{{ feedback.code }}</span>
    </div>
    <p>{{ feedback.message }}</p>
    <dl>
      <div v-if="feedback.httpStatus">
        <dt>HTTP</dt>
        <dd>{{ feedback.httpStatus }}</dd>
      </div>
      <div v-if="feedback.requestID">
        <dt>Request ID</dt>
        <dd><code translate="no">{{ feedback.requestID }}</code></dd>
      </div>
      <div v-if="feedback.traceID">
        <dt>Trace ID</dt>
        <dd><code translate="no">{{ feedback.traceID }}</code></dd>
      </div>
      <div v-if="feedback.idempotencyKey">
        <dt>Idempotency Key</dt>
        <dd><code translate="no">{{ feedback.idempotencyKey }}</code></dd>
      </div>
    </dl>
    <p
      v-if="feedback.state === 'conflict'"
      class="conflict-guidance"
    >
      This command is fail-closed. Refresh the current projection before trying again; the rejected payload is not replayed.
    </p>
    <div class="feedback-actions">
      <button
        v-if="feedback.retryable"
        type="button"
        :disabled="pending"
        @click="emit('retry')"
      >
        {{ pending ? "Retrying…" : "Retry With Same Idempotency Key" }}
      </button>
      <button
        v-if="feedback.state === 'conflict'"
        type="button"
        :disabled="pending"
        @click="emit('refresh')"
      >
        Refresh Current Projection
      </button>
    </div>
  </section>
</template>

<style scoped>
.command-feedback {
  display: grid;
  min-width: 0;
  gap: var(--co-space-2);
  padding: var(--co-space-3) var(--co-space-4);
  border-left: 3px solid var(--co-status-neutral-border);
  color: var(--co-text-secondary);
  background: var(--co-bg-subtle);
  font-size: 12px;
}

.command-feedback--accepted { border-left-color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.command-feedback--submitting { border-left-color: var(--co-action-primary); background: var(--co-bg-active); }
.command-feedback--forbidden,
.command-feedback--conflict,
.command-feedback--invalid,
.command-feedback--error { border-left-color: var(--co-status-critical-fg); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.command-feedback--unavailable { border-left-color: var(--co-status-warning-fg); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }

.feedback-heading,
.command-feedback dl,
.feedback-actions,
.command-feedback button { min-width: 0; }
.feedback-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.feedback-heading span { font-family: var(--co-font-mono); font-size: 11px; overflow-wrap: anywhere; }
.command-feedback p { margin: 0; overflow-wrap: anywhere; }
.command-feedback dl { display: flex; flex-wrap: wrap; gap: var(--co-space-2) var(--co-space-5); margin: 0; }
.command-feedback dl div { display: grid; min-width: 0; gap: 1px; }
.command-feedback dt { color: inherit; font-size: 10px; font-weight: 700; text-transform: uppercase; opacity: .76; }
.command-feedback dd { min-width: 0; margin: 0; overflow-wrap: anywhere; }
.conflict-guidance { font-weight: 650; }
.feedback-actions { display: flex; flex-wrap: wrap; gap: var(--co-space-2); }
.command-feedback button { width: fit-content; min-height: 44px; padding: 0 var(--co-space-3); border: 1px solid currentcolor; border-radius: var(--co-radius-control); color: inherit; background: transparent; cursor: pointer; }
.command-feedback button:hover { background: var(--co-bg-hover); }
.command-feedback button:disabled { cursor: wait; opacity: .65; }

@media (max-width: 767px) {
  .feedback-actions { display: grid; grid-template-columns: minmax(0, 1fr); }
  .command-feedback button { width: 100%; min-height: 44px; }
}
</style>
