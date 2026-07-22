<script setup lang="ts">
import { computed, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { SectionError } from "../../composables/incidents/useIncidentDetail";
import {
  deterministicResourceSummary,
  resourceKindLabel,
  resourceProvenance,
  resourceStatusLabel,
  resourceTimestamp,
} from "../../models/incidentResources";
import type { LoadState, ResourceView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
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

const route = useRoute();
const router = useRouter();
const queryRunID = computed(() => queryValue(route.query.run));
const selectedRun = computed(() => props.items.find((item) => item.id === queryRunID.value) ?? props.items[0] ?? null);

function queryValue(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value;
  return typeof raw === "string" ? raw : "";
}

function replaceRunQuery(runID: string) {
  const query = { ...route.query };
  if (runID) query.run = runID;
  else delete query.run;
  void router.replace({ path: route.path, query, hash: route.hash });
}

function onRunSelect(event: Event) {
  replaceRunQuery((event.target as HTMLSelectElement).value);
}

watch(
  () => [props.items.length, queryRunID.value, selectedRun.value?.id],
  () => {
    if (props.items.length > 1 && selectedRun.value && queryRunID.value !== selectedRun.value.id) {
      replaceRunQuery(selectedRun.value.id);
    } else if (props.items.length <= 1 && queryRunID.value) {
      replaceRunQuery("");
    }
  },
  { immediate: true },
);
</script>

<template>
  <IncidentSectionShell
    id="agent-activity"
    title="Agent Activity"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :loading-more="loadingMore"
    empty-text="No bounded Investigation run is projected for this cycle."
    projection-note="Only the frozen generic Investigation Resource projection is shown. Private reasoning and unprojected AgentStep details remain unavailable."
    retryable
    @retry="emit('retry')"
  >
    <template #heading>
      <div
        v-if="items.length > 1"
        class="run-selector"
      >
        <label for="agent-run-select">Investigation Run</label>
        <select
          id="agent-run-select"
          name="investigation_run"
          autocomplete="off"
          :value="selectedRun?.id"
          @change="onRunSelect"
        >
          <option
            v-for="item in items"
            :key="item.id"
            :value="item.id"
          >
            {{ resourceStatusLabel(item.status) }} · {{ item.id }}
          </option>
        </select>
      </div>
    </template>

    <article
      v-if="selectedRun"
      class="agent-run"
    >
      <header>
        <div>
          <span>{{ resourceKindLabel(selectedRun.kind) }}</span>
          <h4>{{ deterministicResourceSummary(selectedRun) }}</h4>
        </div>
        <ResultBadge :result="selectedRun.status || 'unknown'" />
      </header>

      <dl class="run-facts">
        <div><dt>Public Run ID</dt><dd><code translate="no">{{ selectedRun.id }}</code></dd></div>
        <div><dt>Cycle / Version</dt><dd>{{ selectedRun.cycle || "Not projected" }} / {{ selectedRun.version || "Not projected" }}</dd></div>
        <div><dt>Created</dt><dd><time :datetime="selectedRun.created_at">{{ formatIncidentTime(selectedRun.created_at) }}</time></dd></div>
        <div><dt>Updated</dt><dd><time :datetime="resourceTimestamp(selectedRun)">{{ formatIncidentTime(resourceTimestamp(selectedRun)) }}</time></dd></div>
        <div><dt>Provenance</dt><dd>{{ resourceProvenance(selectedRun) }}</dd></div>
      </dl>

      <HashValue
        label="Run projection hash"
        :value="selectedRun.hash"
      />

      <section
        class="projection-boundary"
        aria-labelledby="agent-projection-boundary-title"
      >
        <div>
          <h4 id="agent-projection-boundary-title">
            Current Contract Boundary
          </h4>
          <p>These fields are not present in the generic Resource contract and are not inferred from summary text.</p>
        </div>
        <dl>
          <div><dt>Step Tool &amp; Purpose</dt><dd>Not projected</dd></div>
          <div><dt>Retry &amp; Attempt Detail</dt><dd>Not projected</dd></div>
          <div><dt>Budget / Token Usage</dt><dd>Not projected</dd></div>
          <div><dt>Evidence Links &amp; Claims</dt><dd>Not projected</dd></div>
        </dl>
      </section>
    </article>

    <div class="agent-actions">
      <span>{{ items.length }} Investigation run{{ items.length === 1 ? "" : "s" }} loaded</span>
      <el-button
        v-if="nextCursor"
        :loading="loadingMore"
        :disabled="loadingMore"
        @click="emit('loadMore')"
      >
        {{ loadingMore ? "Loading More…" : "Load More Investigation Runs" }}
      </el-button>
    </div>
  </IncidentSectionShell>
</template>

<style scoped>
.run-selector {
  display: grid;
  min-width: min(360px, 100%);
  gap: 2px;
}

.run-selector label {
  color: var(--co-text-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.run-selector select {
  width: 100%;
  max-width: 100%;
  min-width: 0;
  min-height: 40px;
  overflow: hidden;
  padding: 0 34px 0 var(--co-space-3);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-text-primary);
  background-color: var(--co-bg-surface);
  font-family: var(--co-font-mono);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-run {
  display: grid;
  min-width: 0;
  gap: var(--co-space-4);
  padding: var(--co-space-4);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.agent-run > header,
.agent-actions {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-4);
}

.agent-run > header > div { min-width: 0; }
.agent-run > header span { color: var(--co-text-muted); font-size: 11px; font-weight: 700; text-transform: uppercase; }
.agent-run h4 { margin: var(--co-space-1) 0 0; color: var(--co-text-primary); font-size: 16px; line-height: 1.45; overflow-wrap: anywhere; }

.run-facts,
.projection-boundary dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--co-space-3) var(--co-space-5);
  min-width: 0;
  margin: 0;
}

.run-facts div,
.projection-boundary dl div { min-width: 0; }
.run-facts dt,
.projection-boundary dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.run-facts dd,
.projection-boundary dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-secondary); font-size: 12px; overflow-wrap: anywhere; }

.projection-boundary {
  display: grid;
  grid-template-columns: minmax(180px, .65fr) minmax(0, 1.35fr);
  gap: var(--co-space-5);
  padding: var(--co-space-4);
  border-left: 3px solid var(--co-status-neutral-border);
  background: var(--co-bg-subtle);
}

.projection-boundary h4,
.projection-boundary p { margin: 0; }
.projection-boundary h4 { font-size: 13px; }
.projection-boundary p { margin-top: 3px; color: var(--co-text-muted); font-size: 12px; }
.projection-boundary dd { font-weight: 650; }

.agent-actions { align-items: center; color: var(--co-text-muted); font-size: 12px; }

@media (max-width: 767px) {
  .run-selector { min-width: 0; width: 100%; }
  .run-selector select { min-height: 44px; font-size: 16px; }
  .agent-run > header,
  .agent-actions { flex-direction: column; }
  .run-facts,
  .projection-boundary,
  .projection-boundary dl { grid-template-columns: minmax(0, 1fr); }
  .agent-actions :deep(.el-button) { width: 100%; }
}
</style>
