<script setup lang="ts">
import { ref } from "vue";

import type { K8sConfigMapSummary } from "../../types";

import YamlViewer from "./YamlViewer.vue";

defineProps<{
  configmaps: K8sConfigMapSummary[];
}>();

defineEmits<{
  view: [configmap: K8sConfigMapSummary];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("configmap");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "configmap";
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
</script>

<template>
  <el-table
    :data="configmaps"
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
      min-width="200"
      show-overflow-tooltip
    />
    <el-table-column
      label="Data Keys"
      width="120"
      align="center"
    >
      <template #default="{ row }">
        <el-tag
          size="small"
          type="info"
        >
          {{ row.data_keys?.length ?? 0 }}
        </el-tag>
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
    <el-table-column
      label="操作"
      width="120"
      align="center"
    >
      <template #default="{ row }">
        <el-button
          size="small"
          type="primary"
          link
          @click="$emit('view', row)"
        >
          详情
        </el-button>
        <el-button
          size="small"
          type="primary"
          link
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
