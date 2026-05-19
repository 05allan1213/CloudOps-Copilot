<script setup lang="ts">
import { ref } from "vue";

import type { K8sEventSummary } from "../../types";

import K8sStatusBadge from "./K8sStatusBadge.vue";
import YamlViewer from "./YamlViewer.vue";

defineProps<{
  events: K8sEventSummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("event");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "event";
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}
</script>

<template>
  <el-table :data="events" stripe highlight-current-row style="width: 100%">
    <el-table-column label="类型" width="90" align="center">
      <template #default="{ row }">
        <K8sStatusBadge :status="row.type || 'Normal'" type="event" />
      </template>
    </el-table-column>
    <el-table-column prop="reason" label="原因" min-width="140" show-overflow-tooltip />
    <el-table-column prop="involved_kind" label="Kind" width="100" show-overflow-tooltip />
    <el-table-column prop="involved_name" label="对象" min-width="160" show-overflow-tooltip />
    <el-table-column prop="namespace" label="命名空间" min-width="120" show-overflow-tooltip />
    <el-table-column prop="message" label="消息" min-width="200" show-overflow-tooltip />
    <el-table-column prop="count" label="次数" width="70" align="center" />
    <el-table-column label="最近发生" width="160">
      <template #default="{ row }">
        {{ row.last_seen ? new Date(row.last_seen).toLocaleString() : "-" }}
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
