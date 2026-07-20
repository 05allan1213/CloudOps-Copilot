<script setup lang="ts">
import type { LoadState, RemediationPlanView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import JSONSnapshot from "./JSONSnapshot.vue";

withDefaults(defineProps<{
  state: LoadState;
  error: string;
  plans: RemediationPlanView[];
  nextCursor?: string;
  isOperator: boolean;
  commandPending: boolean;
}>(), { nextCursor: "" });

defineEmits<{
  loadMore: [];
  decide: [plan: RemediationPlanView, decision: "approved" | "rejected"];
}>();
</script>

<template>
  <IncidentSectionShell
    id="remediation-plans"
    title="Remediation plans"
    :state="state"
    :error="error"
    empty-text="No immutable remediation plan exists for this cycle."
  >
    <div class="plan-stack">
      <article
        v-for="plan in plans"
        :key="plan.id"
        class="plan"
      >
        <header class="plan__header">
          <div>
            <span class="eyebrow">Plan {{ plan.id }}</span>
            <h3>{{ plan.patch_summary }}</h3>
            <p>{{ plan.operation_type }} · {{ plan.target.path }} · {{ plan.target.field_ref }}</p>
          </div>
          <div class="status-stack">
            <IncidentStatusBadge :status="plan.status" />
            <el-tag effect="plain">
              Risk: {{ plan.risk_level }}
            </el-tag>
          </div>
        </header>

        <dl class="fact-grid">
          <div><dt>Repository</dt><dd>{{ plan.target.repository }} @ {{ plan.target.base_branch }}</dd></div>
          <div><dt>Resource</dt><dd>{{ plan.target.resource.kind }}/{{ plan.target.resource.name }}</dd></div>
          <div><dt>Namespace / container</dt><dd>{{ plan.target.resource.namespace }} / {{ plan.target.resource.container || "Not projected" }}</dd></div>
          <div><dt>Cycle / Plan version</dt><dd>{{ plan.cycle }} / {{ plan.plan_version }}</dd></div>
          <div><dt>Created by AgentRun</dt><dd><code>{{ plan.created_by_agent_run_id }}</code></dd></div>
          <div><dt>Expires</dt><dd>{{ formatIncidentTime(plan.expires_at) }}</dd></div>
        </dl>

        <section
          class="diff-block"
          :aria-labelledby="`diff-${plan.id}`"
        >
          <div class="subheading">
            <div>
              <span class="eyebrow">Complete bounded diff</span>
              <h4 :id="`diff-${plan.id}`">
                Approved artifact preview
              </h4>
            </div>
            <code>{{ plan.target.file_mode }}</code>
          </div>
          <pre tabindex="0"><code>{{ plan.bounded_diff }}</code></pre>
        </section>

        <section
          class="identity-block"
          :aria-labelledby="`identity-${plan.id}`"
        >
          <h4 :id="`identity-${plan.id}`">
            Immutable identity
          </h4>
          <HashValue
            label="Canonical plan"
            :value="plan.canonical_plan_hash"
          />
          <HashValue
            label="Diagnosis"
            :value="plan.diagnosis_hash"
          />
          <HashValue
            label="Expected before"
            :value="plan.expected_before_hash"
          />
          <HashValue
            label="Expected post-image"
            :value="plan.expected_post_image_hash"
          />
          <HashValue
            label="Expected tree"
            :value="plan.expected_tree_hash"
          />
          <HashValue
            label="Patch manifest"
            :value="plan.proposed_patch_hash"
          />
          <HashValue
            label="Policy"
            :value="plan.policy_hash"
          />
          <HashValue
            label="Verification plan"
            :value="plan.verification_plan_hash"
          />
          <HashValue
            label="Evidence set"
            :value="plan.evidence_set_hash"
          />
        </section>

        <div class="plan-notes">
          <div><strong>Rollback</strong><p>{{ plan.rollback_plan }}</p></div>
          <div><strong>Validation</strong><p>{{ plan.validation_plan }}</p></div>
        </div>

        <div class="snapshots">
          <JSONSnapshot
            title="Canonical change manifest"
            :value="plan.canonical_manifest"
          />
          <JSONSnapshot
            :title="`Policy snapshot · ${plan.policy_version}`"
            :value="plan.policy_snapshot"
          />
          <JSONSnapshot
            title="Verification plan"
            :value="plan.verification_plan"
          />
        </div>

        <section
          class="evidence-bindings"
          :aria-labelledby="`evidence-bindings-${plan.id}`"
        >
          <h4 :id="`evidence-bindings-${plan.id}`">
            Approval-bound Evidence
          </h4>
          <ul>
            <li
              v-for="binding in plan.evidence_bindings"
              :key="binding.id"
            >
              <code>{{ binding.id }}</code>
              <span>{{ binding.content_hash }}</span>
            </li>
          </ul>
        </section>

        <section
          v-if="plan.decision"
          class="decision"
          :aria-labelledby="`decision-${plan.id}`"
        >
          <div>
            <span class="eyebrow">Immutable operator decision</span>
            <h4 :id="`decision-${plan.id}`">
              {{ plan.decision.decision }}
            </h4>
          </div>
          <p>{{ plan.decision.reason }}</p>
          <dl class="decision-facts">
            <div><dt>Actor</dt><dd>{{ plan.decision.actor.login }} · {{ plan.decision.actor.role }}</dd></div>
            <div><dt>Authenticated</dt><dd>{{ formatIncidentTime(plan.decision.request_authenticated_at) }}</dd></div>
            <div><dt>Decision ID</dt><dd><code>{{ plan.decision.id }}</code></dd></div>
            <div><dt>Request ID</dt><dd><code>{{ plan.decision.request_id }}</code></dd></div>
          </dl>
        </section>

        <div
          v-else
          class="approval-row"
        >
          <p v-if="!isOperator">
            Viewer access is read-only. An operator must make the immutable decision.
          </p>
          <p v-else-if="plan.status !== 'awaiting_approval'">
            This Plan is not awaiting a decision.
          </p>
          <template v-else>
            <el-button
              type="success"
              :loading="commandPending"
              @click="$emit('decide', plan, 'approved')"
            >
              Approve exact plan
            </el-button>
            <el-button
              type="danger"
              plain
              :loading="commandPending"
              @click="$emit('decide', plan, 'rejected')"
            >
              Reject plan
            </el-button>
          </template>
        </div>
      </article>
    </div>
    <el-button
      v-if="nextCursor"
      class="load-more"
      @click="$emit('loadMore')"
    >
      Load next persisted page
    </el-button>
  </IncidentSectionShell>
</template>

<style scoped>
.plan-stack { display: grid; gap: 20px; }
.plan { min-width: 0; padding: 18px; border: 1px solid var(--cloudops-border-color); border-radius: 9px; background: var(--el-fill-color-lighter); }
.plan__header, .subheading { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.plan__header h3, .subheading h4 { margin: 4px 0; }
.plan__header p { margin: 0; color: var(--el-text-color-secondary); overflow-wrap: anywhere; }
.eyebrow, dt { color: var(--el-text-color-secondary); font-size: 11px; text-transform: uppercase; letter-spacing: .04em; }
.status-stack { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
.fact-grid, .decision-facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 12px; margin: 18px 0; }
.fact-grid div, .decision-facts div { min-width: 0; }
dd { margin: 4px 0 0; overflow-wrap: anywhere; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 11px; }
.diff-block { margin: 18px 0; }
.diff-block pre { max-height: 520px; margin: 10px 0 0; padding: 14px; overflow: auto; border: 1px solid var(--cloudops-border-color); border-radius: 7px; color: var(--el-text-color-primary); background: var(--cloudops-bg-card); white-space: pre; }
.diff-block pre:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
.identity-block { margin-top: 20px; }
.identity-block h4, .evidence-bindings h4 { margin: 0 0 8px; }
.plan-notes { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 12px; margin: 20px 0; }
.plan-notes > div { padding: 12px; border-left: 3px solid var(--el-color-primary); background: var(--cloudops-bg-card); }
.plan-notes p { margin: 6px 0 0; white-space: pre-wrap; }
.snapshots { display: grid; }
.evidence-bindings { margin-top: 18px; }
.evidence-bindings ul { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
.evidence-bindings li { display: grid; grid-template-columns: minmax(180px, .45fr) minmax(0, 1fr); gap: 12px; padding: 9px 0; border-bottom: 1px solid var(--cloudops-border-color); }
.evidence-bindings span { overflow-wrap: anywhere; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 11px; }
.decision { margin-top: 20px; padding: 14px; border: 1px solid var(--el-color-success-light-5); border-radius: 7px; background: var(--el-color-success-light-9); }
.decision h4 { margin: 2px 0 0; text-transform: capitalize; }
.decision p { margin: 12px 0 0; }
.approval-row { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 10px; margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--cloudops-border-color); }
.approval-row p { margin: 0 auto 0 0; color: var(--el-text-color-secondary); }
.load-more { margin-top: 16px; }
@media (max-width: 720px) { .plan { padding: 14px; } .plan__header, .subheading { flex-direction: column; } .status-stack { justify-content: flex-start; } .evidence-bindings li { grid-template-columns: minmax(0, 1fr); } .approval-row :deep(.el-button) { width: 100%; margin-left: 0; min-height: 44px; } }
</style>
