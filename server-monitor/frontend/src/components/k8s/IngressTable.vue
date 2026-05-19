<script setup lang="ts">
import { ref } from "vue";

import type { K8sIngressSummary } from "../../types";

import YamlViewer from "./YamlViewer.vue";

defineProps<{
  ingresses: K8sIngressSummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("ingress");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "ingress";
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

function formatHosts(hosts: string[]): string {
  if (!hosts || hosts.length === 0) return "*";
  return hosts.join(", ");
}
</script>

<template>
  <el-table :data="ingresses" stripe highlight-current-row style="width: 100%">
    <el-table-column prop="namespace" label="命名空间" min-width="120" show-overflow-tooltip />
    <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
    <el-table-column label="Hosts" min-width="160" show-overflow-tooltip>
      <template #default="{ row }">
        {{ formatHosts(row.hosts) }}
      </template>
    </el-table-column>
    <el-table-column label="Paths" width="100" align="center">
      <template #default="{ row }">
        <el-tag size="small" type="info">{{ row.paths?.length ?? 0 }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column label="TLS" width="100" align="center">
      <template #default="{ row }">
        <el-tag v-if="row.tls?.length" size="small" type="success">Yes</el-tag>
        <el-tag v-else size="small" type="info">No</el-tag>
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
