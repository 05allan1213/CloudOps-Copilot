<script setup lang="ts">
import { computed } from "vue";
import { Link as LinkIcon } from "@element-plus/icons-vue";

import { resourceProvenance } from "../../models/incidentResources";
import { safeExternalURL } from "../../models/workbench";
import type { DeliveryView, LoadState } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import JSONSnapshot from "./JSONSnapshot.vue";
import ResultBadge from "./ResultBadge.vue";

interface SectionProblem {
  message: string;
  requestID?: string;
  traceID?: string;
}

const props = withDefaults(defineProps<{
  state: LoadState;
  error?: SectionProblem | null;
  delivery: DeliveryView | null;
  refreshing?: boolean;
}>(), {
  error: null,
  refreshing: false,
});

const emit = defineEmits<{ retry: [] }>();
const pullRequestURL = computed(() => safeExternalURL(props.delivery?.pr_url));
const phases = computed(() => {
  const delivery = props.delivery;
  if (!delivery) return [];
  const mergeResult = delivery.merged_commit_sha
    ? "completed"
    : delivery.pr_state === "open" ? "pending" : "not_run";
  return [
    {
      index: "01",
      label: "Draft PR",
      result: delivery.pr_number ? (delivery.pr_state || "observed") : "not_run",
      detail: delivery.pr_number ? `PR #${delivery.pr_number} · ${delivery.head_branch}` : "No pull request projected",
    },
    {
      index: "02",
      label: "Required CI",
      result: delivery.ci_status || "not_run",
      detail: delivery.ci_status ? `Persisted CI status: ${delivery.ci_status}` : "CI status not projected",
    },
    {
      index: "03",
      label: "Human Merge",
      result: mergeResult,
      detail: delivery.merged_commit_sha ? "Merged commit observed" : "No merged commit observed",
    },
    {
      index: "04",
      label: "Argo Sync",
      result: delivery.argocd_sync_status || "not_run",
      detail: [delivery.argocd_sync_status, delivery.argocd_operation_phase, delivery.argocd_health_status].filter(Boolean).join(" · ") || "Argo observation not projected",
    },
    {
      index: "05",
      label: "Rollout",
      result: delivery.status || "not_run",
      detail: `${delivery.available_replicas}/${delivery.desired_replicas} available · ${delivery.unavailable_replicas} unavailable`,
    },
  ];
});
</script>

<template>
  <IncidentSectionShell
    id="delivery"
    title="Delivery Rail"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :retryable="true"
    empty-text="No persisted Delivery projection exists for this cycle."
    projection-note="Delivery observations stop at Git, CI, Argo, and rollout facts. Synced or Healthy does not mean the Incident is resolved."
    @retry="emit('retry')"
  >
    <article
      v-if="delivery"
      class="delivery"
    >
      <header class="delivery-header">
        <div>
          <span>Delivery Projection</span>
          <h3>{{ delivery.repository }}</h3>
          <p><code translate="no">{{ delivery.head_branch }}</code> · {{ resourceProvenance(delivery) }}</p>
        </div>
        <ResultBadge :result="delivery.status" />
      </header>

      <ol
        class="delivery-rail"
        aria-label="Persisted delivery phases"
      >
        <li
          v-for="phase in phases"
          :key="phase.index"
        >
          <span
            class="phase-index"
            aria-hidden="true"
          >{{ phase.index }}</span>
          <div>
            <strong>{{ phase.label }}</strong>
            <ResultBadge :result="phase.result" />
            <small>{{ phase.detail }}</small>
          </div>
        </li>
      </ol>

      <div class="delivery-actions">
        <a
          v-if="pullRequestURL"
          class="external-link"
          :href="pullRequestURL"
          target="_blank"
          rel="noopener noreferrer"
        >
          <el-icon aria-hidden="true">
            <LinkIcon />
          </el-icon>
          Open GitHub PR<span v-if="delivery.pr_number"> #{{ delivery.pr_number }}</span> in new tab
        </a>
        <span v-else>No safe HTTP pull-request link was projected.</span>
      </div>

      <section
        class="delivery-section"
        aria-labelledby="delivery-identities"
      >
        <h4 id="delivery-identities">
          Exact Delivery Identity
        </h4>
        <div class="hash-grid">
          <HashValue
            label="Base Revision"
            :value="delivery.base_revision"
          />
          <HashValue
            label="Commit SHA"
            :value="delivery.commit_sha"
          />
          <HashValue
            label="Merged Commit"
            :value="delivery.merged_commit_sha"
          />
          <HashValue
            label="Target Revision"
            :value="delivery.target_revision"
          />
          <HashValue
            label="Detected Revision"
            :value="delivery.detected_revision"
          />
          <HashValue
            label="Rollout Revision"
            :value="delivery.rollout_revision"
          />
        </div>
      </section>

      <div class="delivery-grid">
        <section aria-labelledby="delivery-runtime">
          <h4 id="delivery-runtime">
            Argo & Workload Facts
          </h4>
          <dl>
            <div><dt>Application / Project</dt><dd>{{ delivery.argocd_application || "Not projected" }} / {{ delivery.argocd_project || "Not projected" }}</dd></div>
            <div><dt>Target</dt><dd>{{ delivery.cluster || "Not projected" }} · {{ delivery.environment || "Not projected" }} · {{ delivery.namespace || "Not projected" }}</dd></div>
            <div><dt>Workload</dt><dd>{{ delivery.workload_kind || "Not projected" }}/{{ delivery.workload_name || "Not projected" }}</dd></div>
            <div><dt>Generation</dt><dd>{{ delivery.observed_generation ?? 0 }} observed / {{ delivery.deployment_generation ?? 0 }} desired</dd></div>
            <div><dt>Replicas</dt><dd>{{ delivery.available_replicas }} available · {{ delivery.updated_replicas }} updated · {{ delivery.unavailable_replicas }} unavailable / {{ delivery.desired_replicas }} desired</dd></div>
          </dl>
        </section>
        <section aria-labelledby="delivery-timing">
          <h4 id="delivery-timing">
            Persisted Timing
          </h4>
          <dl>
            <div><dt>Delivery Started</dt><dd>{{ formatIncidentTime(delivery.delivery_started_at) }}</dd></div>
            <div><dt>Deadline</dt><dd>{{ formatIncidentTime(delivery.delivery_deadline_at) }}</dd></div>
            <div><dt>Sync Started</dt><dd>{{ formatIncidentTime(delivery.sync_started_at) }}</dd></div>
            <div><dt>Sync Completed</dt><dd>{{ formatIncidentTime(delivery.sync_completed_at) }}</dd></div>
            <div><dt>Delivery Completed</dt><dd>{{ formatIncidentTime(delivery.delivery_completed_at) }}</dd></div>
            <div><dt>Last Observed</dt><dd>{{ formatIncidentTime(delivery.last_observed_at) }}</dd></div>
          </dl>
        </section>
      </div>

      <div
        v-if="delivery.failure_code || delivery.failure_reason"
        class="delivery-failure"
        role="status"
      >
        <strong>{{ delivery.failure_code || "Delivery blocked" }}</strong>
        <span>{{ delivery.failure_reason || "No bounded failure reason was projected." }}</span>
      </div>

      <JSONSnapshot
        v-if="delivery.resource_health !== undefined"
        title="Persisted Argo Resource Health"
        :value="delivery.resource_health"
      />
    </article>
  </IncidentSectionShell>
</template>

<style scoped>
.delivery {
  display: grid;
  min-width: 0;
  gap: var(--co-space-5);
}

.delivery-header,
.delivery-actions {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-4);
}

.delivery-header > div { min-width: 0; }
.delivery-header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.delivery-header h3 { margin: 3px 0; color: var(--co-text-primary); font-size: 18px; overflow-wrap: anywhere; }
.delivery-header p { margin: 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }

.delivery-rail {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  min-width: 0;
  margin: 0;
  padding: 0;
  border-block: 1px solid var(--co-border-default);
  list-style: none;
}

.delivery-rail li {
  display: grid;
  min-width: 0;
  grid-template-columns: 32px minmax(0, 1fr);
  gap: var(--co-space-2);
  padding: var(--co-space-4) var(--co-space-3);
}

.delivery-rail li + li { border-left: 1px solid var(--co-border-default); }
.phase-index { display: grid; width: 30px; height: 30px; place-items: center; border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-pill); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.delivery-rail li > div { display: grid; min-width: 0; align-content: start; justify-items: start; gap: var(--co-space-2); }
.delivery-rail strong { color: var(--co-text-primary); font-size: 13px; }
.delivery-rail small { color: var(--co-text-muted); overflow-wrap: anywhere; }

.delivery-actions { align-items: center; color: var(--co-text-muted); font-size: 12px; }
.external-link { display: inline-flex; min-height: 44px; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid var(--co-action-primary); border-radius: var(--co-radius-control); color: var(--co-action-primary); font-weight: 700; }
.external-link:hover { background: var(--co-bg-hover); }

.delivery-section,
.delivery-grid section { display: grid; min-width: 0; gap: var(--co-space-3); }
.delivery-section h4,
.delivery-grid h4 { margin: 0; color: var(--co-text-primary); font-size: 15px; }
.hash-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 var(--co-space-5); }
.delivery-grid { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-6); }
.delivery-grid dl { display: grid; margin: 0; }
.delivery-grid dl div { min-width: 0; padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-default); }
.delivery-grid dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.delivery-grid dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }

.delivery-failure { display: grid; gap: 2px; padding: var(--co-space-3) var(--co-space-4); border-left: 3px solid var(--co-status-critical-fg); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }

@media (max-width: 1050px) {
  .delivery-rail { grid-template-columns: minmax(0, 1fr); }
  .delivery-rail li + li { border-top: 1px solid var(--co-border-default); border-left: 0; }
}

@media (max-width: 760px) {
  .delivery-header,
  .delivery-actions { align-items: flex-start; flex-direction: column; }
  .hash-grid,
  .delivery-grid { grid-template-columns: minmax(0, 1fr); }
  .external-link { width: 100%; justify-content: center; }
}
</style>
