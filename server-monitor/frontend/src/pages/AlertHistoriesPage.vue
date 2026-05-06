<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";

import { fetchAlertHistories, type AlertHistoryQuery } from "../api/alertHistories";
import { fetchHostGroups } from "../api/hostGroups";
import type { AlertHistory, AlertHistoryListResponse, HostGroup } from "../types";

const histories = ref<AlertHistoryListResponse>({
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
});
const groups = ref<HostGroup[]>([]);
const loading = ref(false);
const error = ref("");
const filters = reactive<AlertHistoryQuery>({
  status: "",
  severity: "",
  alert_name: "",
  instance: "",
  group: 0,
  page: 1,
  page_size: 20,
});

const pageCount = computed(() =>
  Math.max(1, Math.ceil(histories.value.total / histories.value.page_size)),
);

function severityLabel(value: string) {
  switch (value) {
    case "critical":
      return "严重";
    case "warning":
      return "警告";
    case "info":
      return "提示";
    default:
      return value || "-";
  }
}

function formatTime(value?: string) {
  if (!value) return "-";
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

async function loadGroups() {
  try {
    groups.value = await fetchHostGroups();
  } catch {
    groups.value = [];
  }
}

async function loadHistories() {
  loading.value = true;
  error.value = "";
  try {
    histories.value = await fetchAlertHistories(filters);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载告警历史失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  filters.page = 1;
  loadHistories();
}

function resetFilters() {
  Object.assign(filters, {
    status: "",
    severity: "",
    alert_name: "",
    instance: "",
    group: 0,
    page: 1,
    page_size: 20,
  });
  loadHistories();
}

function changePage(nextPage: number) {
  filters.page = Math.min(Math.max(nextPage, 1), pageCount.value);
  loadHistories();
}

onMounted(() => {
  loadGroups();
  loadHistories();
});
</script>

<template>
  <section class="history-page">
    <header class="page-header">
      <div>
        <h2>告警历史</h2>
        <p>查询 MySQL 中归档的告警记录。</p>
      </div>
    </header>

    <form class="filter-panel" @submit.prevent="applyFilters">
      <label>
        <span>状态</span>
        <select v-model="filters.status">
          <option value="">全部</option>
          <option value="firing">firing</option>
          <option value="resolved">resolved</option>
        </select>
      </label>
      <label>
        <span>级别</span>
        <select v-model="filters.severity">
          <option value="">全部</option>
          <option value="critical">critical</option>
          <option value="warning">warning</option>
          <option value="info">info</option>
        </select>
      </label>
      <label>
        <span>分组</span>
        <select v-model.number="filters.group">
          <option :value="0">全部分组</option>
          <option v-for="group in groups" :key="group.id" :value="group.id">
            {{ group.name }}
          </option>
        </select>
      </label>
      <label>
        <span>告警名</span>
        <input v-model.trim="filters.alert_name" />
      </label>
      <label>
        <span>实例</span>
        <input v-model.trim="filters.instance" />
      </label>
      <div class="filter-actions">
        <button class="primary-btn" type="submit">查询</button>
        <button class="ghost-btn" type="button" @click="resetFilters">重置</button>
      </div>
    </form>

    <div v-if="error" class="message error">{{ error }}</div>

    <div class="table-panel">
      <div class="table-head">
        <span>共 {{ histories.total }} 条</span>
        <span>第 {{ histories.page }} / {{ pageCount }} 页</span>
      </div>
      <div v-if="loading" class="empty-line">加载中</div>
      <div v-else-if="histories.items.length === 0" class="empty-line">暂无告警历史</div>
      <table v-else>
        <thead>
          <tr>
            <th>告警</th>
            <th>实例</th>
            <th>级别</th>
            <th>状态</th>
            <th>触发时间</th>
            <th>摘要</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in histories.items" :key="item.id">
            <td>{{ item.alert_name || "-" }}</td>
            <td class="mono-cell">{{ item.instance || "-" }}</td>
            <td>{{ severityLabel(item.severity) }}</td>
            <td>{{ item.status }}</td>
            <td>{{ formatTime(item.fired_at) }}</td>
            <td class="summary-cell">{{ item.summary || "-" }}</td>
          </tr>
        </tbody>
      </table>
      <div class="pager">
        <button type="button" :disabled="histories.page <= 1" @click="changePage(histories.page - 1)">
          上一页
        </button>
        <button type="button" :disabled="histories.page >= pageCount" @click="changePage(histories.page + 1)">
          下一页
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.history-page {
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
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
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

input,
select {
  width: 100%;
  color: var(--text-primary);
  background: rgba(11, 15, 23, 0.72);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.62rem 0.7rem;
}

input {
  cursor: text;
}

select {
  cursor: pointer;
}

.filter-actions,
.pager {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.primary-btn,
.ghost-btn,
.pager button {
  border-radius: var(--radius-sm);
  padding: 0.55rem 0.8rem;
  font-weight: 800;
}

.primary-btn {
  color: #fff;
  background: var(--accent);
}

.ghost-btn,
.pager button {
  color: var(--text-secondary);
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
}

.pager button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.message.error {
  color: var(--danger);
  border-color: rgba(239, 68, 68, 0.3);
}

.table-head {
  display: flex;
  justify-content: space-between;
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
  border-bottom: 1px solid var(--border-color);
  padding: 0.75rem 0.5rem;
  text-align: left;
  vertical-align: top;
  font-size: 0.82rem;
}

th {
  color: var(--text-muted);
  font-size: 0.72rem;
}

.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.summary-cell {
  max-width: 360px;
  color: var(--text-secondary);
}

.empty-line {
  color: var(--text-muted);
  font-size: 0.86rem;
}

.pager {
  justify-content: flex-end;
  margin-top: 0.9rem;
}

@media (max-width: 720px) {
  table {
    display: block;
    overflow-x: auto;
  }
}
</style>
