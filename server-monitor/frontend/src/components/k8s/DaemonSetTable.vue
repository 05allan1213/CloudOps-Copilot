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
    style="width: 100%"
  >
    <el-table-column
      prop="namespace"
      label="Namespace"
      width="160"
    />
    <el-table-column
      prop="name"
      label="Name"
      min-width="200"
    />
    <el-table-column
      label="Desired"
      width="90"
      align="center"
    >
      <template #default="{ row }">
        {{ row.desired }}
      </template>
    </el-table-column>
    <el-table-column
      label="Current"
      width="90"
      align="center"
    >
      <template #default="{ row }">
        {{ row.current }}
      </template>
    </el-table-column>
    <el-table-column
      label="Ready"
      width="90"
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
      label="Updated"
      width="90"
      align="center"
    >
      <template #default="{ row }">
        {{ row.updated }}
      </template>
    </el-table-column>
    <el-table-column
      prop="node_selector"
      label="Node Selector"
      min-width="160"
      show-overflow-tooltip
    >
      <template #default="{ row }">
        {{ row.node_selector || "-" }}
      </template>
    </el-table-column>
    <el-table-column
      label="Age"
      width="80"
      align="center"
    >
      <template #default="{ row }">
        {{ formatAge(row.age) }}
      </template>
    </el-table-column>
    <el-table-column
      label="Actions"
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
