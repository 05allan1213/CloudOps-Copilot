<script setup lang="ts">
import { resourceTimestamp } from "../../models/incidents";
import type { LoadState, ResourceView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";

withDefaults(defineProps<{
  id: string;
  title: string;
  state: LoadState;
  error: string;
  items: ResourceView[];
  nextCursor?: string;
  emptyText?: string;
}>(), {
  nextCursor: "",
  emptyText: "No persisted V3 projection is available.",
});

defineEmits<{ loadMore: [] }>();
</script>

<template>
  <IncidentSectionShell
    :id="id"
    :title="title"
    :state="state"
    :error="error"
    :empty-text="emptyText"
  >
    <div class="resource-list">
      <article
        v-for="item in items"
        :key="item.id"
        class="resource-card"
      >
        <div class="resource-heading">
          <div>
            <span class="kind">{{ item.kind.replace(/_/g, " ") }}</span>
            <strong>{{ item.summary || "No bounded summary" }}</strong>
          </div>
          <IncidentStatusBadge
            v-if="item.status"
            :status="item.status"
          />
        </div>
        <dl>
          <div><dt>Public ID</dt><dd><code>{{ item.id }}</code></dd></div>
          <div><dt>Cycle / version</dt><dd>{{ item.cycle || "—" }} / {{ item.version || "—" }}</dd></div>
          <div><dt>Updated</dt><dd>{{ formatIncidentTime(resourceTimestamp(item)) }}</dd></div>
          <div class="hash">
            <dt>Authoritative hash</dt><dd><code>{{ item.hash || "Not projected" }}</code></dd>
          </div>
        </dl>
        <slot
          name="actions"
          :item="item"
        />
      </article>
    </div>
    <el-button
      v-if="nextCursor"
      class="load-more"
      @click="$emit('loadMore')"
    >
      Load next persisted page
    </el-button>
  </IncidentSectionShell>
</template>

<style scoped>
.resource-list { display: grid; gap: 12px; }
.resource-card { padding: 14px; border: 1px solid var(--cloudops-border-color); border-radius: 8px; background: var(--el-fill-color-lighter); }
.resource-heading { display: flex; justify-content: space-between; align-items: start; gap: 12px; }
.resource-heading > div { display: grid; gap: 5px; min-width: 0; }
.kind, dt { color: var(--el-text-color-secondary); font-size: 11px; text-transform: uppercase; }
.resource-heading strong { overflow-wrap: anywhere; }
dl { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; margin: 14px 0 0; }
dl div { min-width: 0; }
dd { margin: 4px 0 0; overflow-wrap: anywhere; font-size: 12px; }
.hash { grid-column: 1 / -1; }
code { font-size: 11px; }
.load-more { margin-top: 14px; }
</style>
