<script setup lang="ts">
import type { K8sDaemonSetSummary } from "../../types";

defineProps<{
  items: K8sDaemonSetSummary[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  (e: "view-yaml", item: K8sDaemonSetSummary): void;
}>();

function formatAge(ns: number): string {
  const s = Math.floor(ns / 1e9);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}
</script>

<template>
  <el-table
    v-loading="loading"
    :data="items"
    stripe
    highlight-current-row
    style="width: 100%"
  >
    <el-table-column
      prop="namespace"
      label="命名空间"
      min-width="120"
      show-overflow-tooltip
    />
    <el-table-column
      prop="name"
      label="名称"
      min-width="180"
      show-overflow-tooltip
    />
    <el-table-column
      label="期望"
      width="80"
      align="center"
    >
      <template #default="{ row }">
        {{ row.desired }}
      </template>
    </el-table-column>
    <el-table-column
      label="当前"
      width="80"
      align="center"
    >
      <template #default="{ row }">
        {{ row.current }}
      </template>
    </el-table-column>
    <el-table-column
      label="就绪"
      width="100"
      align="center"
    >
      <template #default="{ row }">
        <el-tag
          :type="row.ready === row.desired ? 'success' : 'warning'"
          size="small"
        >
          {{ row.ready }}/{{ row.desired }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column
      label="已更新"
      width="80"
      align="center"
    >
      <template #default="{ row }">
        {{ row.updated }}
      </template>
    </el-table-column>
    <el-table-column
      prop="node_selector"
      label="节点选择器"
      min-width="160"
      show-overflow-tooltip
    >
      <template #default="{ row }">
        {{ row.node_selector || "-" }}
      </template>
    </el-table-column>
    <el-table-column
      label="存活"
      width="80"
      align="center"
    >
      <template #default="{ row }">
        {{ formatAge(row.age) }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作"
      width="80"
      fixed="right"
      align="center"
    >
      <template #default="{ row }">
        <el-button
          link
          type="primary"
          size="small"
          @click="emit('view-yaml', row)"
        >
          YAML
        </el-button>
      </template>
    </el-table-column>
  </el-table>
</template>
