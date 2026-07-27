<script setup lang="ts">
import { computed, ref } from "vue";
import { ArrowDown, ArrowUp, BellRing, ShieldCheck } from "lucide-vue-next";

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

defineEmits<{ loadMore: [] }>();

const sortKey = ref<SortKey>("updated");
const sortDirection = ref<SortDirection>("descending");
const severityOrder: Record<string, number> = { critical: 4, warning: 3, info: 2, unknown: 1 };

const sortedItems = computed(() => [...props.items].sort((left, right) => {
  let comparison = 0;
  if (sortKey.value === "severity") comparison = (severityOrder[left.severity] ?? 0) - (severityOrder[right.severity] ?? 0);
  else if (sortKey.value === "status") comparison = left.status.localeCompare(right.status);
  else comparison = dateValue(left.updated_at) - dateValue(right.updated_at);
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

function recoveryLabel(value: IncidentView["recovery"]["state"]): string {
  return ({
    not_started: "尚未验证",
    awaiting_verification: "等待验证",
    verifying: "验证中",
    investigate: "返回调查",
    recovered: "已证明恢复",
  })[value];
}

function stageLabel(value: IncidentView["attention"]["stage"]): string {
  return ({
    detect: "检测",
    investigate: "调查",
    decide: "决策",
    act: "执行",
    verify: "验证",
    recovered: "已恢复",
    closed: "已关闭",
    unknown: "未知",
  })[value];
}
</script>

<template>
  <section class="incident-results" :aria-busy="pending" aria-labelledby="incident-results-title">
    <div class="desktop-table-wrap" role="region" aria-label="Incident 列表，可横向滚动查看全部列。" tabindex="0">
      <table>
        <caption class="visually-hidden">当前已加载的 Incident；表头排序仅作用于当前 cursor 结果。</caption>
        <thead>
          <tr>
            <th scope="col" :aria-sort="ariaSort('severity')"><button type="button" class="sort-button" @click="setSort('severity')">级别<ArrowUp v-if="sortKey === 'severity' && sortDirection === 'ascending'" :size="13" aria-hidden="true" /><ArrowDown v-else :size="13" aria-hidden="true" /></button></th>
            <th scope="col">Incident</th>
            <th scope="col">Scope / Alert</th>
            <th scope="col" :aria-sort="ariaSort('status')"><button type="button" class="sort-button" @click="setSort('status')">状态 / Attention<ArrowUp v-if="sortKey === 'status' && sortDirection === 'ascending'" :size="13" aria-hidden="true" /><ArrowDown v-else :size="13" aria-hidden="true" /></button></th>
            <th scope="col">恢复</th>
            <th scope="col" :aria-sort="ariaSort('updated')"><button type="button" class="sort-button" @click="setSort('updated')">更新<ArrowUp v-if="sortKey === 'updated' && sortDirection === 'ascending'" :size="13" aria-hidden="true" /><ArrowDown v-else :size="13" aria-hidden="true" /></button></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="incident in sortedItems" :key="incident.id">
            <td data-label="级别"><SeverityBadge :severity="incident.severity" /></td>
            <td class="incident-identity" data-label="Incident">
              <RouterLink :to="incidentDetailPath(incident.id)"><strong>{{ incident.summary || "Incident Cycle 进行中" }}</strong><span><code translate="no">{{ compactID(incident.id) }}</code> · Cycle {{ incident.cycle }} · v{{ incident.version }}</span></RouterLink>
              <span v-if="incident.migrated_legacy_context" class="legacy-context">迁移的 legacy context</span>
            </td>
            <td class="scope-cell" data-label="Scope / Alerts">
              <strong>{{ incident.operational_context.namespace }}/{{ incident.operational_context.resource.name }}</strong>
              <span>{{ incident.operational_context.cluster }} · {{ incident.operational_context.service }}</span>
              <span class="alert-count"><BellRing :size="13" aria-hidden="true" />{{ incident.related_alert_count }} Alert</span>
            </td>
            <td class="lifecycle-cell" data-label="状态 / Attention">
              <div><IncidentStatusBadge :status="incident.status" /><span>{{ stageLabel(incident.attention.stage) }}</span></div>
              <div><AttentionFlag :active="incident.attention.required" /><span v-if="incident.attention.required">{{ humanizeCode(incident.attention.reason_code) }}</span></div>
            </td>
            <td class="recovery-cell" data-label="恢复">
              <strong><ShieldCheck :size="15" aria-hidden="true" />{{ recoveryLabel(incident.recovery.state) }}</strong>
              <span>{{ incident.recovery.verification_attempts }} 次尝试 · {{ incident.recovery.failed_verification_count }} 次失败</span>
              <span>{{ incident.recovery.resolution_report_id ? "ResolutionReport 已生成" : "尚无 ResolutionReport" }}</span>
            </td>
            <td class="time-cell" data-label="更新"><time :datetime="incident.updated_at">{{ formatIncidentTime(incident.updated_at) }}</time></td>
          </tr>
        </tbody>
      </table>
    </div>

    <ul class="mobile-incident-list">
      <li v-for="incident in sortedItems" :key="incident.id">
        <article>
          <div class="mobile-incident-badges"><SeverityBadge :severity="incident.severity" /><IncidentStatusBadge :status="incident.status" /></div>
          <RouterLink class="mobile-incident-link" :to="incidentDetailPath(incident.id)"><strong>{{ incident.summary || "Incident Cycle 进行中" }}</strong><span><code translate="no">{{ compactID(incident.id) }}</code> · Cycle {{ incident.cycle }}</span></RouterLink>
          <dl>
            <div><dt>Scope</dt><dd>{{ incident.operational_context.namespace }}/{{ incident.operational_context.resource.name }}</dd></div>
            <div><dt>Alert</dt><dd>{{ incident.related_alert_count }}</dd></div>
            <div><dt>Attention</dt><dd><AttentionFlag :active="incident.attention.required" /><span v-if="incident.attention.required">{{ humanizeCode(incident.attention.reason_code) }}</span></dd></div>
            <div><dt>恢复</dt><dd>{{ recoveryLabel(incident.recovery.state) }}</dd></div>
            <div><dt>更新</dt><dd><time :datetime="incident.updated_at">{{ formatIncidentTime(incident.updated_at) }}</time></dd></div>
          </dl>
        </article>
      </li>
    </ul>

    <footer class="results-footer">
      <p>Alert、Attention、Decision 与恢复字段均来自每条 Incident 的当前 Cycle 投影。</p>
      <button v-if="nextCursor" type="button" class="load-more" :disabled="loadingMore" @click="$emit('loadMore')">{{ loadingMore ? "正在加载…" : "加载更多 Incident" }}</button>
    </footer>
  </section>
</template>

<style scoped>
.incident-results { min-width: 0; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.desktop-table-wrap { width: 100%; overflow-x: auto; overscroll-behavior: contain; }
table { width: 100%; min-width: 1160px; border-collapse: collapse; table-layout: fixed; }
th, td { padding: var(--co-space-3) var(--co-space-4); border-bottom: 1px solid var(--co-border-default); text-align: left; vertical-align: middle; }
th { color: var(--co-text-muted); background: var(--co-bg-subtle); font-size: 10px; font-weight: 750; text-transform: uppercase; }
th:nth-child(1) { width: 110px; } th:nth-child(2) { width: 25%; } th:nth-child(3) { width: 20%; } th:nth-child(4) { width: 22%; } th:nth-child(5) { width: 20%; } th:nth-child(6) { width: 150px; }
tbody tr { content-visibility: auto; contain-intrinsic-size: auto 92px; }
tbody tr:hover { background: var(--co-bg-hover); }
.sort-button { display: inline-flex; min-height: 36px; align-items: center; gap: 5px; margin: -8px; padding: 8px; border: 0; border-radius: var(--co-radius-control); color: inherit; background: transparent; cursor: pointer; font: inherit; font-weight: inherit; text-transform: inherit; }
.sort-button:hover { color: var(--co-text-primary); background: var(--co-bg-active); }
.sort-button:focus-visible, .load-more:focus-visible, a:focus-visible { outline: 2px solid var(--co-action-primary); outline-offset: 2px; }
.incident-identity, .scope-cell, .recovery-cell, .lifecycle-cell { min-width: 0; }
.incident-identity > a, .scope-cell, .recovery-cell { display: grid; gap: 4px; }
.incident-identity strong, .scope-cell strong { color: var(--co-text-primary); font-size: 13px; overflow-wrap: anywhere; }
.incident-identity > a > span, .scope-cell > span, .recovery-cell > span { color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
.incident-identity a:hover strong { color: var(--co-action-primary); }
.legacy-context { display: inline-flex; width: fit-content; margin-top: 5px; padding: 2px 6px; border: 1px solid var(--co-status-warning-border); border-radius: var(--co-radius-pill); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); font-size: 9px; }
.alert-count, .recovery-cell strong { display: inline-flex; align-items: center; gap: 5px; }
.alert-count { color: var(--co-text-secondary) !important; }
.lifecycle-cell { display: grid; gap: var(--co-space-2); }
.lifecycle-cell > div { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.lifecycle-cell span { color: var(--co-text-muted); font-size: 10px; overflow-wrap: anywhere; }
.recovery-cell strong { color: var(--co-text-primary); font-size: 12px; }
.time-cell time { color: var(--co-text-secondary); font-size: 11px; font-variant-numeric: tabular-nums; }
.mobile-incident-list { display: none; margin: 0; padding: 0; list-style: none; }
.results-footer { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-3) var(--co-space-4); }
.results-footer p { margin: 0; color: var(--co-text-muted); font-size: 11px; }
.load-more { display: inline-flex; min-height: 40px; flex: 0 0 auto; align-items: center; padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); cursor: pointer; font-weight: 700; }
.load-more:hover { border-color: var(--co-border-strong); background: var(--co-bg-hover); }
.load-more:disabled { cursor: wait; opacity: .55; }
@media (max-width: 767px) {
  .desktop-table-wrap { display: none; }
  .mobile-incident-list { display: grid; }
  .mobile-incident-list li + li { border-top: 1px solid var(--co-border-default); }
  .mobile-incident-list article { display: grid; min-width: 0; gap: var(--co-space-3); padding: var(--co-space-4); }
  .mobile-incident-badges { display: flex; flex-wrap: wrap; gap: var(--co-space-2); }
  .mobile-incident-link { display: grid; min-width: 0; gap: 4px; }
  .mobile-incident-link strong { color: var(--co-text-primary); font-size: 14px; overflow-wrap: anywhere; }
  .mobile-incident-link span { color: var(--co-text-muted); font-size: 11px; }
  .mobile-incident-list dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-3); margin: 0; }
  .mobile-incident-list dt { color: var(--co-text-muted); font-size: 10px; text-transform: uppercase; }
  .mobile-incident-list dd { display: flex; min-width: 0; align-items: center; gap: 5px; margin: 3px 0 0; color: var(--co-text-secondary); font-size: 11px; overflow-wrap: anywhere; }
  .results-footer { align-items: stretch; flex-direction: column; }
  .load-more { width: 100%; justify-content: center; }
}
@media (max-width: 420px) { .mobile-incident-list dl { grid-template-columns: minmax(0, 1fr); } }
</style>
