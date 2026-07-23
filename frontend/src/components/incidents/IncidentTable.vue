<script setup lang="ts">
import { computed, ref } from "vue";
import { ArrowDown, ArrowUp } from "@element-plus/icons-vue";

import { humanizeCode, incidentDetailPath } from "../../models/incidents";
import type { IncidentView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import AttentionFlag from "./AttentionFlag.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import SeverityBadge from "./SeverityBadge.vue";

type SortKey = "severity" | "status" | "updated";
type SortDirection = "ascending" | "descending";

const props = defineProps<{
  items: IncidentView[];
  pending?: boolean;
  nextCursor?: string;
  loadingMore?: boolean;
}>();

defineEmits<{
  loadMore: [];
}>();

const sortKey = ref<SortKey>("updated");
const sortDirection = ref<SortDirection>("descending");
const severityOrder: Record<string, number> = { critical: 4, warning: 3, info: 2, unknown: 1 };

const sortedItems = computed(() => [...props.items].sort((left, right) => {
  let comparison = 0;
  if (sortKey.value === "severity") {
    comparison = (severityOrder[left.severity] ?? 0) - (severityOrder[right.severity] ?? 0);
  } else if (sortKey.value === "status") {
    comparison = left.status.localeCompare(right.status);
  } else {
    comparison = dateValue(left.updated_at) - dateValue(right.updated_at);
  }
  if (comparison === 0) comparison = left.id.localeCompare(right.id);
  return sortDirection.value === "ascending" ? comparison : -comparison;
}));

function setSort(key: SortKey) {
  if (sortKey.value === key) {
    sortDirection.value = sortDirection.value === "ascending" ? "descending" : "ascending";
    return;
  }
  sortKey.value = key;
  sortDirection.value = key === "status" ? "ascending" : "descending";
}

function ariaSort(key: SortKey): SortDirection | "none" {
  return sortKey.value === key ? sortDirection.value : "none";
}

function dateValue(value?: string): number {
  if (!value) return 0;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? timestamp : 0;
}

function compactID(value: string): string {
  return value.length > 16 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value;
}
</script>

<template>
  <section
    class="incident-results"
    :aria-busy="pending"
    aria-labelledby="incident-results-title"
  >
    <div
      class="desktop-table-wrap"
      role="region"
      aria-label="Incident table. Scroll horizontally to view all columns."
      tabindex="0"
    >
      <table>
        <caption class="visually-hidden">
          Loaded V3 Incidents. Sort controls apply to the currently loaded cursor results.
        </caption>
        <thead>
          <tr>
            <th
              scope="col"
              :aria-sort="ariaSort('severity')"
            >
              <button
                type="button"
                class="sort-button"
                @click="setSort('severity')"
              >
                Severity
                <el-icon
                  :size="13"
                  aria-hidden="true"
                >
                  <ArrowUp v-if="sortKey === 'severity' && sortDirection === 'ascending'" />
                  <ArrowDown v-else />
                </el-icon>
              </button>
            </th>
            <th scope="col">
              Incident
            </th>
            <th
              scope="col"
              :aria-sort="ariaSort('status')"
            >
              <button
                type="button"
                class="sort-button"
                @click="setSort('status')"
              >
                Status / Stage
                <el-icon
                  :size="13"
                  aria-hidden="true"
                >
                  <ArrowUp v-if="sortKey === 'status' && sortDirection === 'ascending'" />
                  <ArrowDown v-else />
                </el-icon>
              </button>
            </th>
            <th scope="col">
              Attention
            </th>
            <th scope="col">
              Cycle / Version
            </th>
            <th
              scope="col"
              :aria-sort="ariaSort('updated')"
            >
              <button
                type="button"
                class="sort-button"
                @click="setSort('updated')"
              >
                Updated
                <el-icon
                  :size="13"
                  aria-hidden="true"
                >
                  <ArrowUp v-if="sortKey === 'updated' && sortDirection === 'ascending'" />
                  <ArrowDown v-else />
                </el-icon>
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="incident in sortedItems"
            :key="incident.id"
          >
            <td data-label="Severity">
              <SeverityBadge :severity="incident.severity" />
            </td>
            <td
              class="incident-identity"
              data-label="Incident"
            >
              <RouterLink :to="incidentDetailPath(incident.id)">
                <strong>{{ incident.summary || "Incident cycle in progress" }}</strong>
                <span>
                  <code translate="no">{{ compactID(incident.id) }}</code>
                  <span aria-hidden="true"> · </span>
                  Cycle {{ incident.cycle }}
                </span>
              </RouterLink>
              <span
                v-if="incident.migrated_legacy_context"
                class="legacy-context"
              >
                Legacy context
              </span>
            </td>
            <td data-label="Status / Stage">
              <IncidentStatusBadge :status="incident.status" />
            </td>
            <td
              class="attention-cell"
              data-label="Attention"
            >
              <AttentionFlag :active="incident.needs_attention" />
              <span v-if="incident.needs_attention">
                {{ humanizeCode(incident.blocking_reason_code) }}
              </span>
            </td>
            <td
              class="numeric-cell"
              data-label="Cycle / Version"
            >
              <span>{{ incident.cycle }}</span>
              <span aria-hidden="true">/</span>
              <span>{{ incident.version }}</span>
            </td>
            <td
              class="time-cell"
              data-label="Updated"
            >
              <time :datetime="incident.updated_at">
                {{ formatIncidentTime(incident.updated_at) }}
              </time>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <ul class="mobile-incident-list">
      <li
        v-for="incident in sortedItems"
        :key="incident.id"
      >
        <article>
          <div class="mobile-incident-badges">
            <SeverityBadge :severity="incident.severity" />
            <IncidentStatusBadge :status="incident.status" />
          </div>
          <RouterLink
            class="mobile-incident-link"
            :to="incidentDetailPath(incident.id)"
          >
            <strong>{{ incident.summary || "Incident cycle in progress" }}</strong>
            <span><code translate="no">{{ compactID(incident.id) }}</code> · Cycle {{ incident.cycle }}</span>
          </RouterLink>
          <dl>
            <div>
              <dt>Attention</dt>
              <dd>
                <AttentionFlag :active="incident.needs_attention" />
                <span v-if="incident.needs_attention">{{ humanizeCode(incident.blocking_reason_code) }}</span>
              </dd>
            </div>
            <div>
              <dt>Updated</dt>
              <dd><time :datetime="incident.updated_at">{{ formatIncidentTime(incident.updated_at) }}</time></dd>
            </div>
          </dl>
        </article>
      </li>
    </ul>

    <footer class="results-footer">
      <p>
        The List projection does not expose service or workload fields. Use the exact service filter, then open an Incident for persisted scope and Evidence.
      </p>
      <el-button
        v-if="nextCursor"
        :loading="loadingMore"
        @click="$emit('loadMore')"
      >
        {{ loadingMore ? "Loading More…" : "Load More Incidents" }}
      </el-button>
    </footer>
  </section>
</template>

<style scoped>
.incident-results {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.desktop-table-wrap {
  width: 100%;
  overflow-x: auto;
  overscroll-behavior: contain;
}

table {
  width: 100%;
  min-width: 960px;
  border-collapse: collapse;
  table-layout: fixed;
}

th,
td {
  padding: var(--co-space-3) var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
  text-align: left;
  vertical-align: middle;
}

th {
  color: var(--co-text-muted);
  background: var(--co-bg-subtle);
  font-size: 11px;
  font-weight: 750;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

th:nth-child(1) { width: 12%; }
th:nth-child(2) { width: 28%; }
th:nth-child(3) { width: 14%; }
th:nth-child(4) { width: 20%; }
th:nth-child(5) { width: 11%; }
th:nth-child(6) { width: 15%; }

tbody tr {
  content-visibility: auto;
  contain-intrinsic-size: auto 64px;
  transition: background-color var(--co-motion-fast) var(--co-ease-out);
}

tbody tr:hover {
  background: var(--co-bg-hover);
}

tbody tr:last-child td {
  border-bottom: 0;
}

.sort-button {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  gap: 5px;
  padding: 0;
  border: 0;
  color: inherit;
  background: transparent;
  cursor: pointer;
}

.sort-button:hover {
  color: var(--co-text-primary);
}

.incident-identity,
.incident-identity a {
  min-width: 0;
}

.incident-identity a {
  display: grid;
  gap: var(--co-space-1);
  color: var(--co-action-primary);
}

.incident-identity a:hover strong,
.incident-identity a:focus-visible strong {
  text-decoration: underline;
  text-underline-offset: 3px;
}

.incident-identity strong {
  display: -webkit-box;
  overflow: hidden;
  color: var(--co-text-primary);
  font-size: 14px;
  line-height: 1.4;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.incident-identity a > span,
.legacy-context,
.attention-cell > span,
.time-cell {
  color: var(--co-text-muted);
  font-size: 12px;
}

.legacy-context {
  display: inline-block;
  margin-top: var(--co-space-1);
}

.attention-cell > span {
  display: block;
  margin-top: 2px;
  overflow-wrap: anywhere;
}

.numeric-cell {
  font-family: var(--co-font-mono);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.time-cell time {
  font-variant-numeric: tabular-nums;
}

.mobile-incident-list {
  display: none;
  min-width: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}

.results-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-4);
  padding: var(--co-space-4);
  border-top: 1px solid var(--co-border-default);
  background: var(--co-bg-subtle);
}

.results-footer p {
  max-width: 78ch;
  margin: 0;
  color: var(--co-text-muted);
  font-size: 12px;
}

@media (max-width: 767px) {
  .desktop-table-wrap {
    display: none;
  }

  .mobile-incident-list {
    display: grid;
  }

  .mobile-incident-list li + li {
    border-top: 1px solid var(--co-border-default);
  }

  .mobile-incident-list article {
    display: grid;
    min-width: 0;
    gap: var(--co-space-3);
    padding: var(--co-space-4);
  }

  .mobile-incident-badges {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: var(--co-space-2);
  }

  .mobile-incident-link {
    display: grid;
    min-width: 0;
    min-height: 44px;
    gap: var(--co-space-1);
    align-content: center;
  }

  .mobile-incident-link strong {
    min-width: 0;
    color: var(--co-text-primary);
    font-size: 16px;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .mobile-incident-link > span {
    min-width: 0;
    color: var(--co-text-muted);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  .mobile-incident-list dl {
    display: grid;
    gap: var(--co-space-2);
    margin: 0;
  }

  .mobile-incident-list dl > div {
    display: grid;
    grid-template-columns: 72px minmax(0, 1fr);
    gap: var(--co-space-2);
  }

  .mobile-incident-list dt {
    color: var(--co-text-muted);
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
  }

  .mobile-incident-list dd {
    display: grid;
    min-width: 0;
    gap: 2px;
    margin: 0;
    color: var(--co-text-secondary);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  .results-footer {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
