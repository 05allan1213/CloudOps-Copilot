<script setup lang="ts">
import { useRouter } from "vue-router";

import type { Host } from "../../types";
import { formatTime } from "../../utils/format";

defineProps<{
  hosts: Host[];
}>();

const router = useRouter();

function cpuColor(value: number): string {
  if (value >= 80) return "var(--el-color-danger)";
  if (value > 60) return "var(--el-color-warning)";
  return "var(--el-color-success)";
}

function memoryColor(value: number): string {
  if (value >= 85) return "var(--el-color-danger)";
  if (value > 70) return "var(--el-color-warning)";
  return "var(--el-color-success)";
}

function handleRowClick(row: Host) {
  router.push(`/hosts/${encodeURIComponent(row.instance)}`);
}
</script>

<template>
  <el-table
    :data="hosts"
    stripe
    highlight-current-row
    style="width: 100%"
    @row-click="handleRowClick"
  >
    <el-table-column
      prop="instance"
      label="实例名"
      min-width="180"
      show-overflow-tooltip
    />
    <el-table-column
      label="状态"
      width="100"
      align="center"
    >
      <template #default="{ row }">
        <el-tag
          :type="row.status === 'up' ? 'success' : 'danger'"
          size="small"
        >
          {{ row.status === "up" ? "在线" : "离线" }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column
      label="CPU"
      width="200"
    >
      <template #default="{ row }">
        <el-progress
          :percentage="Number(row.cpu.toFixed(1))"
          :color="cpuColor(row.cpu)"
          :stroke-width="10"
          :format="(val: number) => val.toFixed(1) + '%'"
        />
      </template>
    </el-table-column>
    <el-table-column
      label="内存"
      width="200"
    >
      <template #default="{ row }">
        <el-progress
          :percentage="Number(row.memory.toFixed(1))"
          :color="memoryColor(row.memory)"
          :stroke-width="10"
          :format="(val: number) => val.toFixed(1) + '%'"
        />
      </template>
    </el-table-column>
    <el-table-column
      label="最后采集"
      width="180"
    >
      <template #default="{ row }">
        {{ formatTime(row.lastScrape) }}
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
          @click.stop="router.push(`/hosts/${encodeURIComponent(row.instance)}`)"
        >
          详情
        </el-button>
      </template>
    </el-table-column>
  </el-table>
</template>
