<script setup lang="ts">
import { ref } from "vue";

import type { K8sPVCSummary } from "../../types";

import K8sStatusBadge from "./K8sStatusBadge.vue";
import YamlViewer from "./YamlViewer.vue";

defineProps<{
  pvcs: K8sPVCSummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("persistentvolumeclaim");
const yamlNamespace = ref("");
const yamlName = ref("");

function viewYaml(namespace: string, name: string) {
  yamlKind.value = "persistentvolumeclaim";
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}
</script>

<template>
  <el-table
    :data="pvcs"
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
      prop="storage_class"
      label="存储类"
      min-width="130"
      show-overflow-tooltip
    />
    <el-table-column
      prop="volume_name"
      label="绑定 PV"
      min-width="180"
      show-overflow-tooltip
    />
    <el-table-column
      label="访问模式"
      min-width="140"
      show-overflow-tooltip
    >
      <template #default="{ row }">
        {{ row.access_modes?.join(", ") || "-" }}
      </template>
    </el-table-column>
    <el-table-column
      label="状态"
      width="110"
      align="center"
    >
      <template #default="{ row }">
        <K8sStatusBadge
          :status="row.status"
          type="pvc"
        />
      </template>
    </el-table-column>
    <el-table-column
      label="存活"
      width="100"
      align="center"
    >
      <template #default="{ row }">
        {{ row.age || "-" }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作"
      width="80"
      align="center"
    >
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
