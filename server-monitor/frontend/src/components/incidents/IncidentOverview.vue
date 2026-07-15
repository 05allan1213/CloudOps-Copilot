<script setup lang="ts">
import type { IncidentDTO } from "../../types/incidents";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ incident: IncidentDTO }>();
</script>

<template>
  <IncidentSectionShell
    id="overview"
    title="Overview"
    state="ready"
  >
    <div class="summary-grid">
      <article
        v-for="(value, key) in incident.summary"
        :key="key"
      >
        <span>{{ key }}</span>
        <IncidentStatusBadge
          :status="value.status"
          :label="value.availability === 'forbidden' ? 'Restricted' : value.status.replace(/_/g, ' ')"
        />
      </article>
    </div>
    <p class="authority-note">
      These statuses are persisted server facts. The browser does not combine them into a lifecycle verdict.
    </p>
  </IncidentSectionShell>
</template>

<style scoped>
.summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 12px; }
article { display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 12px; border-radius: 8px; background: var(--el-fill-color-light); text-transform: capitalize; }
.authority-note { margin: 14px 0 0; color: var(--el-text-color-secondary); font-size: 12px; }
</style>
