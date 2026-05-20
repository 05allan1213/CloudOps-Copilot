<script setup lang="ts">
import type { K8sResourceQuotaSummary } from "../../types";

defineProps<{
  quotas: K8sResourceQuotaSummary[];
}>();

function formatAge(age: number): string {
  if (age < 60) return `${Math.round(age)}s`;
  if (age < 3600) return `${Math.round(age / 60)}m`;
  if (age < 86400) return `${Math.round(age / 3600)}h`;
  return `${Math.round(age / 86400)}d`;
}

function parseQuantity(val: string): number {
  if (!val) return 0;
  if (val.endsWith("m")) return parseFloat(val) / 1000;
  if (val.endsWith("Ki")) return parseFloat(val) * 1024;
  if (val.endsWith("Mi")) return parseFloat(val) * 1024 * 1024;
  if (val.endsWith("Gi")) return parseFloat(val) * 1024 * 1024 * 1024;
  return parseFloat(val) || 0;
}

function getPercentage(used: string, hard: string): number {
  const u = parseQuantity(used);
  const h = parseQuantity(hard);
  if (h === 0) return 0;
  return Math.min(Math.round((u / h) * 100), 100);
}

function progressStatus(pct: number): "" | "success" | "warning" | "exception" {
  if (pct >= 90) return "exception";
  if (pct >= 70) return "warning";
  return "";
}
</script>

<template>
  <el-table
    :data="quotas"
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
      label="资源使用"
      min-width="300"
    >
      <template #default="{ row }">
        <div
          v-for="key in Object.keys(row.hard)"
          :key="key"
          class="quota-row"
        >
          <span class="quota-key">{{ key }}</span>
          <el-progress
            :percentage="getPercentage(row.used?.[key]?.value ?? '0', row.hard[key]?.value ?? '0')"
            :status="progressStatus(getPercentage(row.used?.[key]?.value ?? '0', row.hard[key]?.value ?? '0'))"
            :stroke-width="14"
            style="flex: 1; margin: 0 8px"
          />
          <span class="quota-value">{{ row.used?.[key]?.value ?? "0" }} / {{ row.hard[key]?.value }}</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column
      label="存活"
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
.quota-row {
  display: flex;
  align-items: center;
  margin-bottom: 4px;
  gap: 4px;
}

.quota-key {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  min-width: 80px;
  white-space: nowrap;
}

.quota-value {
  font-size: 12px;
  color: var(--el-text-color-regular);
  white-space: nowrap;
}
</style>
