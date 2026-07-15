<script setup lang="ts">
import type { K8sDeploymentSummary, K8sEventSummary, K8sPodSummary, K8sServiceSummary } from "../../types";
import type { LoadState } from "../../types/incidents";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{
  state: LoadState;
  error: string;
  cluster: string;
  resources: { deployments: K8sDeploymentSummary[]; pods: K8sPodSummary[]; services: K8sServiceSummary[]; events: K8sEventSummary[] };
}>();
</script>

<template>
  <IncidentSectionShell
    id="resources"
    title="Related Resources"
    :state="state"
    :error="error"
    empty-text="No related typed Kubernetes summaries are available."
  >
    <p class="scope-note">
      Authenticated read-only context · cluster {{ cluster || "default" }}. No YAML, logs, Secrets, arbitrary GVR or mutation controls are exposed.
    </p>
    <h3>Deployments</h3>
    <el-table
      :data="resources.deployments"
      aria-label="Related Kubernetes deployments"
    >
      <el-table-column
        prop="namespace"
        label="Namespace"
      /><el-table-column
        prop="name"
        label="Deployment"
      /><el-table-column label="Rollout">
        <template #default="{ row }">
          {{ row.ready_replicas }}/{{ row.replicas }} ready · {{ row.updated_replicas }} updated
        </template>
      </el-table-column>
    </el-table>
    <h3>Pods</h3>
    <el-table
      :data="resources.pods"
      aria-label="Related Kubernetes pods"
    >
      <el-table-column
        prop="namespace"
        label="Namespace"
      /><el-table-column
        prop="name"
        label="Pod"
      /><el-table-column
        prop="phase"
        label="Phase"
      /><el-table-column label="Containers">
        <template #default="{ row }">
          {{ row.ready_containers }}/{{ row.total_containers }} ready
        </template>
      </el-table-column><el-table-column
        prop="restart_count"
        label="Restarts"
      />
    </el-table>
    <h3>Services</h3>
    <el-table
      :data="resources.services"
      aria-label="Related Kubernetes services"
    >
      <el-table-column
        prop="namespace"
        label="Namespace"
      /><el-table-column
        prop="name"
        label="Service"
      /><el-table-column
        prop="type"
        label="Type"
      />
    </el-table>
    <h3>Events</h3>
    <el-table
      :data="resources.events"
      aria-label="Related Kubernetes events"
    >
      <el-table-column
        prop="type"
        label="Type"
        width="100"
      /><el-table-column
        prop="reason"
        label="Reason"
      /><el-table-column
        prop="involved_kind"
        label="Kind"
      /><el-table-column
        prop="involved_name"
        label="Resource"
      /><el-table-column
        prop="message"
        label="Bounded summary"
        min-width="260"
      />
    </el-table>
  </IncidentSectionShell>
</template>

<style scoped>
.scope-note { color: var(--el-text-color-secondary); font-size: 12px; }
h3 { margin: 20px 0 8px; font-size: 14px; }
</style>
