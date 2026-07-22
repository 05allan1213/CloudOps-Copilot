<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { Close } from "@element-plus/icons-vue";
import { useRoute, useRouter } from "vue-router";

import type { SectionError } from "../../composables/incidents/useIncidentDetail";
import {
  deterministicResourceSummary,
  evidenceStateDefinition,
  resourceKindLabel,
  resourceProvenance,
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
const drawer = ref<HTMLDialogElement | null>(null);
const closeButton = ref<HTMLButtonElement | null>(null);
let restoreFocusTo: HTMLElement | null = null;

const queryEvidenceID = computed(() => queryValue(route.query.evidence));
const selectedEvidence = computed(() => props.items.find((item) => item.id === queryEvidenceID.value) ?? null);
const selectedState = computed(() => evidenceStateDefinition(selectedEvidence.value?.status));

function queryValue(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value;
  return typeof raw === "string" ? raw : "";
}

function replaceEvidenceQuery(evidenceID: string) {
  const query = { ...route.query };
  if (evidenceID) query.evidence = evidenceID;
  else delete query.evidence;
  return router.replace({ path: route.path, query, hash: window.location.hash });
}

function openEvidence(item: ResourceView, event: MouseEvent) {
  restoreFocusTo = event.currentTarget instanceof HTMLElement ? event.currentTarget : null;
  void replaceEvidenceQuery(item.id);
}

function closeEvidence() {
  void replaceEvidenceQuery("");
}

function onDrawerCancel(event: Event) {
  event.preventDefault();
  closeEvidence();
}

function onDrawerClosed() {
  if (queryEvidenceID.value) void replaceEvidenceQuery("");
  const target = restoreFocusTo;
  restoreFocusTo = null;
  target?.focus({ preventScroll: true });
}

watch(
  () => [queryEvidenceID.value, selectedEvidence.value?.id],
  async () => {
    await nextTick();
    if (selectedEvidence.value && drawer.value && !drawer.value.open) {
      drawer.value.showModal();
      closeButton.value?.focus({ preventScroll: true });
    } else if (!selectedEvidence.value && drawer.value?.open) {
      drawer.value.close();
    }
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  if (drawer.value?.open) drawer.value.close();
});
</script>

<template>
  <IncidentSectionShell
    id="evidence"
    title="Evidence"
    :state="state"
    :error="error"
    :refreshing="refreshing"
    :loading-more="loadingMore"
    empty-text="No persisted Evidence is available for this cycle."
    projection-note="State, summary, hash, timestamps, and provenance come from the generic Resource projection. Trust, authority, and claim use are explicit contract gaps."
    retryable
    @retry="emit('retry')"
  >
    <template #heading>
      <span class="evidence-count">{{ items.length }} item{{ items.length === 1 ? "" : "s" }} loaded</span>
    </template>

    <div class="evidence-desktop">
      <table>
        <caption class="visually-hidden">
          Persisted Evidence projections for this Incident cycle
        </caption>
        <thead>
          <tr>
            <th scope="col">
              State
            </th>
            <th scope="col">
              Type
            </th>
            <th scope="col">
              Summary
            </th>
            <th scope="col">
              Hash
            </th>
            <th scope="col">
              Updated
            </th>
            <th scope="col">
              Provenance
            </th>
            <th scope="col">
              <span class="visually-hidden">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.id"
          >
            <td><ResultBadge :result="item.status || 'unknown'" /></td>
            <td>{{ resourceKindLabel(item.kind) }}</td>
            <td class="summary-cell">
              {{ deterministicResourceSummary(item) }}
            </td>
            <td><code translate="no">{{ item.hash || "Not projected" }}</code></td>
            <td><time :datetime="resourceTimestamp(item)">{{ formatIncidentTime(resourceTimestamp(item)) }}</time></td>
            <td>{{ resourceProvenance(item) }}</td>
            <td>
              <button
                type="button"
                class="inspect-button"
                :aria-label="`Inspect Evidence ${item.id}`"
                @click="openEvidence(item, $event)"
              >
                Inspect
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="evidence-mobile">
      <article
        v-for="item in items"
        :key="item.id"
        class="evidence-mobile-row"
      >
        <header>
          <div>
            <span>{{ resourceKindLabel(item.kind) }}</span>
            <strong>{{ deterministicResourceSummary(item) }}</strong>
          </div>
          <ResultBadge :result="item.status || 'unknown'" />
        </header>
        <dl>
          <div><dt>Updated</dt><dd>{{ formatIncidentTime(resourceTimestamp(item)) }}</dd></div>
          <div><dt>Hash</dt><dd><code translate="no">{{ item.hash || "Not projected" }}</code></dd></div>
          <div><dt>Provenance</dt><dd>{{ resourceProvenance(item) }}</dd></div>
        </dl>
        <button
          type="button"
          class="inspect-button"
          @click="openEvidence(item, $event)"
        >
          Inspect Evidence
        </button>
      </article>
    </div>

    <div
      v-if="loadingMore"
      class="evidence-tail-skeleton"
      role="status"
      aria-live="polite"
      aria-label="Loading more Evidence"
    >
      <span />
      <span />
    </div>

    <div class="evidence-actions">
      <span>{{ nextCursor ? "More persisted Evidence is available." : "All returned Evidence pages are loaded." }}</span>
      <el-button
        v-if="nextCursor"
        :loading="loadingMore"
        :disabled="loadingMore"
        @click="emit('loadMore')"
      >
        {{ loadingMore ? "Loading More…" : "Load More Evidence" }}
      </el-button>
    </div>

    <dialog
      ref="drawer"
      class="evidence-drawer"
      aria-labelledby="evidence-drawer-title"
      @cancel="onDrawerCancel"
      @close="onDrawerClosed"
    >
      <template v-if="selectedEvidence">
        <header class="drawer-header">
          <div>
            <span>Evidence Detail</span>
            <h4 id="evidence-drawer-title">
              {{ resourceKindLabel(selectedEvidence.kind) }}
            </h4>
          </div>
          <button
            ref="closeButton"
            type="button"
            class="drawer-close"
            aria-label="Close Evidence detail"
            @click="closeEvidence"
          >
            <el-icon aria-hidden="true">
              <Close />
            </el-icon>
          </button>
        </header>

        <div class="drawer-body">
          <div class="evidence-state-callout">
            <ResultBadge
              :result="selectedEvidence.status || 'unknown'"
              :label="selectedState.label"
            />
            <p>{{ selectedState.description }}</p>
          </div>

          <section aria-labelledby="evidence-summary-title">
            <h5 id="evidence-summary-title">
              Bounded Summary
            </h5>
            <p>{{ deterministicResourceSummary(selectedEvidence) }}</p>
          </section>

          <dl class="drawer-facts">
            <div><dt>Cycle / Version</dt><dd>{{ selectedEvidence.cycle || "Not projected" }} / {{ selectedEvidence.version || "Not projected" }}</dd></div>
            <div><dt>Created</dt><dd>{{ formatIncidentTime(selectedEvidence.created_at) }}</dd></div>
            <div><dt>Updated</dt><dd>{{ formatIncidentTime(resourceTimestamp(selectedEvidence)) }}</dd></div>
            <div><dt>Provenance</dt><dd>{{ resourceProvenance(selectedEvidence) }}</dd></div>
          </dl>

          <HashValue
            label="Public Evidence ID"
            :value="selectedEvidence.id"
          />
          <HashValue
            label="Content hash"
            :value="selectedEvidence.hash"
          />

          <section
            class="trust-boundary"
            aria-labelledby="evidence-trust-boundary-title"
          >
            <h5 id="evidence-trust-boundary-title">
              Trust &amp; Claim Boundary
            </h5>
            <dl>
              <div><dt>Trust Classification</dt><dd>Not projected</dd></div>
              <div><dt>Authority</dt><dd>Not projected</dd></div>
              <div><dt>Claim Use</dt><dd>Not projected</dd></div>
            </dl>
            <p>The browser does not derive these fields from the kind, status, hash, or summary.</p>
          </section>
        </div>
      </template>
    </dialog>
  </IncidentSectionShell>
</template>

<style scoped>
.evidence-count,
.evidence-actions {
  color: var(--co-text-muted);
  font-size: 12px;
}

.evidence-desktop {
  min-width: 0;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  overflow: hidden;
  background: var(--co-bg-surface);
}

table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  font-size: 12px;
}

th,
td {
  min-width: 0;
  padding: var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
  text-align: left;
  vertical-align: top;
  overflow-wrap: anywhere;
}

th {
  color: var(--co-text-muted);
  background: var(--co-bg-subtle);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

th:nth-child(1) { width: 12%; }
th:nth-child(2) { width: 11%; }
th:nth-child(3) { width: 25%; }
th:nth-child(4) { width: 17%; }
th:nth-child(5) { width: 13%; }
th:nth-child(6) { width: 14%; }
th:nth-child(7) { width: 8%; }
tbody tr:last-child td { border-bottom: 0; }
tbody tr { content-visibility: auto; contain-intrinsic-size: auto 88px; }
td code { font-size: 10px; }
.summary-cell { color: var(--co-text-primary); line-height: 1.45; }
.evidence-desktop :deep(.result-badge) { white-space: nowrap; }

.inspect-button {
  min-height: 40px;
  padding: 0 var(--co-space-3);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-action-primary);
  background: transparent;
  font-weight: 700;
  cursor: pointer;
}

.inspect-button:hover { border-color: var(--co-action-primary); background: var(--co-bg-hover); }

.evidence-mobile { display: none; }

.evidence-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.evidence-tail-skeleton {
  display: grid;
  gap: var(--co-space-3);
}

.evidence-tail-skeleton span {
  height: 58px;
  border-radius: var(--co-radius-control);
  background: linear-gradient(90deg, var(--co-bg-subtle), var(--co-bg-hover), var(--co-bg-subtle));
  animation: evidence-pulse 1.4s ease-in-out infinite;
}

.evidence-drawer {
  position: fixed;
  inset: 0 0 0 auto;
  width: min(560px, 94vw);
  max-width: none;
  height: 100dvh;
  max-height: none;
  margin: 0;
  padding: 0;
  border: 0;
  color: var(--co-text-primary);
  background: var(--co-bg-overlay);
  box-shadow: var(--co-shadow-overlay);
  overscroll-behavior: contain;
}

.evidence-drawer::backdrop { background: rgb(0 0 0 / 52%); }

.drawer-header {
  position: sticky;
  top: 0;
  z-index: 1;
  display: flex;
  min-height: 64px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-4);
  padding: var(--co-space-3) var(--co-space-5);
  border-bottom: 1px solid var(--co-border-default);
  background: var(--co-bg-overlay);
}

.drawer-header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.drawer-header h4 { margin: 1px 0 0; font-size: 18px; }
.drawer-close { display: grid; width: 44px; height: 44px; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: transparent; cursor: pointer; }
.drawer-close:hover { background: var(--co-bg-hover); }

.drawer-body {
  display: grid;
  gap: var(--co-space-5);
  height: calc(100dvh - 64px);
  padding: var(--co-space-5);
  overflow-y: auto;
  overscroll-behavior: contain;
}

.evidence-state-callout {
  display: flex;
  align-items: flex-start;
  gap: var(--co-space-3);
  padding: var(--co-space-4);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-subtle);
}

.evidence-state-callout p,
.drawer-body section p { margin: 0; color: var(--co-text-secondary); font-size: 13px; }
.drawer-body h5 { margin: 0 0 var(--co-space-2); font-size: 13px; }
.drawer-body section > p { overflow-wrap: anywhere; }

.drawer-facts,
.trust-boundary dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--co-space-3);
  margin: 0;
}

.drawer-facts div,
.trust-boundary dl div { min-width: 0; }
.drawer-facts dt,
.trust-boundary dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.drawer-facts dd,
.trust-boundary dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-secondary); font-size: 12px; overflow-wrap: anywhere; }

.trust-boundary {
  padding: var(--co-space-4);
  border-left: 3px solid var(--co-status-neutral-border);
  background: var(--co-bg-subtle);
}

.trust-boundary p { margin-top: var(--co-space-3) !important; }

@keyframes evidence-pulse {
  50% { opacity: 0.55; }
}

@media (prefers-reduced-motion: reduce) {
  .evidence-tail-skeleton span { animation: none; }
}

@media (max-width: 1100px) {
  .evidence-desktop { display: none; }
  .evidence-mobile { display: grid; gap: var(--co-space-3); }

  .evidence-mobile-row {
    display: grid;
    min-width: 0;
    gap: var(--co-space-3);
    padding: var(--co-space-4);
    border: 1px solid var(--co-border-default);
    border-radius: var(--co-radius-panel);
    background: var(--co-bg-surface);
    content-visibility: auto;
    contain-intrinsic-size: auto 240px;
  }

  .evidence-mobile-row header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
  .evidence-mobile-row header > div { display: grid; min-width: 0; gap: 3px; }
  .evidence-mobile-row header span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
  .evidence-mobile-row header strong { overflow-wrap: anywhere; font-size: 13px; }
  .evidence-mobile-row dl { display: grid; gap: var(--co-space-2); margin: 0; }
  .evidence-mobile-row dl div { min-width: 0; }
  .evidence-mobile-row dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
  .evidence-mobile-row dd { min-width: 0; margin: 2px 0 0; font-size: 11px; overflow-wrap: anywhere; }
  .evidence-mobile-row .inspect-button { width: 100%; min-height: 44px; }
  .evidence-actions { align-items: flex-start; flex-direction: column; }
  .evidence-actions :deep(.el-button) { width: 100%; }
  .drawer-facts,
  .trust-boundary dl { grid-template-columns: minmax(0, 1fr); }
}
</style>
