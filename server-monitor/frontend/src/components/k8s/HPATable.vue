<script setup lang="ts">
import { ref, computed } from "vue";

import type { K8sHPASummary } from "../../types";

import YamlViewer from "./YamlViewer.vue";

const props = defineProps<{
  hpas: K8sHPASummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("horizontalpodautoscaler");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "horizontalpodautoscaler";
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}

function formatAge(age: number): string {
  if (age < 60) return `${Math.round(age)}s`;
  if (age < 3600) return `${Math.round(age / 60)}m`;
  if (age < 86400) return `${Math.round(age / 3600)}h`;
  return `${Math.round(age / 86400)}d`;
}

function replicasTagType(hpa: K8sHPASummary): "" | "warning" | "danger" {
  if (hpa.max_replicas <= 0) return "";
  const ratio = hpa.current_replicas / hpa.max_replicas;
  if (ratio >= 1) return "danger";
  if (ratio >= 0.8) return "warning";
  return "";
}

const sortedHpas = computed(() => {
  const items = [...props.hpas];
  items.sort((a, b) => {
    const aRatio = a.max_replicas > 0 ? a.current_replicas / a.max_replicas : 0;
    const bRatio = b.max_replicas > 0 ? b.current_replicas / b.max_replicas : 0;
    return bRatio - aRatio;
  });
  return items;
});
</script>

<template>
  <el-table :data="sortedHpas" stripe highlight-current-row style="width: 100%">
    <el-table-column prop="namespace" label="命名空间" min-width="120" show-overflow-tooltip />
    <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
    <el-table-column prop="reference" label="Reference" min-width="180" show-overflow-tooltip />
    <el-table-column label="Min" width="80" align="center">
      <template #default="{ row }">
        {{ row.min_replicas }}
      </template>
    </el-table-column>
    <el-table-column label="Max" width="80" align="center">
      <template #default="{ row }">
        {{ row.max_replicas }}
      </template>
    </el-table-column>
    <el-table-column label="Current" width="100" align="center">
      <template #default="{ row }">
        <el-tag
          v-if="replicasTagType(row)"
          size="small"
          :type="replicasTagType(row)"
        >
          {{ row.current_replicas }}
        </el-tag>
        <span v-else>{{ row.current_replicas }}</span>
      </template>
    </el-table-column>
    <el-table-column prop="target_utilization" label="Target" min-width="140" show-overflow-tooltip>
      <template #default="{ row }">
        {{ row.target_utilization || "-" }}
      </template>
    </el-table-column>
    <el-table-column label="Age" width="100" align="center">
      <template #default="{ row }">
        {{ formatAge(row.age) }}
      </template>
    </el-table-column>
    <el-table-column label="操作" width="80" align="center">
      <template #default="{ row }">
        <el-button
          type="primary"
          link
          size="small"
          @click="viewYaml(row.namespace, row.name)"
        >
          YAML
        </el-button>
      </template>
    </el-table-column>
  </el-table>
  <YamlViewer
    v-model:visible="yamlVisible"
    :kind="yamlKind"
    :namespace="yamlNamespace"
    :name="yamlName"
  />
</template>
