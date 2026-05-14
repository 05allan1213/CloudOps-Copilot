<script setup lang="ts">
import { Search } from "@element-plus/icons-vue";

import HostCard from "./HostCard.vue";
import HostTable from "./host/HostTable.vue";
import type { Host } from "../types";

type HostStatus = "all" | "up" | "down";
type HostSort = "instance" | "cpu_desc" | "memory_desc";
type HostRisk = "all" | "high_cpu" | "high_memory";
type ViewMode = "card" | "table";

const props = defineProps<{
  hosts: Host[];
  loading: boolean;
  hostSearchInput: string;
  appliedHostQuery: string;
  selectedHostStatus: HostStatus;
  selectedHostSort: HostSort;
  selectedHostRisk: HostRisk;
  hostViewSummary: string;
  hostFilterSummary: string[];
  hasActiveHostFilters: boolean;
}>();

const emit = defineEmits<{
  "update:hostSearchInput": [value: string];
  applySearch: [];
  statusChange: [value: HostStatus];
  sortChange: [value: HostSort];
  riskChange: [value: HostRisk];
  resetFilters: [];
}>();

const viewMode = defineModel<ViewMode>("viewMode", { default: "card" });
</script>

<template>
  <el-card shadow="never" class="hosts-panel">
    <template #header>
      <div class="panel-header">
        <div class="panel-title">
          <el-icon :size="18" color="var(--el-color-primary)"><Search /></el-icon>
          <span class="panel-title-text">主机指标</span>
        </div>
        <div class="panel-actions">
          <el-input
            :model-value="props.hostSearchInput"
            placeholder="搜索主机名"
            :prefix-icon="Search"
            clearable
            style="width: 200px"
            @update:model-value="emit('update:hostSearchInput', $event)"
            @keyup.enter="emit('applySearch')"
          />

          <el-radio-group
            :model-value="selectedHostStatus"
            size="small"
            @change="emit('statusChange', $event as HostStatus)"
          >
            <el-radio-button value="all">全部</el-radio-button>
            <el-radio-button value="up">在线</el-radio-button>
            <el-radio-button value="down">离线</el-radio-button>
          </el-radio-group>

          <el-radio-group
            :model-value="selectedHostSort"
            size="small"
            @change="emit('sortChange', $event as HostSort)"
          >
            <el-radio-button value="instance">名称</el-radio-button>
            <el-radio-button value="cpu_desc">CPU</el-radio-button>
            <el-radio-button value="memory_desc">内存</el-radio-button>
          </el-radio-group>

          <el-radio-group
            :model-value="selectedHostRisk"
            size="small"
            @change="emit('riskChange', $event as HostRisk)"
          >
            <el-radio-button value="all">全风险</el-radio-button>
            <el-radio-button value="high_cpu">高 CPU</el-radio-button>
            <el-radio-button value="high_memory">高内存</el-radio-button>
          </el-radio-group>

          <el-radio-group
            v-model="viewMode"
            size="small"
          >
            <el-radio-button value="card">卡片</el-radio-button>
            <el-radio-button value="table">表格</el-radio-button>
          </el-radio-group>

          <el-button
            v-if="hasActiveHostFilters"
            size="small"
            @click="emit('resetFilters')"
          >
            重置
          </el-button>

          <el-tag size="small" type="info">WebSocket 实时推送</el-tag>
        </div>
      </div>
    </template>

    <div class="host-summary">
      <span class="host-summary-label">当前条件</span>
      <el-tag size="small" type="primary">{{ hostViewSummary }}</el-tag>
      <el-tag v-if="!hasActiveHostFilters" size="small" type="info">默认视图</el-tag>
      <el-tag
        v-for="item in hostFilterSummary"
        :key="item"
        size="small"
      >
        {{ item }}
      </el-tag>
    </div>

    <el-skeleton v-if="loading" :rows="5" animated />

    <el-empty
      v-else-if="hosts.length === 0"
      :description="
        appliedHostQuery
          ? '没有匹配的主机'
          : selectedHostStatus === 'all'
            ? '暂无主机数据'
            : '当前筛选条件下没有主机'
      "
    />

    <template v-else>
      <div v-if="viewMode === 'card'" class="hosts-grid">
        <HostCard
          v-for="host in hosts"
          :key="host.instance"
          :host="host"
        />
      </div>
      <HostTable v-else :hosts="hosts" />
    </template>
  </el-card>
</template>

<style scoped>
.hosts-panel :deep(.el-card__body) {
  padding: 16px 20px;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.panel-title-text {
  font-size: 15px;
  font-weight: 600;
}

.panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.host-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}

.host-summary-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-weight: 600;
}

.hosts-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}

@media (max-width: 768px) {
  .panel-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .panel-actions {
    width: 100%;
  }

  .hosts-grid {
    grid-template-columns: 1fr;
  }
}
</style>
