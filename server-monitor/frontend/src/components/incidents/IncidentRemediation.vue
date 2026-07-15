<script setup lang="ts">
import type { LoadState, RemediationDTO } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; remediation: RemediationDTO | null }>();
</script>

<template>
  <IncidentSectionShell
    id="remediation"
    title="Remediation and Approval"
    :state="state"
    :error="error"
    empty-text="No remediation proposal has been persisted."
  >
    <template v-if="remediation">
      <div class="section-row">
        <IncidentStatusBadge
          :status="remediation.status"
          :label="remediation.status.replace(/_/g, ' ')"
        /><el-tag type="warning">
          Risk: {{ remediation.risk_level }}
        </el-tag>
      </div>
      <el-descriptions
        :column="2"
        border
      >
        <el-descriptions-item label="Operation">
          {{ remediation.operation_type }}
        </el-descriptions-item>
        <el-descriptions-item label="Allowed target">
          {{ remediation.target.kind }}/{{ remediation.target.namespace }}/{{ remediation.target.name }}{{ remediation.target.container ? ` · ${remediation.target.container}` : "" }}
        </el-descriptions-item>
        <el-descriptions-item label="Approver">
          {{ remediation.approval_actor || "Unknown" }}
        </el-descriptions-item>
        <el-descriptions-item label="Decision time">
          {{ formatIncidentTime(remediation.approval_decided_at) }}
        </el-descriptions-item>
        <el-descriptions-item
          label="Patch summary"
          :span="2"
        >
          {{ remediation.patch_summary || "Unknown" }}
        </el-descriptions-item>
        <el-descriptions-item
          label="Rollback plan"
          :span="2"
        >
          {{ remediation.rollback_plan || "Unknown" }}
        </el-descriptions-item>
      </el-descriptions>
      <p class="read-only-note">
        This Workbench section is read-only. Existing approval APIs and RBAC remain unchanged.
      </p>
    </template>
  </IncidentSectionShell>
</template>

<style scoped>
.section-row { display: flex; gap: 8px; margin-bottom: 14px; }
.read-only-note { color: var(--el-text-color-secondary); font-size: 12px; margin-bottom: 0; }
</style>
