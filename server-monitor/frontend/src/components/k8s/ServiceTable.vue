<script setup lang="ts">
import { ref } from "vue";

import type { K8sServiceSummary } from "../../types";

import YamlViewer from "./YamlViewer.vue";

defineProps<{
  services: K8sServiceSummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("service");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "service";
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}

function formatPorts(ports?: Array<{ name?: string; protocol: string; port: number; target_port?: string }>): string {
  if (!ports || ports.length === 0) return "-";
  return ports.map(p => `${p.port}${p.target_port ? ":" + p.target_port : ""}/${p.protocol}`).join(", ");
}
</script>

<template>
  <el-table :data="services" stripe highlight-current-row style="width: 100%">
    <el-table-column prop="namespace" label="命名空间" min-width="120" show-overflow-tooltip />
    <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
    <el-table-column label="类型" width="120" align="center">
      <template #default="{ row }">
        <el-tag size="small">{{ row.type }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="cluster_ip" label="ClusterIP" min-width="130" show-overflow-tooltip />
    <el-table-column label="端口" min-width="160" show-overflow-tooltip>
      <template #default="{ row }">
        {{ formatPorts(row.ports) }}
      </template>
    </el-table-column>
    <el-table-column label="Selector" min-width="160" show-overflow-tooltip>
      <template #default="{ row }">
        {{ row.selector ? Object.entries(row.selector).map(([k, v]) => `${k}=${v}`).join(", ") : "-" }}
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
