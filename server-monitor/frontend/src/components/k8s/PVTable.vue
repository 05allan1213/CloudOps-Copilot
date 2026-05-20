<script setup lang="ts">
import { ref } from "vue";

import type { K8sPVSummary } from "../../types";

import K8sStatusBadge from "./K8sStatusBadge.vue";
import YamlViewer from "./YamlViewer.vue";

defineProps<{
  pvs: K8sPVSummary[];
}>();

const yamlVisible = ref(false);
const yamlKind = ref("persistentvolume");
const yamlNamespace = ref("default");
const yamlName = ref("");

function viewYaml(name: string) {
  yamlKind.value = "persistentvolume";
  yamlNamespace.value = "default";
  yamlName.value = name;
  yamlVisible.value = true;
}
</script>

<template>
  <el-table
    :data="pvs"
    stripe
    highlight-current-row
    style="width: 100%"
  >
    <el-table-column
      prop="name"
      label="名称"
      min-width="200"
      show-overflow-tooltip
    />
    <el-table-column
      prop="capacity"
      label="容量"
      width="100"
      align="center"
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
          type="pv"
        />
      </template>
    </el-table-column>
    <el-table-column
      prop="claim_ref"
      label="绑定声明"
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
          @click="viewYaml(row.name)"
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
