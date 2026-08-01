<script setup lang="ts">
import { computed } from "vue";

import type { QueryExecution, QuerySeries } from "../../api/monitoring";
import MonitoringChart from "./MonitoringChart.vue";
import {
  monitoringSeriesLabel,
  monitoringValueAt,
} from "./monitoringChart";
import WorkspaceTechnicalDetails, { type TechnicalDetailField } from "../workspace/WorkspaceTechnicalDetails.vue";

const props = defineProps<{
  execution: QueryExecution | null;
  cursorTimestamp: number | null;
}>();

const emit = defineEmits<{
  cursor: [timestampSeconds: number | null];
}>();

const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });
const numberFormatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 5 });
const chartSeries = computed(() => props.execution?.result?.series.filter((series) => series.points.length > 0) ?? []);
const tableRows = computed(() => chartSeries.value.map((series, index) => {
  const point = monitoringValueAt(series, props.cursorTimestamp);
  return {
    key: `${index}-${JSON.stringify(series.labels)}`,
    label: monitoringSeriesLabel(series, index),
    point,
    sampleCount: series.points.length,
    logsLocation: point ? logsLocationAt(point.timestamp) : { path: "/logs" },
  };
}));
const tableTimestampLabel = computed(() => props.cursorTimestamp === null ? "最新值" : "游标值");
const allValues = computed(() => chartSeries.value.flatMap((series) => series.points.map((point) => point.value)).filter(Number.isFinite));
const primaryPoints = computed(() => chartSeries.value[0]?.points ?? []);
const latestValue = computed(() => primaryPoints.value.at(-1)?.value);
const peakValue = computed(() => allValues.value.length ? Math.max(...allValues.value) : undefined);
const changeRate = computed(() => {
  const first = primaryPoints.value[0]?.value;
  const latest = latestValue.value;
  if (first === undefined || latest === undefined || first === 0) return undefined;
  return ((latest - first) / Math.abs(first)) * 100;
});
const technicalFields = computed<TechnicalDetailField[]>(() => {
  const execution = props.execution;
  if (!execution) return [];
  return [
    { label: "Execution", value: execution.id, code: true, copyValue: execution.id },
    { label: "Query hash", value: execution.query_hash, code: true, copyValue: execution.query_hash },
    { label: "Configuration Revision", value: execution.configuration_revision_id, code: true, copyValue: execution.configuration_revision_id },
    { label: "Provider identity", value: execution.source.identity, code: true, copyValue: execution.source.identity },
    { label: "Scope", value: `${execution.scope.cluster_id} / ${execution.resource.namespace}`, code: true },
    { label: "Resource", value: `${execution.resource.kind} / ${execution.resource.name}`, code: true, copyValue: execution.resource.id },
    { label: "UTC range", value: `${execution.time_range.from} -> ${execution.time_range.to}`, code: true },
  ];
});

function statusLabel(status: QueryExecution["status"]): string {
  return ({ pending: "等待执行", running: "查询中", succeeded: "已完成", failed: "失败", cancelled: "已取消" })[status];
}

function statusColor(status: QueryExecution["status"]): "neutral" | "info" | "success" | "error" {
  if (status === "succeeded") return "success";
  if (status === "failed" || status === "cancelled") return "error";
  if (status === "running") return "info";
  return "neutral";
}

function formatTime(value?: string): string {
  if (!value) return "无";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB"];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(amount)} ${units[unit]}`;
}

function shortHash(value: string): string {
  return value.length > 18 ? `${value.slice(0, 14)}…` : value;
}

function formatMetric(value?: number): string {
  return value === undefined ? "--" : numberFormatter.format(value);
}

function formatChange(value?: number): string {
  if (value === undefined) return "无法计算";
  const prefix = value > 0 ? "+" : "";
  return `${prefix}${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(value)}%`;
}

function logsLocationAt(value: string) {
  const execution = props.execution;
  const timestamp = new Date(value).getTime();
  if (!execution || !Number.isFinite(timestamp)) return { path: "/logs" };
  return {
    path: "/logs",
    query: {
      cluster: execution.scope.cluster_id,
      namespace: execution.resource.namespace,
      resource: execution.resource.id,
      from: new Date(timestamp - 5 * 60_000).toISOString(),
      to: new Date(timestamp + 5 * 60_000).toISOString(),
    },
  };
}

function seriesIdentity(series: QuerySeries): string {
  return Object.entries(series.labels).map(([key, value]) => `${key}=${value}`).join(", ");
}
</script>

<template>
  <section
    class="monitoring-result"
    aria-labelledby="monitoring-result-title"
  >
    <header class="monitoring-result__header">
      <div>
        <span>Query Execution</span>
        <h2 id="monitoring-result-title">
          查询结果
        </h2>
      </div>
      <div
        v-if="execution"
        class="monitoring-result__actions"
      >
        <UBadge
          :color="statusColor(execution.status)"
          variant="soft"
          :icon="execution.status === 'running' ? 'i-lucide-loader-circle' : undefined"
          :label="statusLabel(execution.status)"
        />
        <slot name="actions" />
      </div>
    </header>

    <WorkspaceState
      v-if="!execution"
      kind="empty"
      title="尚无查询结果"
      description="选择真实 Workload 与时间范围后执行查询。"
    />

    <template v-else>
      <div class="monitoring-result__meta">
        <span><b>{{ execution.series_count }}</b> series</span>
        <span><b>{{ execution.sample_count }}</b> samples</span>
        <span><b>{{ formatBytes(execution.response_bytes) }}</b></span>
        <span>Revision <b>{{ shortHash(execution.configuration_revision_id) }}</b></span>
        <span>采集 {{ formatTime(execution.source.collected_at) }}</span>
      </div>

      <dl
        class="monitoring-result__summary"
        aria-label="指标摘要"
      >
        <div>
          <dt>当前值</dt>
          <dd>{{ formatMetric(latestValue) }}</dd>
          <small>{{ chartSeries[0] ? monitoringSeriesLabel(chartSeries[0], 0) : "暂无主序列" }}</small>
        </div>
        <div>
          <dt>区间峰值</dt>
          <dd>{{ formatMetric(peakValue) }}</dd>
          <small>全部返回序列</small>
        </div>
        <div>
          <dt>区间变化</dt>
          <dd :class="{ 'is-rising': (changeRate ?? 0) > 0 }">
            {{ formatChange(changeRate) }}
          </dd>
          <small>主序列首尾对比</small>
        </div>
        <div>
          <dt>覆盖</dt>
          <dd>{{ execution.series_count }} / {{ execution.sample_count }}</dd>
          <small>序列 / 采样点</small>
        </div>
      </dl>

      <WorkspaceState
        v-if="execution.status === 'failed'"
        kind="error"
        :title="execution.error_code || 'QUERY_FAILED'"
        :description="execution.error_detail || 'Prometheus 查询失败。'"
      />
      <WorkspaceState
        v-else-if="execution.result_expired"
        kind="expired"
        title="遥测结果已过期"
        description="完整结果未长期保留，查询身份与执行审计仍可用。"
      />
      <UAlert
        v-else-if="execution.partial || execution.truncated"
        color="warning"
        variant="soft"
        icon="i-lucide-triangle-alert"
        title="结果不完整"
        :description="[execution.partial ? 'Provider 返回部分结果' : '', execution.truncated ? '结果触及服务端边界并被截断' : ''].filter(Boolean).join('；')"
      />

      <MonitoringChart
        v-if="chartSeries.length && !execution.result_expired"
        :series="chartSeries"
        @cursor="emit('cursor', $event)"
      />

      <WorkspaceState
        v-else-if="execution.status === 'succeeded' && !execution.result_expired"
        kind="empty"
        title="查询没有返回时序"
        description="查询已完成，当前范围内没有可显示的数据点。"
      />

      <section
        v-if="tableRows.length && !execution.result_expired"
        class="monitoring-result__table"
        aria-labelledby="monitoring-series-table-title"
      >
        <header>
          <div>
            <h3 id="monitoring-series-table-title">
              同步序列表
            </h3>
            <span>{{ tableTimestampLabel }} · {{ tableRows.length }} rows</span>
          </div>
        </header>
        <div class="monitoring-result__table-scroll">
          <table>
            <thead>
              <tr><th>Labels</th><th>{{ tableTimestampLabel }}</th><th>时间</th><th>Samples</th><th>相关</th></tr>
            </thead>
            <tbody>
              <tr
                v-for="(row, index) in tableRows"
                :key="row.key"
              >
                <td
                  class="monitoring-result__series"
                  :title="seriesIdentity(chartSeries[index]!)"
                >
                  {{ row.label }}
                </td>
                <td class="monitoring-result__number">
                  {{ row.point ? numberFormatter.format(row.point.value) : "无" }}
                </td>
                <td>{{ formatTime(row.point?.timestamp) }}</td>
                <td class="monitoring-result__number">
                  {{ row.sampleCount }}
                </td>
                <td>
                  <UButton
                    :to="row.logsLocation"
                    color="neutral"
                    variant="ghost"
                    size="xs"
                    icon="i-lucide-logs"
                    label="日志"
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section
        class="monitoring-result__audit"
        aria-labelledby="monitoring-audit-title"
      >
        <header>
          <h3 id="monitoring-audit-title">
            执行审计
          </h3>
          <span>{{ execution.events.length }} events</span>
        </header>
        <ol>
          <li
            v-for="event in execution.events"
            :key="event.id"
          >
            <strong>{{ event.type }}</strong>
            <span>{{ event.actor }} · {{ formatTime(event.created_at) }}</span>
            <p v-if="event.detail">
              {{ event.detail }}
            </p>
          </li>
        </ol>
      </section>

      <WorkspaceTechnicalDetails
        :fields="technicalFields"
        title="查询技术详情"
        description="Execution、精确 Hash、Revision、Provider 与 UTC 范围"
      />
    </template>
  </section>
</template>

<style scoped>
.monitoring-result { min-width: 0; }
.monitoring-result__header,
.monitoring-result__actions,
.monitoring-result__meta,
.monitoring-result__table header,
.monitoring-result__audit header {
  display: flex;
  min-width: 0;
  align-items: center;
}
.monitoring-result__header { min-height: 52px; justify-content: space-between; gap: var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.monitoring-result__header > div:first-child { display: grid; gap: 1px; }
.monitoring-result__header h2 { margin: 0; font-size: 16px; }
.monitoring-result__header > div:first-child > span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.monitoring-result__actions { flex-wrap: wrap; justify-content: flex-end; gap: var(--co-space-1); }
.monitoring-result__meta { flex-wrap: wrap; gap: var(--co-space-2) var(--co-space-5); padding: var(--co-space-2) 0; color: var(--co-text-secondary); font-size: 11px; font-variant-numeric: tabular-nums; }
.monitoring-result__summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; border-block: 1px solid var(--co-border-default); background: var(--co-bg-subtle); }
.monitoring-result__summary > div { display: grid; min-width: 0; gap: 2px; padding: var(--co-space-3); border-right: 1px solid var(--co-border-subtle); }
.monitoring-result__summary > div:last-child { border-right: 0; }
.monitoring-result__summary dt { color: var(--co-text-muted); font-size: 10px; }
.monitoring-result__summary dd { min-width: 0; margin: 0; color: var(--co-text-primary); font-size: 20px; font-weight: 650; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.monitoring-result__summary dd.is-rising { color: var(--co-status-warning-fg); }
.monitoring-result__summary small { min-width: 0; overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-result__table { min-width: 0; min-height: 230px; border-bottom: 1px solid var(--co-border-default); }
.monitoring-result__table header,
.monitoring-result__audit header { min-height: 44px; justify-content: space-between; }
.monitoring-result__table h3,
.monitoring-result__audit h3 { margin: 0; font-size: 14px; }
.monitoring-result__table header span,
.monitoring-result__audit header span { color: var(--co-text-muted); font-size: 11px; }
.monitoring-result__table-scroll { max-height: 280px; overflow: auto; }
.monitoring-result__table table { width: 100%; border-collapse: collapse; font-size: 11px; }
.monitoring-result__table th,
.monitoring-result__table td { height: 40px; padding: var(--co-space-2); border-bottom: 1px solid var(--co-border-subtle); text-align: left; }
.monitoring-result__table th { position: sticky; top: 0; z-index: 1; background: var(--co-bg-surface); color: var(--co-text-muted); font-weight: 700; }
.monitoring-result__series { max-width: var(--co-table-cell-max-width); overflow: hidden; font-family: var(--co-font-mono); text-overflow: ellipsis; white-space: nowrap; }
.monitoring-result__table td.monitoring-result__number { text-align: right; font-variant-numeric: tabular-nums; }
.monitoring-result__audit { padding-bottom: var(--co-space-4); }
.monitoring-result__audit ol { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.monitoring-result__audit li { min-width: 0; padding: var(--co-space-2) var(--co-space-3); border-left: 2px solid var(--co-border-strong); background: var(--co-bg-subtle); }
.monitoring-result__audit li strong,
.monitoring-result__audit li span { display: block; }
.monitoring-result__audit li strong { font-size: 11px; }
.monitoring-result__audit li span,
.monitoring-result__audit li p { color: var(--co-text-muted); font-size: 10px; }
.monitoring-result__audit li p { margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; }

@media (max-width: 1024px) {
  .monitoring-result__summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .monitoring-result__summary > div:nth-child(2) { border-right: 0; }
  .monitoring-result__summary > div:nth-child(-n + 2) { border-bottom: 1px solid var(--co-border-subtle); }
}
</style>
