<script setup lang="ts">
import type { IncidentSignalDTO, LoadState } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; signals: IncidentSignalDTO[] }>();
</script>

<template>
  <IncidentSectionShell
    id="signals"
    title="Signals"
    :state="state"
    :error="error"
    empty-text="No bounded signal facts are available."
  >
    <el-table
      :data="signals"
      aria-label="Incident signals"
    >
      <el-table-column
        prop="status"
        label="Status"
        width="100"
      />
      <el-table-column
        prop="severity"
        label="Severity"
        width="100"
      />
      <el-table-column
        prop="category"
        label="Category"
        min-width="130"
      />
      <el-table-column
        prop="summary"
        label="Bounded summary"
        min-width="240"
      />
      <el-table-column
        label="Occurred (UTC)"
        min-width="190"
      >
        <template #default="{ row }">
          {{ formatIncidentTime(row.occurred_at) }}
        </template>
      </el-table-column>
    </el-table>
  </IncidentSectionShell>
</template>
