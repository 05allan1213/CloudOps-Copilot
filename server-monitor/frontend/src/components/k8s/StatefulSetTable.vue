<script setup lang="ts">
import type { K8sStatefulSetSummary } from "../../types";

defineProps<{
  items: K8sStatefulSetSummary[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  (e: "view-yaml", item: K8sStatefulSetSummary): void;
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
  <el-table :data="items" v-loading="loading" stripe style="width: 100%">
    <el-table-column prop="namespace" label="Namespace" width="160" />
    <el-table-column prop="name" label="Name" min-width="200" />
    <el-table-column label="Ready" width="100" align="center">
      <template #default="{ row }">
        <el-tag
          :type="row.replicas_ready === row.replicas_desired ? 'success' : 'warning'"
          size="small"
        >
          {{ row.replicas_ready }}/{{ row.replicas_desired }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="service_name" label="Service Name" min-width="180" show-overflow-tooltip>
      <template #default="{ row }">
        {{ row.service_name || "-" }}
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
