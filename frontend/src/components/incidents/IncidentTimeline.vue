<script setup lang="ts">
import type { IncidentTimelineDTO, LoadState } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentSectionShell from "./IncidentSectionShell.vue";

defineProps<{ state: LoadState; error: string; items: IncidentTimelineDTO[]; total: number }>();
defineEmits<{ loadMore: [] }>();
</script>

<template>
  <IncidentSectionShell
    id="timeline"
    title="Timeline"
    :state="state"
    :error="error"
    empty-text="No persisted timeline events are available."
  >
    <ol
      class="timeline"
      aria-label="Incident timeline"
    >
      <li
        v-for="item in items"
        :key="item.key"
      >
        <span
          class="timeline-dot"
          aria-hidden="true"
        />
        <div><strong>{{ item.event_type.replace(/_/g, ' ') }}</strong><p>{{ item.summary || "Unknown" }}</p><small>{{ formatIncidentTime(item.occurred_at) }} · {{ item.actor_type }}</small></div>
      </li>
    </ol>
    <el-button
      v-if="items.length < total"
      class="load-more"
      @click="$emit('loadMore')"
    >
      Load more timeline facts
    </el-button>
  </IncidentSectionShell>
</template>

<style scoped>
.timeline { list-style: none; margin: 0; padding: 0; display: grid; gap: 16px; }
.timeline li { display: grid; grid-template-columns: 16px 1fr; gap: 10px; }
.timeline-dot { width: 10px; height: 10px; margin-top: 5px; border-radius: 50%; background: var(--el-color-primary); box-shadow: 0 0 0 4px var(--el-color-primary-light-9); }
p { margin: 4px 0; color: var(--el-text-color-regular); }
small { color: var(--el-text-color-secondary); }
.load-more { margin-top: 16px; }
</style>
