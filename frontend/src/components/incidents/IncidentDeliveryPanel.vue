<script setup lang="ts">
import { computed } from "vue";
import { Link as LinkIcon } from "@element-plus/icons-vue";

import { safeExternalURL } from "../../models/workbench";
import type { DeliveryView, LoadState } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import JSONSnapshot from "./JSONSnapshot.vue";

const props = defineProps<{ state: LoadState; error: string; delivery: DeliveryView | null }>();
const pullRequestURL = computed(() => safeExternalURL(props.delivery?.pr_url));
</script>

<template>
  <IncidentSectionShell
    id="delivery"
    title="Delivery"
    :state="state"
    :error="error"
    empty-text="No ChangeRequest has been projected for this cycle."
  >
    <article
      v-if="delivery"
      class="delivery"
    >
      <header class="delivery__header">
        <div>
          <span class="eyebrow">ChangeRequest {{ delivery.id }}</span>
          <h3>{{ delivery.repository }}</h3>
          <p>{{ delivery.head_branch }}</p>
        </div>
        <IncidentStatusBadge :status="delivery.status" />
      </header>

      <ol
        class="delivery-rail"
        aria-label="Persisted delivery phases"
      >
        <li>
          <span>01</span>
          <div><strong>Draft PR</strong><small>{{ delivery.pr_state || "Not observed" }}</small></div>
        </li>
        <li>
          <span>02</span>
          <div><strong>Required CI</strong><small>{{ delivery.ci_status }}</small></div>
        </li>
        <li>
          <span>03</span>
          <div><strong>Argo CD</strong><small>{{ delivery.argocd_sync_status || "Not observed" }} / {{ delivery.argocd_health_status || "Not observed" }}</small></div>
        </li>
        <li>
          <span>04</span>
          <div><strong>Rollout</strong><small>{{ delivery.status }}</small></div>
        </li>
      </ol>

      <a
        v-if="pullRequestURL"
        class="external-link"
        :href="pullRequestURL"
        target="_blank"
        rel="noopener noreferrer"
      >
        <el-icon aria-hidden="true"><LinkIcon /></el-icon>
        Open GitHub PR<span v-if="delivery.pr_number"> #{{ delivery.pr_number }}</span>
      </a>

      <section
        class="delivery-section"
        aria-labelledby="delivery-identities"
      >
        <h4 id="delivery-identities">
          Exact identities
        </h4>
        <HashValue
          label="Base revision"
          :value="delivery.base_revision"
        />
        <HashValue
          label="Commit SHA"
          :value="delivery.commit_sha"
        />
        <HashValue
          label="Merged commit"
          :value="delivery.merged_commit_sha"
        />
        <HashValue
          label="Target revision"
          :value="delivery.target_revision"
        />
        <HashValue
          label="Detected revision"
          :value="delivery.detected_revision"
        />
        <HashValue
          label="Rollout revision"
          :value="delivery.rollout_revision"
        />
      </section>

      <section
        class="delivery-section"
        aria-labelledby="delivery-runtime"
      >
        <h4 id="delivery-runtime">
          Argo and workload facts
        </h4>
        <dl class="fact-grid">
          <div><dt>Application / Project</dt><dd>{{ delivery.argocd_application || "Not projected" }} / {{ delivery.argocd_project || "Not projected" }}</dd></div>
          <div><dt>Operation phase</dt><dd>{{ delivery.argocd_operation_phase || "Not observed" }}</dd></div>
          <div><dt>Target</dt><dd>{{ delivery.cluster || "—" }} · {{ delivery.environment || "—" }} · {{ delivery.namespace || "—" }}</dd></div>
          <div><dt>Workload</dt><dd>{{ delivery.workload_kind || "—" }}/{{ delivery.workload_name || "—" }}</dd></div>
          <div><dt>Generation</dt><dd>{{ delivery.observed_generation ?? 0 }} observed / {{ delivery.deployment_generation ?? 0 }} desired</dd></div>
          <div><dt>Replicas</dt><dd>{{ delivery.available_replicas }} available · {{ delivery.updated_replicas }} updated · {{ delivery.unavailable_replicas }} unavailable / {{ delivery.desired_replicas }} desired</dd></div>
        </dl>
      </section>

      <section
        class="delivery-section"
        aria-labelledby="delivery-timing"
      >
        <h4 id="delivery-timing">
          Persisted timing
        </h4>
        <dl class="fact-grid">
          <div><dt>Delivery started</dt><dd>{{ formatIncidentTime(delivery.delivery_started_at) }}</dd></div>
          <div><dt>Deadline</dt><dd>{{ formatIncidentTime(delivery.delivery_deadline_at) }}</dd></div>
          <div><dt>Sync started</dt><dd>{{ formatIncidentTime(delivery.sync_started_at) }}</dd></div>
          <div><dt>Sync completed</dt><dd>{{ formatIncidentTime(delivery.sync_completed_at) }}</dd></div>
          <div><dt>Delivery completed</dt><dd>{{ formatIncidentTime(delivery.delivery_completed_at) }}</dd></div>
          <div><dt>Last observed</dt><dd>{{ formatIncidentTime(delivery.last_observed_at) }}</dd></div>
        </dl>
      </section>

      <div
        v-if="delivery.failure_code || delivery.failure_reason"
        class="failure"
        role="status"
      >
        <strong>{{ delivery.failure_code || "Delivery blocked" }}</strong>
        <span>{{ delivery.failure_reason || "No bounded reason was projected." }}</span>
      </div>

      <JSONSnapshot
        v-if="delivery.resource_health !== undefined"
        title="Persisted Argo resource health"
        :value="delivery.resource_health"
      />
    </article>
  </IncidentSectionShell>
</template>

<style scoped>
.delivery { display: grid; gap: 20px; min-width: 0; }
.delivery__header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.delivery__header h3 { margin: 4px 0; }
.delivery__header p { margin: 0; color: var(--el-text-color-secondary); overflow-wrap: anywhere; }
.eyebrow, dt { color: var(--el-text-color-secondary); font-size: 11px; text-transform: uppercase; letter-spacing: .04em; }
.delivery-rail { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; padding: 0; list-style: none; border-block: 1px solid var(--cloudops-border-color); }
.delivery-rail li { position: relative; display: flex; gap: 10px; min-width: 0; padding: 14px; }
.delivery-rail li + li { border-left: 1px solid var(--cloudops-border-color); }
.delivery-rail li > span { display: grid; place-items: center; flex: 0 0 30px; height: 30px; border: 1px solid var(--cloudops-border-color); border-radius: 50%; font-size: 11px; font-weight: 700; }
.delivery-rail div { display: grid; min-width: 0; }
.delivery-rail small { color: var(--el-text-color-secondary); overflow-wrap: anywhere; text-transform: capitalize; }
.external-link { display: inline-flex; align-items: center; gap: 8px; width: fit-content; min-height: 44px; padding: 8px 12px; border: 1px solid var(--el-color-primary); border-radius: 7px; color: var(--el-color-primary); }
.external-link:hover, .external-link:focus-visible { background: var(--el-color-primary-light-9); outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
.delivery-section h4 { margin: 0 0 8px; }
.fact-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; margin: 0; }
.fact-grid div { min-width: 0; padding: 10px 0; border-bottom: 1px solid var(--cloudops-border-color); }
dd { margin: 4px 0 0; overflow-wrap: anywhere; }
.failure { display: grid; gap: 4px; padding: 12px; border-left: 4px solid var(--el-color-danger); color: var(--el-color-danger-dark-2); background: var(--el-color-danger-light-9); }
@media (max-width: 820px) { .delivery-rail { grid-template-columns: minmax(0, 1fr); } .delivery-rail li + li { border-top: 1px solid var(--cloudops-border-color); border-left: 0; } }
@media (max-width: 560px) { .delivery__header { flex-direction: column; } }
</style>
