<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { RouterLink } from "vue-router";

import { fetchDiagnosisList, type DiagnosisQuery } from "../api/diagnosis";
import type { DiagnosisListResponse } from "../types";

const reports = ref<DiagnosisListResponse>({
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
});
const loading = ref(false);
const error = ref("");
const filters = reactive<DiagnosisQuery>({
  status: "",
  trigger_type: "",
  page: 1,
  page_size: 20,
});

const pageCount = computed(() =>
  Math.max(1, Math.ceil(reports.value.total / reports.value.page_size)),
);

function statusLabel(value: string) {
  switch (value) {
    case "pending":
      return "等待中";
    case "running":
      return "诊断中";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    default:
      return value || "-";
  }
}

function triggerLabel(value: string) {
  switch (value) {
    case "manual":
      return "手动";
    case "chat":
      return "对话";
    case "auto":
      return "自动";
    default:
      return value || "-";
  }
}

function formatPercent(value: number) {
  return `${Math.round(value * 100)}%`;
}

function formatTime(value?: string) {
  if (!value) return "-";
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

async function loadReports() {
  loading.value = true;
  error.value = "";
  try {
    reports.value = await fetchDiagnosisList(filters);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载诊断报告失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  filters.page = 1;
  loadReports();
}

function changePage(nextPage: number) {
  filters.page = Math.min(Math.max(nextPage, 1), pageCount.value);
  loadReports();
}

onMounted(loadReports);
</script>

<template>
  <section class="diagnosis-page">
    <header class="page-header">
      <div>
        <h2>诊断报告</h2>
        <p>查看手动或 Copilot 触发的告警诊断结果。</p>
      </div>
    </header>

    <form class="filter-panel" @submit.prevent="applyFilters">
      <label>
        <span>状态</span>
        <select v-model="filters.status">
          <option value="">全部</option>
          <option value="pending">pending</option>
          <option value="running">running</option>
          <option value="completed">completed</option>
          <option value="failed">failed</option>
        </select>
      </label>
      <label>
        <span>来源</span>
        <select v-model="filters.trigger_type">
          <option value="">全部</option>
          <option value="manual">手动</option>
          <option value="chat">对话</option>
          <option value="auto">自动</option>
        </select>
      </label>
      <div class="filter-actions">
        <button class="primary-btn" type="submit">查询</button>
      </div>
    </form>

    <div v-if="error" class="message error">{{ error }}</div>

    <div class="table-panel">
      <div class="table-head">
        <span>共 {{ reports.total }} 条</span>
        <span>第 {{ reports.page }} / {{ pageCount }} 页</span>
      </div>
      <div v-if="loading" class="empty-line">加载中</div>
      <div v-else-if="reports.items.length === 0" class="empty-line">暂无诊断报告</div>
      <table v-else>
        <thead>
          <tr>
            <th>ID</th>
            <th>告警</th>
            <th>目标</th>
            <th>状态</th>
            <th>来源</th>
            <th>置信度</th>
            <th>摘要</th>
            <th>创建时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in reports.items" :key="item.id">
            <td>
              <RouterLink class="detail-link" :to="`/diagnosis/${item.id}`">#{{ item.id }}</RouterLink>
            </td>
            <td>{{ item.alert_name || "-" }}</td>
            <td class="mono-cell">{{ item.target_name || "-" }}</td>
            <td>{{ statusLabel(item.status) }}</td>
            <td>{{ triggerLabel(item.trigger_type) }}</td>
            <td>{{ formatPercent(item.confidence) }}</td>
            <td class="summary-cell">{{ item.summary || "-" }}</td>
            <td>{{ formatTime(item.created_at) }}</td>
          </tr>
        </tbody>
      </table>
      <div class="pager">
        <button type="button" :disabled="reports.page <= 1" @click="changePage(reports.page - 1)">
          上一页
        </button>
        <button type="button" :disabled="reports.page >= pageCount" @click="changePage(reports.page + 1)">
          下一页
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.diagnosis-page {
  display: grid;
  gap: 1rem;
}

.page-header h2 {
  font-size: 1.25rem;
  margin: 0;
}

.page-header p {
  color: var(--text-muted);
  font-size: 0.82rem;
  margin-top: 0.3rem;
}

.filter-panel,
.table-panel,
.message {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.filter-panel {
  display: flex;
  gap: 0.85rem;
  align-items: end;
}

label {
  display: grid;
  gap: 0.4rem;
  color: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 700;
}

select {
  color: var(--text-primary);
  background: rgba(11, 15, 23, 0.72);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.62rem 0.7rem;
}

.table-head,
.pager {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  color: var(--text-muted);
  font-size: 0.78rem;
  margin-bottom: 0.75rem;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  border-top: 1px solid var(--border-color);
  padding: 0.68rem 0.5rem;
  text-align: left;
  font-size: 0.82rem;
}

th {
  color: var(--text-muted);
  font-size: 0.72rem;
}

.summary-cell {
  max-width: 360px;
  overflow-wrap: anywhere;
}

.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.detail-link {
  color: var(--accent);
  font-weight: 700;
}

.empty-line {
  color: var(--text-muted);
  padding: 1rem 0;
  text-align: center;
}

.message.error {
  color: var(--danger);
  background: var(--danger-soft);
}
</style>
