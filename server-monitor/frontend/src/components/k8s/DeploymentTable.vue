<script setup lang="ts">
import { ref } from "vue";

import type { K8sDeploymentSummary } from "../../types";

import YamlViewer from "./YamlViewer.vue";

defineProps<{
  deployments: K8sDeploymentSummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("deployment");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "deployment";
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}
</script>

<template>
  <el-table :data="deployments" stripe highlight-current-row style="width: 100%">
    <el-table-column prop="namespace" label="命名空间" min-width="120" show-overflow-tooltip />
    <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
    <el-table-column label="副本" width="100" align="center">
      <template #default="{ row }">
        {{ row.ready_replicas }}/{{ row.replicas }}
      </template>
    </el-table-column>
    <el-table-column prop="updated_replicas" label="已更新" width="80" align="center" />
    <el-table-column prop="available_replicas" label="可用" width="80" align="center" />
    <el-table-column prop="strategy" label="策略" width="90" align="center" />
    <el-table-column label="状态" width="80" align="center">
      <template #default="{ row }">
        <el-tag :type="row.available_replicas >= row.replicas ? 'success' : 'warning'" size="small">
          {{ row.available_replicas >= row.replicas ? "正常" : "异常" }}
        </el-tag>
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
