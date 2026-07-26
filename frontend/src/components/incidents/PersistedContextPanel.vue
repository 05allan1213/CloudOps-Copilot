<script setup lang="ts">
import { computed } from "vue";
import { Box, Network as Connection, Rocket as Promotion } from "lucide-vue-next";

import {
  deterministicResourceSummary,
  isChangeContext,
  isKubernetesContext,
  resourceKindLabel,
  resourceTimestamp,
} from "../../models/incidentResources";
import type { ResourceView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import HashValue from "./HashValue.vue";
import ResultBadge from "./ResultBadge.vue";

const props = defineProps<{
  signals: ResourceView[];
  evidence: ResourceView[];
  timeline: ResourceView[];
}>();

const persistedContext = computed(() => [...props.evidence, ...props.signals]);
const kubernetesItem = computed(() => persistedContext.value.find(isKubernetesContext) ?? null);
const changeItem = computed(() => [...props.timeline].reverse().find(isChangeContext) ?? null);
</script>

<template>
  <section
    class="context-panel"
    aria-labelledby="persisted-context-title"
  >
    <div class="context-heading">
      <div>
        <h3 id="persisted-context-title">
          Persisted Runtime Context
        </h3>
        <p>Only MySQL-backed Signal, Timeline, and Evidence projections are shown.</p>
      </div>
      <span class="read-boundary">
        <el-icon aria-hidden="true"><Connection /></el-icon>
        No live cluster read
      </span>
    </div>

    <div class="context-grid">
      <article>
        <header>
          <span
            class="context-icon"
            aria-hidden="true"
          ><el-icon><Box /></el-icon></span>
          <div>
            <span>Kubernetes Snapshot</span>
            <strong>{{ kubernetesItem ? resourceKindLabel(kubernetesItem.kind) : "Not projected" }}</strong>
          </div>
          <ResultBadge
            v-if="kubernetesItem"
            :result="kubernetesItem.status || 'unknown'"
          />
        </header>
        <template v-if="kubernetesItem">
          <p>{{ deterministicResourceSummary(kubernetesItem) }}</p>
          <dl>
            <div><dt>Collected</dt><dd>{{ formatIncidentTime(resourceTimestamp(kubernetesItem)) }}</dd></div>
            <div><dt>Public ID</dt><dd><code translate="no">{{ kubernetesItem.id }}</code></dd></div>
          </dl>
          <HashValue
            label="Content hash"
            :value="kubernetesItem.hash"
          />
        </template>
        <p
          v-else
          class="context-empty"
        >
          The current generic Resource contract exposes no Kubernetes-tagged persisted item. The browser will not replace it with a live `/resources` query.
        </p>
      </article>

      <article>
        <header>
          <span
            class="context-icon context-icon--change"
            aria-hidden="true"
          ><el-icon><Promotion /></el-icon></span>
          <div>
            <span>Recent Exact Change</span>
            <strong>{{ changeItem ? resourceKindLabel(changeItem.kind) : "Not projected" }}</strong>
          </div>
          <ResultBadge
            v-if="changeItem"
            :result="changeItem.status || 'unknown'"
          />
        </header>
        <template v-if="changeItem">
          <p>{{ deterministicResourceSummary(changeItem) }}</p>
          <dl>
            <div><dt>Observed</dt><dd>{{ formatIncidentTime(resourceTimestamp(changeItem)) }}</dd></div>
            <div><dt>Public ID</dt><dd><code translate="no">{{ changeItem.id }}</code></dd></div>
          </dl>
          <HashValue
            label="Exact identity"
            :value="changeItem.hash"
          />
        </template>
        <p
          v-else
          class="context-empty"
        >
          No Timeline item identifies a persisted change or revision. Exact deployment identity remains unavailable rather than inferred from Incident text.
        </p>
      </article>
    </div>
  </section>
</template>

<style scoped>
.context-panel {
  display: grid;
  min-width: 0;
  gap: var(--co-space-4);
  padding: var(--co-space-6) 0;
  border-top: 1px solid var(--co-border-default);
}

.context-heading,
.context-heading > div,
.context-grid,
.context-grid article,
.context-grid header {
  min-width: 0;
}

.context-heading,
.context-grid header,
.read-boundary {
  display: flex;
  align-items: center;
}

.context-heading {
  justify-content: space-between;
  gap: var(--co-space-4);
}

.context-heading h3,
.context-heading p,
.context-grid p,
.context-grid dl {
  margin: 0;
}

.context-heading h3 { font-size: 18px; }
.context-heading p { margin-top: 2px; color: var(--co-text-muted); font-size: 12px; }

.read-boundary {
  flex: 0 0 auto;
  gap: 6px;
  padding: 3px 9px;
  border: 1px solid var(--co-status-neutral-border);
  border-radius: var(--co-radius-pill);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
  font-size: 11px;
  font-weight: 700;
}

.context-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.context-grid article {
  display: grid;
  align-content: start;
  gap: var(--co-space-4);
  padding: var(--co-space-4);
}

.context-grid article + article { border-left: 1px solid var(--co-border-default); }

.context-grid header {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr) auto;
  gap: var(--co-space-3);
}

.context-grid header > div { display: grid; min-width: 0; gap: 1px; }
.context-grid header span { color: var(--co-text-muted); font-size: 11px; text-transform: uppercase; }
.context-grid header strong { overflow-wrap: anywhere; font-size: 14px; }

.context-icon {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  border-radius: var(--co-radius-control);
  color: var(--co-status-info-fg) !important;
  background: var(--co-status-info-bg);
}

.context-icon--change { color: var(--co-status-warning-fg) !important; background: var(--co-status-warning-bg); }

.context-grid article > p {
  color: var(--co-text-secondary);
  overflow-wrap: anywhere;
}

.context-grid dl { display: grid; gap: var(--co-space-2); }
.context-grid dl div { display: grid; grid-template-columns: 88px minmax(0, 1fr); gap: var(--co-space-2); }
.context-grid dt { color: var(--co-text-muted); font-size: 11px; text-transform: uppercase; }
.context-grid dd { min-width: 0; margin: 0; overflow-wrap: anywhere; font-size: 12px; }

.context-empty {
  padding: var(--co-space-3);
  border-left: 3px solid var(--co-status-neutral-border);
  background: var(--co-bg-subtle);
  font-size: 13px;
}

@media (max-width: 900px) {
  .context-grid { grid-template-columns: minmax(0, 1fr); }
  .context-grid article + article { border-top: 1px solid var(--co-border-default); border-left: 0; }
}

@media (max-width: 560px) {
  .context-heading { align-items: flex-start; flex-direction: column; }
  .context-grid header { grid-template-columns: 36px minmax(0, 1fr); }
  .context-grid header > :last-child { grid-column: 2; justify-self: start; }
}
</style>
