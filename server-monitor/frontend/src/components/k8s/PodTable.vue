<script setup lang="ts">
import { ref } from "vue";

import type { K8sPodSummary } from "../../types";

import K8sStatusBadge from "./K8sStatusBadge.vue";
import YamlViewer from "./YamlViewer.vue";

defineProps<{
  pods: K8sPodSummary[];
}>();

const emit = defineEmits<{
  viewLogs: [namespace: string, name: string];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("pod");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "pod";
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}
</script>

<template>
  <el-table
    :data="pods"
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
      label="阶段"
      width="110"
      align="center"
    >
      <template #default="{ row }">
        <K8sStatusBadge
          :status="row.phase"
          type="pod"
        />
      </template>
    </el-table-column>
    <el-table-column
      label="就绪"
      width="80"
      align="center"
    >
      <template #default="{ row }">
        {{ row.ready_containers }}/{{ row.total_containers }}
      </template>
    </el-table-column>
    <el-table-column
      prop="restart_count"
      label="重启"
      width="70"
      align="center"
    />
    <el-table-column
      prop="node_name"
      label="节点"
      min-width="140"
      show-overflow-tooltip
    />
    <el-table-column
      prop="pod_ip"
      label="Pod IP"
      min-width="120"
      show-overflow-tooltip
    />
    <el-table-column
      label="归属"
      min-width="140"
      show-overflow-tooltip
    >
      <template #default="{ row }">
        {{ row.owner_kind ? `${row.owner_kind}/${row.owner_name}` : "-" }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作"
      width="120"
      align="center"
    >
      <template #default="{ row }">
        <el-button
          type="primary"
          link
          size="small"
          @click="emit('viewLogs', row.namespace, row.name)"
        >
          日志
        </el-button>
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
