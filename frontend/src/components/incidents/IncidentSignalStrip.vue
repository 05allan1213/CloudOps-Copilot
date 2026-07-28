<script setup lang="ts">
import { computed } from "vue";

import type { SectionError } from "../../composables/incidents/useIncidentDetail";
import { deterministicResourceSummary, resourceKindLabel, resourceTimestamp } from "../../models/incidentResources";
import type { LoadState, ResourceView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import ResultBadge from "./ResultBadge.vue";

const props = withDefaults(defineProps<{
  state: LoadState;
  error?: SectionError | null;
  items: ResourceView[];
  nextCursor?: string;
  refreshing?: boolean;
  loadingMore?: boolean;
}>(), {
  error: null,
  nextCursor: "",
  refreshing: false,
  loadingMore: false,
});

const emit = defineEmits<{
  loadMore: [];
  retry: [];
}>();

const primarySignals = computed(() => props.items.slice(0, 3));
</script>

<template>
  <IncidentSectionShell
    id="signals"
    title="Signal Summary"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :loading-more="loadingMore"
    empty-text="No persisted Signal is available for this cycle."
    projection-note="Signals are MySQL projections. The browser performs no live Kubernetes or observability query."
    retryable
    @retry="emit('retry')"
  >
    <div class="signal-strip">
      <article
        v-for="item in primarySignals"
        :key="item.id"
        class="signal-item"
      >
        <div class="signal-heading">
          <span>{{ resourceKindLabel(item.kind) }}</span>
          <ResultBadge
            :result="item.status || 'unknown'"
          />
        </div>
        <strong>{{ deterministicResourceSummary(item) }}</strong>
        <div class="signal-meta">
          <time :datetime="resourceTimestamp(item)">{{ formatIncidentTime(resourceTimestamp(item)) }}</time>
          <code translate="no">{{ item.id }}</code>
        </div>
      </article>
    </div>

    <details
      v-if="items.length > primarySignals.length"
      class="all-signals"
    >
      <summary>View all {{ items.length }} persisted Signals</summary>
      <ol>
        <li
          v-for="item in items"
          :key="item.id"
        >
          <div>
            <strong>{{ deterministicResourceSummary(item) }}</strong>
            <span>{{ resourceKindLabel(item.kind) }} · {{ formatIncidentTime(resourceTimestamp(item)) }}</span>
          </div>
          <ResultBadge :result="item.status || 'unknown'" />
        </li>
      </ol>
    </details>

    <div class="signal-actions">
      <span>{{ items.length }} loaded Signal{{ items.length === 1 ? "" : "s" }}</span>
      <el-button
        v-if="nextCursor"
        :loading="loadingMore"
        @click="emit('loadMore')"
      >
        {{ loadingMore ? "Loading More…" : "Load More Signals" }}
      </el-button>
    </div>
  </IncidentSectionShell>
</template>

<style scoped>
.signal-strip {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  min-width: 0;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.signal-item {
  display: grid;
  min-width: 0;
  align-content: start;
  gap: var(--co-space-3);
  padding: var(--co-space-4);
}

.signal-item + .signal-item { border-left: 1px solid var(--co-border-default); }

.signal-heading,
.signal-meta,
.signal-actions,
.all-signals li {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.signal-heading > span {
  color: var(--co-text-muted);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
}

.signal-item > strong {
  color: var(--co-text-primary);
  font-size: 14px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.signal-meta {
  align-items: flex-start;
  flex-direction: column;
  color: var(--co-text-muted);
  font-size: 11px;
}

.signal-meta code {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.all-signals {
  border-top: 1px solid var(--co-border-default);
}

.all-signals summary {
  width: fit-content;
  min-height: 44px;
  padding: var(--co-space-3) 0;
  color: var(--co-action-primary);
  cursor: pointer;
}

.all-signals ol {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}

.all-signals li {
  padding: var(--co-space-3) 0;
  border-top: 1px solid var(--co-border-default);
}

.all-signals li > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.all-signals li strong { overflow-wrap: anywhere; }
.all-signals li span { color: var(--co-text-muted); font-size: 11px; }

.signal-actions {
  color: var(--co-text-muted);
  font-size: 12px;
}

@media (max-width: 900px) {
  .signal-strip { grid-template-columns: minmax(0, 1fr); }
  .signal-item + .signal-item { border-top: 1px solid var(--co-border-default); border-left: 0; }
}

@media (max-width: 560px) {
  .signal-heading,
  .signal-actions,
  .all-signals li { align-items: flex-start; flex-direction: column; }
  .signal-actions :deep(.el-button) { width: 100%; }
}
</style>
