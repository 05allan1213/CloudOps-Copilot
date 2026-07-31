<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { X as Close } from "lucide-vue-next";

import type { CommandFeedback as CommandFeedbackState } from "../../models/commands";
import { resourceProvenance } from "../../models/incidentResources";
import { planDecisionAvailability } from "../../models/recovery";
import type { IncidentStatus, RemediationPlanView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import CodeDiff from "./CodeDiff.vue";
import CommandFeedback from "./CommandFeedback.vue";
import HashValue from "./HashValue.vue";
import JSONSnapshot from "./JSONSnapshot.vue";
import ResultBadge from "./ResultBadge.vue";

const props = defineProps<{
  plan: RemediationPlanView;
  incidentVersion: number;
  incidentStatus: IncidentStatus;
  commandPending: boolean;
  commandFeedback: CommandFeedbackState | null;
}>();

const emit = defineEmits<{
  decide: [plan: RemediationPlanView, decision: "approved" | "rejected", reason: string];
  retry: [];
  refresh: [];
}>();

const dialog = ref<HTMLDialogElement | null>(null);
const reasonInput = ref<HTMLTextAreaElement | null>(null);
const decision = ref<"approved" | "rejected">("approved");
const reason = ref("");
const reasonError = ref("");
const now = ref(Date.now());
let restoreFocusTo: HTMLElement | null = null;
let clockTimer: number | null = null;

const relevantFeedback = computed(() => props.commandFeedback?.resourceID === props.plan.id ? props.commandFeedback : null);
const decisionState = computed(() => planDecisionAvailability(props.plan, {
  incidentVersion: props.incidentVersion,
  incidentStatus: props.incidentStatus,
  nowMs: now.value,
  conflict: relevantFeedback.value?.state === "conflict",
}));
const effectiveDecisionState = computed(() => {
  const feedback = relevantFeedback.value;
  if (!feedback || feedback.state === "submitting" || feedback.state === "accepted") return decisionState.value;
  const reason = feedback.retryable
    ? "The previous submission may have an unknown outcome. Retry only with the same Idempotency-Key."
    : feedback.state === "conflict"
      ? decisionState.value.reason
      : `The previous command ended as ${feedback.state.replace(/_/g, " ")}. Refresh or resolve the reported condition before a new Decision.`;
  return { ...decisionState.value, available: false, reason };
});
const elementSuffix = computed(() => props.plan.id.replace(/[^a-zA-Z0-9_-]/g, "-"));
const dialogTitleID = computed(() => `decision-dialog-title-${elementSuffix.value}`);
const reasonID = computed(() => `decision-reason-${elementSuffix.value}`);
const reasonErrorID = computed(() => `decision-reason-error-${elementSuffix.value}`);
const reasonHelpID = computed(() => `decision-reason-help-${elementSuffix.value}`);

onMounted(() => {
  clockTimer = window.setInterval(() => { now.value = Date.now(); }, 30000);
});

onBeforeUnmount(() => {
  if (clockTimer !== null) window.clearInterval(clockTimer);
  if (dialog.value?.open) dialog.value.close();
});

watch(
  () => relevantFeedback.value?.state,
  (state) => {
    if (state === "accepted" && dialog.value?.open) closeDialog();
  },
);

async function openDialog(nextDecision: "approved" | "rejected", event: MouseEvent) {
  if (!effectiveDecisionState.value.available || props.commandPending) return;
  decision.value = nextDecision;
  reason.value = "";
  reasonError.value = "";
  restoreFocusTo = event.currentTarget instanceof HTMLElement ? event.currentTarget : null;
  await nextTick();
  dialog.value?.showModal();
  reasonInput.value?.focus({ preventScroll: true });
}

function closeDialog() {
  if (dialog.value?.open) dialog.value.close();
}

function onDialogClosed() {
  reason.value = "";
  reasonError.value = "";
  const target = restoreFocusTo;
  restoreFocusTo = null;
  target?.focus({ preventScroll: true });
}

function onDialogCancel(event: Event) {
  event.preventDefault();
  closeDialog();
}

function submitDecision() {
  if (!effectiveDecisionState.value.available || props.commandPending) return;
  const boundedReason = reason.value.trim();
  if (!boundedReason) {
    reasonError.value = "Enter the evidence-backed reason for this immutable Decision.";
    reasonInput.value?.focus();
    return;
  }
  if (boundedReason.length > 1024) {
    reasonError.value = "Keep the Decision reason within 1024 characters.";
    reasonInput.value?.focus();
    return;
  }
  reasonError.value = "";
  emit("decide", props.plan, decision.value, boundedReason);
}
</script>

<template>
  <article class="approval-panel">
    <header class="plan-header">
      <div>
        <span>Remediation Plan</span>
        <h3>{{ plan.patch_summary }}</h3>
        <p><code translate="no">{{ plan.target.repository }}</code> · <code translate="no">{{ plan.target.path }}</code> · {{ plan.target.field_ref }}</p>
      </div>
      <div class="plan-status">
        <ResultBadge :result="plan.status" />
        <ResultBadge
          :result="plan.risk_level"
          :label="`${plan.risk_level} risk`"
        />
      </div>
    </header>

    <dl class="plan-facts">
      <div><dt>Public Plan ID</dt><dd><code translate="no">{{ plan.id }}</code></dd></div>
      <div><dt>Cycle / Plan Version</dt><dd>{{ plan.cycle }} / {{ plan.plan_version }}</dd></div>
      <div><dt>Incident Version</dt><dd>{{ plan.incident_version }}</dd></div>
      <div><dt>Operation</dt><dd>{{ plan.operation_type.replace(/_/g, " ") }}</dd></div>
      <div><dt>Target Resource</dt><dd>{{ plan.target.resource.kind }}/{{ plan.target.resource.name }} · {{ plan.target.resource.namespace }}</dd></div>
      <div><dt>Base Branch</dt><dd><code translate="no">{{ plan.target.base_branch }}</code></dd></div>
      <div><dt>Policy Version</dt><dd><code translate="no">{{ plan.policy_version }}</code></dd></div>
      <div><dt>Expires</dt><dd><time :datetime="plan.expires_at">{{ formatIncidentTime(plan.expires_at) }}</time></dd></div>
      <div><dt>Provenance</dt><dd>{{ resourceProvenance(plan) }}</dd></div>
    </dl>

    <div
      v-if="effectiveDecisionState.expired || effectiveDecisionState.stale"
      class="expiry-warning"
      role="status"
    >
      <strong>{{ effectiveDecisionState.expired ? "Plan Expired" : "Plan Version Is Stale" }}</strong>
      <span>{{ effectiveDecisionState.reason }}</span>
    </div>

    <CodeDiff
      :value="plan.bounded_diff"
      :file-mode="plan.target.file_mode"
      :path="plan.target.path"
    />

    <section
      class="identity-section"
      :aria-labelledby="`plan-identity-${plan.id}`"
    >
      <div class="subsection-heading">
        <div>
          <span>Immutable Identity</span>
          <h4 :id="`plan-identity-${plan.id}`">
            Hashes Bound to This Decision
          </h4>
        </div>
        <small>Hash schema v{{ plan.hash_schema_version }}</small>
      </div>
      <div class="hash-grid">
        <HashValue
          label="Canonical Plan"
          :value="plan.canonical_plan_hash"
        />
        <HashValue
          label="Diagnosis"
          :value="plan.diagnosis_hash"
        />
        <HashValue
          label="Base Revision"
          :value="plan.target.base_revision"
        />
        <HashValue
          label="Last-known-good Revision"
          :value="plan.target.last_known_good_revision"
        />
        <HashValue
          label="Base Blob"
          :value="plan.target.base_blob_sha"
        />
        <HashValue
          label="Expected Before"
          :value="plan.expected_before_hash"
        />
        <HashValue
          label="Expected Post-image"
          :value="plan.expected_post_image_hash"
        />
        <HashValue
          label="Expected Tree"
          :value="plan.expected_tree_hash"
        />
        <HashValue
          label="Patch Manifest"
          :value="plan.proposed_patch_hash"
        />
        <HashValue
          label="Policy"
          :value="plan.policy_hash"
        />
        <HashValue
          label="Verification Plan"
          :value="plan.verification_plan_hash"
        />
        <HashValue
          label="Evidence Set"
          :value="plan.evidence_set_hash"
        />
      </div>
    </section>

    <div class="plan-notes">
      <section>
        <h4>Rollback Plan</h4>
        <p>{{ plan.rollback_plan }}</p>
      </section>
      <section>
        <h4>Validation Plan</h4>
        <p>{{ plan.validation_plan }}</p>
      </section>
    </div>

    <details class="contract-snapshots">
      <summary>Inspect Policy, Manifest &amp; Verification Snapshots</summary>
      <div>
        <JSONSnapshot
          title="Canonical Change Manifest"
          :value="plan.canonical_manifest"
        />
        <JSONSnapshot
          :title="`Policy Snapshot · ${plan.policy_version}`"
          :value="plan.policy_snapshot"
        />
        <JSONSnapshot
          title="Verification Plan"
          :value="plan.verification_plan"
        />
      </div>
    </details>

    <section
      class="evidence-bindings"
      :aria-labelledby="`plan-evidence-${plan.id}`"
    >
      <div class="subsection-heading">
        <div>
          <span>Approval-bound Evidence</span>
          <h4 :id="`plan-evidence-${plan.id}`">
            {{ plan.evidence_bindings.length }} Exact Binding{{ plan.evidence_bindings.length === 1 ? "" : "s" }}
          </h4>
        </div>
      </div>
      <ul>
        <li
          v-for="(binding, index) in plan.evidence_bindings"
          :key="binding.id"
        >
          <div class="evidence-binding-id">
            <span>Evidence ID</span>
            <code translate="no">{{ binding.id }}</code>
          </div>
          <HashValue
            :label="`Binding ${index + 1} Content Hash`"
            :value="binding.content_hash"
          />
        </li>
      </ul>
    </section>

    <section
      v-if="plan.decision"
      class="persisted-decision"
      :aria-labelledby="`persisted-decision-${plan.id}`"
    >
      <div class="subsection-heading">
        <div>
          <span>Immutable Owner Decision</span>
          <h4 :id="`persisted-decision-${plan.id}`">
            {{ plan.decision.decision }}
          </h4>
        </div>
        <ResultBadge :result="plan.decision.decision" />
      </div>
      <p>{{ plan.decision.reason }}</p>
      <dl class="decision-facts">
        <div><dt>Actor</dt><dd>{{ plan.decision.actor.login }} · {{ plan.decision.actor.role }}</dd></div>
        <div><dt>Authenticated</dt><dd>{{ formatIncidentTime(plan.decision.request_authenticated_at) }}</dd></div>
        <div><dt>Decision ID</dt><dd><code translate="no">{{ plan.decision.id }}</code></dd></div>
        <div><dt>Request ID</dt><dd><code translate="no">{{ plan.decision.request_id }}</code></dd></div>
      </dl>
      <details class="decision-bindings">
        <summary>Inspect Approved Hash Bindings</summary>
        <div class="hash-grid">
          <HashValue
            label="Approved Plan"
            :value="plan.decision.approved_plan_hash"
          />
          <HashValue
            label="Approved Base"
            :value="plan.decision.approved_base_sha"
          />
          <HashValue
            label="Approved Post-image"
            :value="plan.decision.approved_post_image_hash"
          />
          <HashValue
            label="Approved Tree"
            :value="plan.decision.approved_tree_hash"
          />
          <HashValue
            label="Approved Patch"
            :value="plan.decision.approved_patch_hash"
          />
          <HashValue
            label="Approved Policy"
            :value="plan.decision.approved_policy_hash"
          />
          <HashValue
            label="Approved Verification"
            :value="plan.decision.approved_verification_hash"
          />
          <HashValue
            label="Approved Evidence Set"
            :value="plan.decision.approved_evidence_set_hash"
          />
        </div>
      </details>
    </section>

    <section
      v-else
      class="decision-actions"
      aria-label="Remediation Plan Decision"
    >
      <div>
        <strong>Exact Decision Required</strong>
        <p>{{ effectiveDecisionState.reason || "Approve or reject this exact version and canonical hash." }}</p>
      </div>
      <div class="decision-buttons">
        <button
          type="button"
          class="approve-button"
          :disabled="!effectiveDecisionState.available || commandPending"
          @click="openDialog('approved', $event)"
        >
          Approve Exact Plan
        </button>
        <button
          type="button"
          class="reject-button"
          :disabled="!effectiveDecisionState.available || commandPending"
          @click="openDialog('rejected', $event)"
        >
          Reject Plan
        </button>
      </div>
    </section>

    <CommandFeedback
      :feedback="relevantFeedback"
      :pending="commandPending"
      @retry="emit('retry')"
      @refresh="emit('refresh')"
    />

    <dialog
      ref="dialog"
      class="decision-dialog"
      :aria-labelledby="dialogTitleID"
      @cancel="onDialogCancel"
      @close="onDialogClosed"
    >
      <header>
        <div>
          <span>Owner Command</span>
          <h4 :id="dialogTitleID">
            {{ decision === "approved" ? "Approve Exact Plan" : "Reject Plan" }}
          </h4>
        </div>
        <button
          type="button"
          class="dialog-close"
          aria-label="Close Decision dialog"
          @click="closeDialog"
        >
          <Close
            :size="16"
            aria-hidden="true"
          />
        </button>
      </header>
      <div class="dialog-body">
        <p>
          This submits Plan version <strong>{{ plan.version }}</strong> with canonical hash
          <code translate="no">{{ plan.canonical_plan_hash }}</code>. The server revalidates Origin, expiry, status, version, hash, and idempotency.
        </p>
        <label :for="reasonID">Decision Reason</label>
        <textarea
          :id="reasonID"
          ref="reasonInput"
          v-model="reason"
          name="decision_reason"
          autocomplete="off"
          maxlength="1024"
          rows="5"
          placeholder="Example: exact Evidence and rollback facts supporting this Decision…"
          :aria-invalid="reasonError ? 'true' : 'false'"
          :aria-describedby="reasonError ? reasonErrorID : reasonHelpID"
        />
        <small :id="reasonHelpID">{{ reason.length }} / 1024 characters · persisted with the Command</small>
        <p
          v-if="reasonError"
          :id="reasonErrorID"
          class="reason-error"
          role="alert"
        >
          {{ reasonError }}
        </p>
        <CommandFeedback
          :feedback="relevantFeedback"
          :pending="commandPending"
          @retry="emit('retry')"
          @refresh="emit('refresh')"
        />
        <div class="dialog-actions">
          <button
            type="button"
            :disabled="commandPending"
            @click="closeDialog"
          >
            Cancel
          </button>
          <button
            type="button"
            class="submit-decision"
            :class="{ 'submit-decision--reject': decision === 'rejected' }"
            :disabled="commandPending || !effectiveDecisionState.available"
            @click="submitDecision"
          >
            {{ commandPending ? "Submitting…" : decision === "approved" ? "Submit Approval" : "Submit Rejection" }}
          </button>
        </div>
      </div>
    </dialog>
  </article>
</template>

<style scoped>
.approval-panel {
  display: grid;
  min-width: 0;
  gap: var(--co-space-5);
  padding: var(--co-space-5);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.plan-header,
.plan-status,
.subsection-heading,
.decision-actions,
.decision-buttons,
.dialog-actions {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.plan-header > div:first-child { min-width: 0; }
.plan-header span,
.subsection-heading span,
.decision-dialog header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.plan-header h3 { margin: 3px 0; color: var(--co-text-primary); font-size: 18px; overflow-wrap: anywhere; }
.plan-header p { margin: 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.plan-status { flex: 0 0 auto; flex-wrap: wrap; justify-content: flex-end; }

.plan-facts,
.decision-facts {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--co-space-3);
  margin: 0;
}

.plan-facts div,
.decision-facts div { min-width: 0; padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-default); }
.plan-facts dt,
.decision-facts dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.plan-facts dd,
.decision-facts dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-secondary); font-size: 12px; overflow-wrap: anywhere; }

.expiry-warning {
  display: grid;
  gap: 2px;
  padding: var(--co-space-3) var(--co-space-4);
  border-left: 3px solid var(--co-status-warning-fg);
  color: var(--co-status-warning-fg);
  background: var(--co-status-warning-bg);
}

.identity-section,
.evidence-bindings,
.persisted-decision { display: grid; min-width: 0; gap: var(--co-space-3); }
.subsection-heading { align-items: center; }
.subsection-heading h4 { margin: 2px 0 0; font-size: 15px; text-transform: capitalize; }
.subsection-heading small { color: var(--co-text-muted); }
.hash-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 var(--co-space-5); }

.plan-notes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.plan-notes section { min-width: 0; padding: var(--co-space-4); border-left: 3px solid var(--co-action-primary); background: var(--co-bg-subtle); }
.plan-notes h4,
.plan-notes p { margin: 0; }
.plan-notes h4 { font-size: 13px; }
.plan-notes p { margin-top: var(--co-space-2); color: var(--co-text-secondary); white-space: pre-wrap; overflow-wrap: anywhere; }

.contract-snapshots { border-block: 1px solid var(--co-border-default); }
.decision-bindings { border-top: 1px solid color-mix(in srgb, currentcolor 28%, transparent); }
.contract-snapshots > summary,
.decision-bindings > summary { width: fit-content; min-height: 44px; padding: var(--co-space-3) 0; color: var(--co-action-primary); font-weight: 700; cursor: pointer; }
.decision-bindings > summary { color: inherit; }
.contract-snapshots > div { padding-bottom: var(--co-space-4); }

.evidence-bindings ul { display: grid; margin: 0; padding: 0; list-style: none; }
.evidence-bindings li { display: grid; grid-template-columns: minmax(180px, .5fr) minmax(0, 1fr); min-width: 0; align-items: center; gap: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.evidence-binding-id { display: grid; min-width: 0; gap: var(--co-space-1); padding: var(--co-space-2) 0; }
.evidence-binding-id span { color: var(--co-text-muted); font-size: 12px; font-weight: 600; }
.evidence-bindings code { min-width: 0; overflow-wrap: anywhere; font-size: 11px; }
.evidence-bindings :deep(.hash-value) { border-bottom: 0; }

.persisted-decision { padding: var(--co-space-4); border-left: 3px solid var(--co-status-success-fg); background: var(--co-status-success-bg); }
.persisted-decision > p { margin: 0; overflow-wrap: anywhere; }

.decision-actions { align-items: center; padding-top: var(--co-space-4); border-top: 1px solid var(--co-border-default); }
.decision-actions > div:first-child { min-width: 0; }
.decision-actions strong { color: var(--co-text-primary); }
.decision-actions p { margin: 2px 0 0; color: var(--co-text-muted); font-size: 12px; }
.decision-buttons { flex: 0 0 auto; }
.decision-buttons button,
.dialog-actions button { min-height: 44px; padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); font-weight: 700; cursor: pointer; }
.approve-button,
.submit-decision { border-color: var(--co-status-success-border) !important; color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.reject-button,
.submit-decision--reject { border-color: var(--co-status-critical-border) !important; color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.decision-buttons button:hover,
.dialog-actions button:hover { filter: brightness(.96); }
.decision-buttons button:disabled,
.dialog-actions button:disabled { cursor: not-allowed; filter: none; opacity: .58; }

.decision-dialog {
  width: min(620px, calc(100vw - 32px));
  max-width: none;
  max-height: min(780px, calc(100dvh - 32px));
  padding: 0;
  overflow: hidden;
  overscroll-behavior: contain;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-overlay);
  color: var(--co-text-primary);
  background: var(--co-bg-overlay);
  box-shadow: var(--co-shadow-overlay);
}

.decision-dialog::backdrop { background: rgb(0 0 0 / 56%); }
.decision-dialog > header { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: max(var(--co-space-4), env(safe-area-inset-top)) max(var(--co-space-5), env(safe-area-inset-right)) var(--co-space-4) max(var(--co-space-5), env(safe-area-inset-left)); border-bottom: 1px solid var(--co-border-default); }
.decision-dialog h4 { margin: 2px 0 0; font-size: 18px; }
.dialog-close { display: grid; width: 44px; height: 44px; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: transparent; cursor: pointer; }
.dialog-close:hover { background: var(--co-bg-hover); }
.dialog-body { display: grid; gap: var(--co-space-3); max-height: calc(100dvh - 104px - env(safe-area-inset-top)); padding: var(--co-space-5) max(var(--co-space-5), env(safe-area-inset-right)) max(var(--co-space-5), env(safe-area-inset-bottom)) max(var(--co-space-5), env(safe-area-inset-left)); overflow-y: auto; overscroll-behavior: contain; }
.dialog-body > p { margin: 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.dialog-body label { font-weight: 700; }
.dialog-body textarea { width: 100%; min-height: 132px; resize: vertical; padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); font-size: 16px; }
.dialog-body small { color: var(--co-text-muted); }
.reason-error { color: var(--co-status-critical-fg) !important; }
.dialog-actions { justify-content: flex-end; padding-top: var(--co-space-2); }
.dialog-actions button:first-child { color: var(--co-text-secondary); background: transparent; }

@media (max-width: 900px) {
  .plan-facts,
  .decision-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .hash-grid { grid-template-columns: minmax(0, 1fr); }
}

@media (max-width: 640px) {
  .approval-panel { padding: var(--co-space-4); }
  .plan-header,
  .decision-actions { align-items: flex-start; flex-direction: column; }
  .plan-status { justify-content: flex-start; }
  .plan-facts,
  .decision-facts,
  .plan-notes { grid-template-columns: minmax(0, 1fr); }
  .decision-buttons { display: grid; width: 100%; grid-template-columns: minmax(0, 1fr); }
  .decision-buttons button { width: 100%; }
  .evidence-bindings li { grid-template-columns: minmax(0, 1fr); padding: var(--co-space-2) 0; }
  .dialog-actions { display: grid; grid-template-columns: minmax(0, 1fr); }
  .dialog-actions button { width: 100%; }
}
</style>
