<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import { fetchDiagnosis } from "../api/diagnosis";
import type { DiagnosisReport } from "../types";

const route = useRoute();
const report = ref<DiagnosisReport | null>(null);
const loading = ref(false);
const error = ref("");

const metrics = computed(() => report.value?.evidence?.metrics ?? []);
const ruleResults = computed(() => report.value?.rule_analysis?.results ?? []);
const actions = computed(() => report.value?.recommended_actions ?? []);
const collectionErrors = computed(() => report.value?.evidence?.collection_errors ?? []);

function formatPercent(value?: number) {
  return `${Math.round((value ?? 0) * 100)}%`;
}

function formatTime(value?: string) {
  if (!value) return "-";
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

function formatJSON(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

async function loadReport() {
  const id = Number(route.params.id);
  if (!Number.isFinite(id) || id <= 0) {
    error.value = "无效诊断报告 ID";
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    report.value = await fetchDiagnosis(id);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载诊断报告失败";
  } finally {
    loading.value = false;
  }
}

onMounted(loadReport);
</script>

<template>
  <section class="detail-page">
    <div v-if="loading" class="empty-line">加载中</div>
    <div v-else-if="error" class="message error">{{ error }}</div>
    <template v-else-if="report">
      <header class="detail-header">
        <div>
          <h2>#{{ report.id }} {{ report.alert_name || "诊断报告" }}</h2>
          <p>{{ report.target_name || "-" }} · {{ report.status }} · {{ formatTime(report.created_at) }}</p>
        </div>
        <div class="confidence">
          <strong>{{ formatPercent(report.confidence) }}</strong>
          <span>{{ report.confidence_level }}</span>
        </div>
      </header>

      <section class="summary-panel">
        <h3>摘要</h3>
        <p>{{ report.summary || "-" }}</p>
        <h3>根因假设</h3>
        <p>{{ report.root_cause || "-" }}</p>
      </section>

      <section class="grid-panels">
        <div class="panel">
          <h3>建议动作</h3>
          <div v-if="actions.length === 0" class="empty-line">暂无建议</div>
          <ul v-else class="action-list">
            <li v-for="action in actions" :key="`${action.type}-${action.description}`">
              <strong>{{ action.description }}</strong>
              <span>{{ action.type }} · {{ action.risk }} · {{ action.requires_approval ? "需审批" : "只读建议" }}</span>
            </li>
          </ul>
        </div>

        <div class="panel">
          <h3>指标证据</h3>
          <div v-if="metrics.length === 0" class="empty-line">暂无指标证据</div>
          <table v-else>
            <thead>
              <tr>
                <th>指标</th>
                <th>avg</th>
                <th>max</th>
                <th>last</th>
                <th>趋势</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="metric in metrics" :key="metric.name">
                <td>{{ metric.name }}</td>
                <td>{{ metric.avg.toFixed(2) }}</td>
                <td>{{ metric.max.toFixed(2) }}</td>
                <td>{{ metric.last.toFixed(2) }}</td>
                <td>{{ metric.trend }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="panel">
        <h3>规则分析</h3>
        <div v-if="ruleResults.length === 0" class="empty-line">暂无规则分析</div>
        <div v-else class="rule-list">
          <div v-for="rule in ruleResults" :key="rule.rule" class="rule-item" :class="{ passed: rule.passed }">
            <strong>{{ rule.rule }}</strong>
            <span>{{ rule.passed ? "命中" : "未命中" }}</span>
            <p>{{ rule.detail }}</p>
          </div>
        </div>
      </section>

      <section v-if="collectionErrors.length" class="panel warning-panel">
        <h3>采集降级</h3>
        <ul class="error-list">
          <li v-for="item in collectionErrors" :key="`${item.source}-${item.error}`">
            {{ item.source }}：{{ item.error }}
          </li>
        </ul>
      </section>

      <details class="panel raw-panel">
        <summary>证据快照 JSON</summary>
        <pre>{{ formatJSON(report.evidence) }}</pre>
      </details>
    </template>
  </section>
</template>

<style scoped>
.detail-page {
  display: grid;
  gap: 1rem;
}

.detail-header,
.summary-panel,
.panel,
.message {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
}

h2,
h3,
p {
  margin: 0;
}

h2 {
  font-size: 1.2rem;
}

h3 {
  color: var(--text-secondary);
  font-size: 0.86rem;
  margin-bottom: 0.65rem;
}

.detail-header p,
.summary-panel p,
.action-list span,
.rule-item p {
  color: var(--text-muted);
  font-size: 0.82rem;
  margin-top: 0.35rem;
}

.confidence {
  display: grid;
  justify-items: end;
  gap: 0.2rem;
}

.confidence strong {
  color: var(--accent);
  font-size: 1.2rem;
}

.confidence span {
  color: var(--text-muted);
  font-size: 0.76rem;
}

.grid-panels {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
  gap: 1rem;
}

.action-list,
.error-list {
  display: grid;
  gap: 0.6rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.action-list li {
  display: grid;
  gap: 0.25rem;
  border-top: 1px solid var(--border-color);
  padding-top: 0.6rem;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  border-top: 1px solid var(--border-color);
  padding: 0.58rem 0.45rem;
  text-align: left;
  font-size: 0.8rem;
}

th {
  color: var(--text-muted);
  font-size: 0.72rem;
}

.rule-list {
  display: grid;
  gap: 0.55rem;
}

.rule-item {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.65rem;
}

.rule-item.passed {
  border-color: rgba(34, 197, 94, 0.35);
}

.rule-item span {
  float: right;
  color: var(--text-muted);
  font-size: 0.75rem;
}

.warning-panel {
  border-color: rgba(245, 158, 11, 0.35);
}

.raw-panel summary {
  cursor: pointer;
  color: var(--text-secondary);
  font-weight: 700;
}

pre {
  max-height: 360px;
  overflow: auto;
  color: var(--text-secondary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.empty-line {
  color: var(--text-muted);
  padding: 0.8rem 0;
  text-align: center;
}

.message.error {
  color: var(--danger);
  background: var(--danger-soft);
}

@media (max-width: 820px) {
  .detail-header,
  .grid-panels {
    grid-template-columns: 1fr;
  }

  .detail-header {
    display: grid;
  }

  .confidence {
    justify-items: start;
  }
}
</style>
