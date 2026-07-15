<script setup lang="ts">
import type { IncidentEvidenceDTO, LoadState } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; items: IncidentEvidenceDTO[] }>();
</script>

<template>
  <IncidentSectionShell
    id="evidence"
    title="Evidence"
    :state="state"
    :error="error"
    empty-text="No safe Evidence summaries are available."
  >
    <ul
      class="evidence-grid"
      aria-label="Incident evidence"
    >
      <li
        v-for="item in items"
        :key="item.id"
      >
        <div class="row">
          <code>{{ item.id }}</code><IncidentStatusBadge
            :status="item.state"
            :label="item.state"
          />
        </div>
        <strong>{{ item.type }} · {{ item.source }}</strong>
        <p>{{ item.summary || "Unknown" }}</p>
        <small>{{ item.resource_ref || "Unknown resource" }} · {{ formatIncidentTime(item.collected_at) }}</small>
        <small>Freshness: {{ item.data_freshness || "unknown" }} · Related claim: {{ item.related_claim || "Unknown" }}</small>
      </li>
    </ul>
  </IncidentSectionShell>
</template>

<style scoped>
.evidence-grid { list-style: none; padding: 0; margin: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; }
li { padding: 14px; border: 1px solid var(--el-border-color-lighter); border-radius: 8px; }
.row { display: flex; justify-content: space-between; gap: 8px; }
code { font-size: 11px; overflow-wrap: anywhere; }
p { margin: 8px 0; }
small { color: var(--el-text-color-secondary); }
</style>
