<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";

import { fetchAlertHistories, type AlertHistoryQuery } from "../api/alertHistories";
import { createDiagnosis } from "../api/diagnosis";
import { fetchHostGroups } from "../api/hostGroups";
import { formatTime } from "../utils/format";
import { usePagination } from "../composables/usePagination";
import { severityTagType, statusTagType } from "../composables/useTagTypes";
import FilterPanel from "../components/common/FilterPanel.vue";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import type { AlertHistory, AlertHistoryListResponse, HostGroup } from "../types";

const router = useRouter();
const histories = ref<AlertHistoryListResponse>({
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
});
const groups = ref<HostGroup[]>([]);
const loading = ref(false);
const error = ref("");
const diagnosisLoading = reactive<Record<number, boolean>>({});
const filters = reactive<AlertHistoryQuery>({
  status: "",
  severity: "",
  alert_name: "",
  instance: "",
  group: 0,
  page: 1,
  page_size: 20,
});

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (histories.value.items.length === 0) return "empty" as const;
  return "default" as const;
});

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
    filters.page = page.value;
    filters.page_size = pageSize.value;
    histories.value = await fetchAlertHistories(filters);
    total.value = histories.value.total;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载告警历史失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadHistories();
}

function resetFilters() {
  Object.assign(filters, {
    status: "",
    severity: "",
    alert_name: "",
    instance: "",
    group: 0,
  });
  resetPage();
  loadHistories();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadHistories();
}

async function diagnose(item: AlertHistory) {
  diagnosisLoading[item.id] = true;
  error.value = "";
  try {
    const report = await createDiagnosis({
      alert_history_id: item.id,
      trigger_type: "manual",
    });
    await router.push(`/diagnosis/${report.id}`);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "生成诊断失败";
  } finally {
    diagnosisLoading[item.id] = false;
  }
}

onMounted(() => {
  loadGroups();
  loadHistories();
});
</script>

<template>
  <section class="history-page">
    <PageHeader
      title="告警历史"
      subtitle="查询 MySQL 中归档的告警记录。"
    />

    <FilterPanel
      @search="applyFilters"
      @reset="resetFilters"
    >
      <el-form-item label="状态">
        <el-select
          v-model="filters.status"
          placeholder="全部"
          clearable
          style="width: 140px"
        >
          <el-option
            label="firing"
            value="firing"
          />
          <el-option
            label="resolved"
            value="resolved"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="级别">
        <el-select
          v-model="filters.severity"
          placeholder="全部"
          clearable
          style="width: 140px"
        >
          <el-option
            label="critical"
            value="critical"
          />
          <el-option
            label="warning"
            value="warning"
          />
          <el-option
            label="info"
            value="info"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="分组">
        <el-select
          v-model.number="filters.group"
          placeholder="全部分组"
          clearable
          style="width: 160px"
        >
          <el-option
            v-for="group in groups"
            :key="group.id"
            :label="group.name"
            :value="group.id"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="告警名">
        <el-input
          v-model.trim="filters.alert_name"
          placeholder="告警名"
          clearable
          style="width: 160px"
        />
      </el-form-item>
      <el-form-item label="实例">
        <el-input
          v-model.trim="filters.instance"
          placeholder="实例"
          clearable
          style="width: 160px"
        />
      </el-form-item>
    </FilterPanel>

    <StateWrapper
      :state="stateKey"
      :error-text="error"
      empty-text="暂无告警历史"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadHistories"
        >
          重试
        </el-button>
      </template>

      <el-table
        :data="histories.items"
        stripe
        style="width: 100%"
      >
        <el-table-column
          label="告警名"
          min-width="160"
          prop="alert_name"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.alert_name || "-" }}
          </template>
        </el-table-column>
        <el-table-column
          label="实例"
          min-width="140"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span class="mono-text">{{ row.instance || "-" }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="级别"
          width="90"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="severityTagType(row.severity)"
              size="small"
              effect="dark"
            >
              {{ severityLabel(row.severity) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="statusTagType(row.status)"
              size="small"
            >
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="触发时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.fired_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="摘要"
          min-width="200"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.summary || "-" }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-button
              type="primary"
              link
              size="small"
              :loading="diagnosisLoading[row.id]"
              @click="diagnose(row)"
            >
              诊断
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-wrap">
        <el-pagination
          v-model:current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="total, prev, pager, next"
          background
          @current-change="handlePageChange"
        />
      </div>
    </StateWrapper>
  </section>
</template>

<style scoped>
.history-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mono-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
