<script setup lang="ts">
import type { DeliveryDTO, LoadState } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; delivery: DeliveryDTO | null }>();
</script>

<template>
  <IncidentSectionShell
    id="delivery"
    title="Delivery"
    :state="state"
    :error="error"
    empty-text="No delivery has been persisted."
  >
    <template v-if="delivery">
      <div class="status-row">
        <IncidentStatusBadge
          :status="delivery.status"
          :label="delivery.status.replace(/_/g, ' ')"
        /><IncidentStatusBadge
          :status="delivery.provenance_status"
          :label="delivery.provenance_status === 'verified' ? 'Verified provenance' : delivery.provenance_status === 'conflict' ? 'Provenance conflict' : 'Unverified provenance'"
        />
      </div>
      <el-descriptions
        :column="2"
        border
      >
        <el-descriptions-item label="Pull Request">
          <a
            v-if="delivery.pull_request_url"
            :href="delivery.pull_request_url"
            target="_blank"
            rel="noopener noreferrer"
          >#{{ delivery.pull_request }}</a><span v-else>Unknown</span>
        </el-descriptions-item>
        <el-descriptions-item label="CI">
          {{ delivery.ci_status || "Unknown" }}
        </el-descriptions-item>
        <el-descriptions-item label="Exact merged SHA">
          <code>{{ delivery.merged_commit_sha || "Unknown" }}</code>
        </el-descriptions-item>
        <el-descriptions-item label="Immutable image digest">
          <code>{{ delivery.image_digest || "Unknown" }}</code>
        </el-descriptions-item>
        <el-descriptions-item label="Argo revision">
          <code>{{ delivery.detected_revision || "Unknown" }}</code>
        </el-descriptions-item>
        <el-descriptions-item label="Argo sync / health">
          {{ delivery.argocd_sync_status || "Unknown" }} / {{ delivery.argocd_health_status || "Unknown" }}
        </el-descriptions-item>
        <el-descriptions-item label="Delivery attempts">
          {{ delivery.attempts }}
        </el-descriptions-item>
        <el-descriptions-item label="Completed">
          {{ formatIncidentTime(delivery.completed_at) }}
        </el-descriptions-item>
        <el-descriptions-item
          label="Rollout replicas"
          :span="2"
        >
          desired {{ delivery.desired_replicas }}, updated {{ delivery.updated_replicas }}, available {{ delivery.available_replicas }}, unavailable {{ delivery.unavailable_replicas }}
        </el-descriptions-item>
      </el-descriptions>
      <el-alert
        v-if="delivery.failure_reason"
        :title="delivery.failure_reason"
        type="error"
        :closable="false"
        class="delivery-alert"
      />
      <p class="authority-note">
        CI, Argo health, delivery and recovery verification are separate persisted statuses.
      </p>
    </template>
  </IncidentSectionShell>
</template>

<style scoped>
.status-row { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 14px; }
code { overflow-wrap: anywhere; }
.delivery-alert { margin-top: 12px; }
.authority-note { color: var(--el-text-color-secondary); font-size: 12px; margin-bottom: 0; }
</style>
