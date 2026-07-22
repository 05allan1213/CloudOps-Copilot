<script setup lang="ts">
import type { CommandFeedback } from "../../models/commands";
import type { IncidentStatus, LoadState, RemediationPlanView } from "../../types/incidents";
import ApprovalPanel from "./ApprovalPanel.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";

interface SectionProblem {
  message: string;
  requestID?: string;
  traceID?: string;
}

withDefaults(defineProps<{
  state: LoadState;
  error?: SectionProblem | null;
  plans: RemediationPlanView[];
  nextCursor?: string;
  refreshing?: boolean;
  loadingMore?: boolean;
  incidentVersion: number;
  incidentStatus: IncidentStatus;
  isOperator: boolean;
  commandPending: boolean;
  commandFeedback: CommandFeedback | null;
}>(), {
  error: null,
  nextCursor: "",
  refreshing: false,
  loadingMore: false,
});

const emit = defineEmits<{
  loadMore: [];
  retryResource: [];
  decide: [plan: RemediationPlanView, decision: "approved" | "rejected", reason: string];
  retryCommand: [];
  refreshConflict: [];
}>();

function forwardDecision(plan: RemediationPlanView, decision: "approved" | "rejected", reason: string) {
  emit("decide", plan, decision, reason);
}
</script>

<template>
  <IncidentSectionShell
    id="remediation-plans"
    title="Remediation Plan & Approval"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :loading-more="loadingMore"
    :retryable="true"
    empty-text="No immutable remediation Plan exists. This can be correct for a no-change recovery."
    projection-note="The browser presents the complete bounded diff and server-projected identities. It does not generate YAML, hashes, policy decisions, or verdicts."
    @retry="emit('retryResource')"
  >
    <div class="plan-stack">
      <ApprovalPanel
        v-for="plan in plans"
        :key="plan.id"
        :plan="plan"
        :incident-version="incidentVersion"
        :incident-status="incidentStatus"
        :is-operator="isOperator"
        :command-pending="commandPending"
        :command-feedback="commandFeedback"
        @decide="forwardDecision"
        @retry="emit('retryCommand')"
        @refresh="emit('refreshConflict')"
      />
    </div>

    <button
      v-if="nextCursor"
      type="button"
      class="load-more"
      :disabled="loadingMore"
      @click="emit('loadMore')"
    >
      {{ loadingMore ? "Loading More Plans…" : "Load More Plans" }}
    </button>
  </IncidentSectionShell>
</template>

<style scoped>
.plan-stack {
  display: grid;
  min-width: 0;
  gap: var(--co-space-5);
}

.load-more {
  width: fit-content;
  min-height: 44px;
  padding: 0 var(--co-space-4);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-action-primary);
  background: var(--co-bg-surface);
  font-weight: 700;
  cursor: pointer;
}

.load-more:hover { border-color: var(--co-action-primary); background: var(--co-bg-hover); }
.load-more:disabled { cursor: wait; opacity: .65; }

@media (max-width: 640px) {
  .load-more { width: 100%; }
}
</style>
