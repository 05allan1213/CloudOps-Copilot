<script setup lang="ts">
import type { LoadState, VerificationDetailDTO, VerificationRunDTO } from "../../types/incidents";
import { verificationRequirementLabel } from "../../models/incidents";
import { formatDuration, formatIncidentTime } from "../../utils/incidentTime";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; detail: VerificationDetailDTO | null; runs: VerificationRunDTO[] }>();

function observedText(value?: Record<string, string | number | boolean | null>): string {
  if (!value) return "Unknown";
  return Object.entries(value).map(([key, item]) => `${key}: ${String(item)}`).join(" · ");
}
</script>

<template>
  <IncidentSectionShell
    id="verification"
    title="Verification"
    :state="state"
    :error="error"
    empty-text="No VerificationRun has been persisted."
  >
    <template v-if="detail">
      <div class="verification-header">
        <div><strong>Attempt {{ detail.verification.attempt }}</strong><p>Exact delivery revision: <code>{{ detail.verification.target_revision }}</code></p></div><IncidentStatusBadge
          :status="detail.verification.status"
          :label="detail.verification.status"
        />
      </div>
      <el-table
        :data="detail.checks"
        aria-label="Verification checks"
      >
        <el-table-column
          label="Check"
          min-width="180"
        >
          <template #default="{ row }">
            <strong>{{ row.type }}</strong><small>{{ row.template_id || "Built-in typed check" }}</small>
          </template>
        </el-table-column>
        <el-table-column
          label="Requirement"
          width="105"
        >
          <template #default="{ row }">
            {{ verificationRequirementLabel(row.required) }}
          </template>
        </el-table-column>
        <el-table-column
          label="Status"
          width="115"
        >
          <template #default="{ row }">
            <IncidentStatusBadge
              :status="row.status"
              :label="row.status"
            />
          </template>
        </el-table-column>
        <el-table-column
          label="Comparison / threshold"
          min-width="155"
        >
          <template #default="{ row }">
            {{ row.comparison || "Server typed" }} {{ row.threshold ?? "—" }}
          </template>
        </el-table-column>
        <el-table-column
          label="Bounded observed value"
          min-width="230"
        >
          <template #default="{ row }">
            {{ observedText(row.observed) }}
          </template>
        </el-table-column>
        <el-table-column
          label="Stability"
          min-width="150"
        >
          <template #default="{ row }">
            {{ formatDuration(row.stability_progress_seconds) }} / {{ formatDuration(row.stability_window_seconds) }}
          </template>
        </el-table-column>
        <el-table-column
          label="Timeout"
          min-width="105"
        >
          <template #default="{ row }">
            {{ formatDuration(row.timeout_seconds) }}
          </template>
        </el-table-column>
      </el-table>
      <div class="run-history">
        <span
          v-for="run in runs"
          :key="run.id"
        >Attempt {{ run.attempt }}: <strong>{{ run.status }}</strong> · {{ formatIncidentTime(run.completed_at) }}</span>
      </div>
      <p class="authority-note">
        Comparison, stability, required/optional aggregate and final verdict are supplied by the server. The browser does not recalculate them.
      </p>
    </template>
  </IncidentSectionShell>
</template>

<style scoped>
.verification-header { display: flex; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.verification-header p { margin: 5px 0 0; }
small { display: block; color: var(--el-text-color-secondary); }
.run-history { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 14px; color: var(--el-text-color-secondary); font-size: 12px; }
.authority-note { color: var(--el-text-color-secondary); font-size: 12px; }
</style>
