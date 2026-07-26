<script setup lang="ts">
import { Clock } from "lucide-vue-next";

import type { SectionError } from "../../composables/incidents/useIncidentDetail";
import {
  deterministicResourceSummary,
  resourceKindLabel,
  resourceProvenance,
  resourceTimestamp,
} from "../../models/incidentResources";
import type { LoadState, ResourceView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentSectionShell from "./IncidentSectionShell.vue";
import ResultBadge from "./ResultBadge.vue";

withDefaults(defineProps<{
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
</script>

<template>
  <IncidentSectionShell
    id="timeline"
    title="Activity Timeline"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :loading-more="loadingMore"
    empty-text="No persisted Timeline event is available for this cycle."
    projection-note="Events stay in server order. The browser does not synthesize a sequence or reorder opaque public IDs."
    retryable
    @retry="emit('retry')"
  >
    <template #heading>
      <span class="timeline-count">{{ items.length }} event{{ items.length === 1 ? "" : "s" }} loaded</span>
    </template>

    <ol class="timeline-rail">
      <li
        v-for="item in items"
        :key="item.id"
      >
        <span
          class="timeline-marker"
          aria-hidden="true"
        >
          <el-icon><Clock /></el-icon>
        </span>
        <article>
          <div class="timeline-heading">
            <div>
              <span>{{ resourceKindLabel(item.kind) }}</span>
              <time :datetime="resourceTimestamp(item)">{{ formatIncidentTime(resourceTimestamp(item)) }}</time>
            </div>
            <ResultBadge :result="item.status || 'unknown'" />
          </div>

          <p>{{ deterministicResourceSummary(item) }}</p>

          <dl>
            <div>
              <dt>Public ID</dt>
              <dd><code translate="no">{{ item.id }}</code></dd>
            </div>
            <div>
              <dt>Content Hash</dt>
              <dd><code translate="no">{{ item.hash || "Not projected" }}</code></dd>
            </div>
            <div>
              <dt>Cycle / Version</dt>
              <dd>{{ item.cycle || "Not projected" }} / {{ item.version || "Not projected" }}</dd>
            </div>
            <div>
              <dt>Provenance</dt>
              <dd>{{ resourceProvenance(item) }}</dd>
            </div>
          </dl>
        </article>
      </li>
    </ol>

    <div
      v-if="loadingMore"
      class="timeline-tail-skeleton"
      role="status"
      aria-live="polite"
      aria-label="Loading more Timeline events"
    >
      <span />
      <span />
    </div>

    <div class="timeline-actions">
      <span>{{ nextCursor ? "More persisted events are available." : "Newest loaded event is the current Timeline tail." }}</span>
      <el-button
        v-if="nextCursor"
        :loading="loadingMore"
        :disabled="loadingMore"
        @click="emit('loadMore')"
      >
        {{ loadingMore ? "Loading More…" : "Load More Timeline Events" }}
      </el-button>
    </div>
  </IncidentSectionShell>
</template>

<style scoped>
.timeline-count,
.timeline-actions {
  color: var(--co-text-muted);
  font-size: 12px;
}

.timeline-rail {
  display: grid;
  min-width: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}

.timeline-rail li {
  position: relative;
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  min-width: 0;
  gap: var(--co-space-3);
  content-visibility: auto;
  contain-intrinsic-size: auto 176px;
}

.timeline-rail li:not(:last-child)::before {
  position: absolute;
  top: 32px;
  bottom: 0;
  left: 15px;
  width: 1px;
  background: var(--co-border-default);
  content: "";
}

.timeline-marker {
  z-index: 1;
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  border: 1px solid var(--co-status-neutral-border);
  border-radius: var(--co-radius-pill);
  color: var(--co-status-neutral-fg);
  background: var(--co-bg-canvas);
}

.timeline-rail article {
  display: grid;
  min-width: 0;
  gap: var(--co-space-3);
  margin-bottom: var(--co-space-4);
  padding: 0 0 var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
}

.timeline-heading,
.timeline-heading > div,
.timeline-actions {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.timeline-heading > div {
  align-items: flex-start;
  flex-direction: column;
  justify-content: flex-start;
  gap: 1px;
}

.timeline-heading span {
  color: var(--co-text-secondary);
  font-size: 12px;
  font-weight: 700;
}

.timeline-heading time {
  color: var(--co-text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.timeline-rail p {
  margin: 0;
  color: var(--co-text-primary);
  font-size: 14px;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.timeline-rail dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--co-space-2) var(--co-space-4);
  min-width: 0;
  margin: 0;
}

.timeline-rail dl div { min-width: 0; }
.timeline-rail dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.timeline-rail dd { min-width: 0; margin: 2px 0 0; color: var(--co-text-secondary); font-size: 11px; overflow-wrap: anywhere; }

.timeline-tail-skeleton {
  display: grid;
  gap: var(--co-space-3);
  padding-left: 44px;
}

.timeline-tail-skeleton span {
  height: 72px;
  border-radius: var(--co-radius-control);
  background: linear-gradient(90deg, var(--co-bg-subtle), var(--co-bg-hover), var(--co-bg-subtle));
  animation: timeline-pulse 1.4s ease-in-out infinite;
}

@keyframes timeline-pulse {
  50% { opacity: 0.55; }
}

@media (prefers-reduced-motion: reduce) {
  .timeline-tail-skeleton span { animation: none; }
}

@media (max-width: 640px) {
  .timeline-heading,
  .timeline-actions { align-items: flex-start; flex-direction: column; }
  .timeline-rail dl { grid-template-columns: minmax(0, 1fr); }
  .timeline-actions :deep(.el-button) { width: 100%; }
}
</style>
