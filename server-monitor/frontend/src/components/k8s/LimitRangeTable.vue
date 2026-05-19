<script setup lang="ts">
import type { K8sLimitRangeSummary } from "../../types";

defineProps<{
  limitranges: K8sLimitRangeSummary[];
}>();

function formatAge(age: number): string {
  if (age < 60) return `${Math.round(age)}s`;
  if (age < 3600) return `${Math.round(age / 60)}m`;
  if (age < 86400) return `${Math.round(age / 3600)}h`;
  return `${Math.round(age / 86400)}d`;
}
</script>

<template>
  <el-table
    :data="limitranges"
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
      label="限制"
      min-width="300"
    >
      <template #default="{ row }">
        <div
          v-for="(item, idx) in row.limits"
          :key="idx"
          class="limit-row"
        >
          <el-tag
            size="small"
            type="info"
          >
            {{ item.type }}
          </el-tag>
          <span
            v-if="item.min"
            class="limit-field"
          >Min: {{ item.min }}</span>
          <span
            v-if="item.max"
            class="limit-field"
          >Max: {{ item.max }}</span>
          <span
            v-if="item.default"
            class="limit-field"
          >Default: {{ item.default }}</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column
      label="Age"
      width="100"
      align="center"
    >
      <template #default="{ row }">
        {{ formatAge(row.age) }}
      </template>
    </el-table-column>
  </el-table>
</template>

<style scoped>
.limit-row {
  display: flex;
  align-items: center;
  margin-bottom: 4px;
  gap: 8px;
}

.limit-field {
  font-size: 12px;
  color: var(--el-text-color-regular);
}
</style>
