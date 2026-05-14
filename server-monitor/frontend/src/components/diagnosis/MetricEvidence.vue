<script setup lang="ts">
import type { DiagnosisMetricEvidence } from "../../types";

defineProps<{
  metrics: DiagnosisMetricEvidence[];
}>();
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <span class="card-title">指标证据</span>
    </template>
    <el-empty v-if="metrics.length === 0" description="暂无指标证据" :image-size="32" />
    <el-table v-else :data="metrics" stripe size="small" style="width: 100%">
      <el-table-column prop="name" label="指标" min-width="120" show-overflow-tooltip />
      <el-table-column label="avg" width="90" align="right">
        <template #default="{ row }">{{ row.avg.toFixed(2) }}</template>
      </el-table-column>
      <el-table-column label="max" width="90" align="right">
        <template #default="{ row }">{{ row.max.toFixed(2) }}</template>
      </el-table-column>
      <el-table-column label="last" width="90" align="right">
        <template #default="{ row }">{{ row.last.toFixed(2) }}</template>
      </el-table-column>
      <el-table-column prop="trend" label="趋势" width="80" />
    </el-table>
  </el-card>
</template>

<style scoped>
.card-title {
  font-size: 14px;
  font-weight: 600;
}
</style>
