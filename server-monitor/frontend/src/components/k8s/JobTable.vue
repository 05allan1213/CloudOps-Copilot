<script setup lang="ts">
import type { K8sJobSummary } from "../../types";

defineProps<{
  items: K8sJobSummary[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  (e: "view-yaml", item: K8sJobSummary): void;
}>();

function formatAge(ns: number): string {
  const s = Math.floor(ns / 1e9);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

const statusTagType: Record<string, string> = {
  Completed: "success",
  Failed: "danger",
  Running: "primary",
  Suspended: "warning",
};
</script>

<template>
  <el-table :data="items" v-loading="loading" stripe style="width: 100%">
    <el-table-column prop="namespace" label="Namespace" width="160" />
    <el-table-column prop="name" label="Name" min-width="200" />
    <el-table-column label="Completions" width="120" align="center">
      <template #default="{ row }">{{ row.completions }}</template>
    </el-table-column>
    <el-table-column label="Duration" width="120" align="center">
      <template #default="{ row }">{{ row.duration || "-" }}</template>
    </el-table-column>
    <el-table-column label="Status" width="110" align="center">
      <template #default="{ row }">
        <el-tag
          :type="(statusTagType[row.status] || 'info') as any"
          size="small"
        >
          {{ row.status }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column label="Age" width="80" align="center">
      <template #default="{ row }">{{ formatAge(row.age) }}</template>
    </el-table-column>
    <el-table-column label="Actions" width="80" fixed="right" align="center">
      <template #default="{ row }">
        <el-button link type="primary" size="small" @click="emit('view-yaml', row)">
          YAML
        </el-button>
      </template>
    </el-table-column>
  </el-table>
</template>
